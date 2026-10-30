package provider

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"avatarharness/internal/config"
	"avatarharness/internal/store"
	"github.com/gorilla/websocket"
)

func (b *Bus) bailian(ctx context.Context, role string, p config.Provider, input map[string]any, emit Emit) (Result, error) {
	if role != "asr" && role != "tts" {
		return Result{}, fmt.Errorf("百炼实时语音不支持 %s", role)
	}
	if os.Getenv(p.APIKeyEnv) == "" {
		return Result{}, fmt.Errorf("缺少百炼密钥 %s，请运行 ./start configure bailian", p.APIKeyEnv)
	}
	// Validate and convert microphone audio before opening a billable session.
	var pcm []byte
	var err error
	if role == "asr" {
		pcm, err = asrPCM(input)
		if err != nil {
			return Result{}, err
		}
	} else if strings.TrimSpace(String(input["text"])) == "" {
		return Result{}, fmt.Errorf("TTS 文本不能为空")
	}
	if role == "tts" && isQwenAudioTTS(p) {
		if _, _, err := qwenAudioParameters(p, input); err != nil {
			return Result{}, err
		}
	}
	conn, err := dialBailian(ctx, p, role)
	if err != nil {
		return Result{}, err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetReadDeadline(deadline)
		_ = conn.SetWriteDeadline(deadline)
	}
	if role == "asr" {
		result, err := bailianASR(conn, p, pcm, emit)
		return result, realtimeError(ctx, err)
	}
	if isQwenAudioTTS(p) {
		result, err := qwenAudioTTS(conn, p, input, emit)
		return result, realtimeError(ctx, err)
	}
	result, err := bailianTTS(conn, p, input, emit)
	return result, realtimeError(ctx, err)
}

func dialBailian(ctx context.Context, p config.Provider, role string) (*websocket.Conn, error) {
	key := os.Getenv(p.APIKeyEnv)
	if key == "" {
		return nil, fmt.Errorf("缺少百炼密钥 %s，请运行 ./start configure bailian", p.APIKeyEnv)
	}
	u, err := url.Parse(p.BaseURL)
	if err != nil {
		return nil, err
	}
	query := u.Query()
	if isQwenAudioTTS(p) {
		query.Del("model")
	} else {
		query.Set("model", p.Model)
	}
	u.RawQuery = query.Encode()
	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	conn, response, err := dialer.DialContext(ctx, u.String(), http.Header{"Authorization": {"Bearer " + key}})
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if response != nil {
			if response.Body != nil {
				response.Body.Close()
			}
			return nil, fmt.Errorf("百炼 %s 连接失败：HTTP %d（检查地域、密钥和模型权限）", role, response.StatusCode)
		}
		return nil, fmt.Errorf("百炼 %s 连接失败：%w", role, err)
	}
	conn.SetReadLimit(8 << 20)
	return conn, nil
}

func realtimeError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if deadline, ok := ctx.Deadline(); err != nil && ok && !time.Now().Before(deadline) {
		return context.DeadlineExceeded
	}
	return err
}

func realtimeSend(conn *websocket.Conn, kind string, fields map[string]any) error {
	event := map[string]any{"event_id": "event_" + store.ID(), "type": kind}
	for key, value := range fields {
		event[key] = value
	}
	return conn.WriteJSON(event)
}

func realtimeRead(conn *websocket.Conn) (map[string]any, error) {
	var event map[string]any
	if err := conn.ReadJSON(&event); err != nil {
		return nil, fmt.Errorf("百炼语音会话提前结束：%w", err)
	}
	if event["type"] == "error" || event["type"] == "conversation.item.input_audio_transcription.failed" {
		detail, _ := event["error"].(map[string]any)
		return nil, fmt.Errorf("百炼语音错误 %s：%s", String(detail["code"]), String(detail["message"]))
	}
	if event["type"] == "response.done" {
		response, _ := event["response"].(map[string]any)
		if status := String(response["status"]); status != "" && status != "completed" {
			return nil, fmt.Errorf("百炼语音响应未完成：%s", status)
		}
	}
	return event, nil
}

func realtimeConfigure(conn *websocket.Conn, session map[string]any) error {
	if err := realtimeSend(conn, "session.update", map[string]any{"session": session}); err != nil {
		return err
	}
	for {
		event, err := realtimeRead(conn)
		if err != nil {
			return err
		}
		switch event["type"] {
		case "session.updated":
			return nil
		case "session.finished":
			return fmt.Errorf("百炼语音会话在配置完成前结束")
		}
	}
}

func bailianASR(conn *websocket.Conn, p config.Provider, pcm []byte, emit Emit) (Result, error) {
	language := String(p.Parameters["language"])
	if language == "" {
		language = "zh"
	}
	session := map[string]any{"input_audio_format": "pcm", "sample_rate": 16000, "input_audio_transcription": map[string]any{"language": language}, "turn_detection": nil}
	if err := realtimeConfigure(conn, session); err != nil {
		return Result{}, err
	}
	// The browser has already segmented the utterance. Commit once to avoid a
	// second server-side silence wait; send 100 ms PCM chunks without fake pacing.
	for offset := 0; offset < len(pcm); offset += 3200 {
		end := min(offset+3200, len(pcm))
		if err := realtimeSend(conn, "input_audio_buffer.append", map[string]any{"audio": base64.StdEncoding.EncodeToString(pcm[offset:end])}); err != nil {
			return Result{}, err
		}
	}
	if err := realtimeSend(conn, "input_audio_buffer.commit", nil); err != nil {
		return Result{}, err
	}
	if err := realtimeSend(conn, "session.finish", nil); err != nil {
		return Result{}, err
	}
	var text strings.Builder
	output := map[string]any{}
	completed := false
	for {
		event, err := realtimeRead(conn)
		if err != nil {
			return Result{}, err
		}
		switch event["type"] {
		case "conversation.item.input_audio_transcription.text":
			if emit != nil {
				if err = emit(map[string]any{"type": "transcript", "text": String(event["text"]) + String(event["stash"]), "final": false}); err != nil {
					return Result{}, err
				}
			}
		case "conversation.item.input_audio_transcription.completed":
			completed = true
			text.WriteString(String(event["transcript"]))
			for _, key := range []string{"language", "emotion"} {
				if value := String(event[key]); value != "" {
					output[key] = value
				}
			}
		case "session.finished":
			if !completed {
				return Result{}, fmt.Errorf("百炼 ASR 缺少最终识别结果")
			}
			output["text"] = strings.TrimSpace(text.String())
			if emit != nil {
				if err = emit(map[string]any{"type": "result", "status": "ok", "output": output}); err != nil {
					return Result{}, err
				}
			}
			return OK(output), nil
		}
	}
}

func bailianTTSSession(p config.Provider, input map[string]any) (map[string]any, int, error) {
	session := map[string]any{"voice": "Cherry", "language_type": "Chinese", "sample_rate": 24000}
	for key, value := range p.Parameters {
		session[key] = value
	}
	if voice := String(input["voice"]); voice != "" {
		session["voice"] = voice
	}
	// Only PCM packets have an unambiguous sample clock in the Harness contract.
	session["mode"] = "commit"
	session["response_format"] = "pcm"
	rate := int(Number(session["sample_rate"]))
	if rate != 8000 && rate != 16000 && rate != 24000 && rate != 48000 {
		return nil, 0, fmt.Errorf("百炼 TTS 采样率无效")
	}
	return session, rate, nil
}

func bailianTTS(conn *websocket.Conn, p config.Provider, input map[string]any, emit Emit) (Result, error) {
	session, rate, err := bailianTTSSession(p, input)
	if err != nil {
		return Result{}, err
	}
	if err := realtimeConfigure(conn, session); err != nil {
		return Result{}, err
	}
	if err := realtimeSend(conn, "input_text_buffer.append", map[string]any{"text": String(input["text"])}); err != nil {
		return Result{}, err
	}
	if err := realtimeSend(conn, "input_text_buffer.commit", nil); err != nil {
		return Result{}, err
	}
	if err := realtimeSend(conn, "session.finish", nil); err != nil {
		return Result{}, err
	}
	return readBailianTTS(conn, rate, emit, true)
}

// A reusable commit-mode session ends a segment at response.done, not at
// session.finished. A cancelled/failed response must never be reused.
func readBailianTTS(conn *websocket.Conn, rate int, emit Emit, waitSession bool) (Result, error) {
	var audio []byte
	received := false
	finish := func() (Result, error) {
		if !received {
			return Result{}, fmt.Errorf("百炼 TTS 未返回音频")
		}
		if emit != nil {
			return OK(nil), emit(map[string]any{"type": "done"})
		}
		return OK(pcmPacket(base64.StdEncoding.EncodeToString(audio), rate)), nil
	}
	for {
		event, err := realtimeRead(conn)
		if err != nil {
			return Result{}, err
		}
		switch event["type"] {
		case "response.audio.delta":
			delta := String(event["delta"])
			raw, decodeErr := base64.StdEncoding.DecodeString(delta)
			if decodeErr != nil || len(raw) == 0 || len(raw)%2 != 0 {
				return Result{}, fmt.Errorf("百炼 TTS 返回无效的 PCM 音频")
			}
			received = true
			if emit != nil {
				if err = emit(map[string]any{"type": "audio", "output": pcmPacket(delta, rate)}); err != nil {
					return Result{}, err
				}
			} else {
				if len(audio)+len(raw) > 32<<20 {
					return Result{}, fmt.Errorf("同步 TTS 音频超过 32 MiB，请使用流式调用")
				}
				audio = append(audio, raw...)
			}
		case "response.done":
			if !waitSession {
				return finish()
			}
		case "session.finished":
			if !waitSession {
				return Result{}, fmt.Errorf("百炼 TTS 会话在响应完成前结束")
			}
			return finish()
		}
	}
}

func pcmPacket(audio string, rate int) map[string]any {
	return map[string]any{"audio_base64": audio, "format": "pcm_s16le", "sample_rate": rate, "channels": 1}
}
