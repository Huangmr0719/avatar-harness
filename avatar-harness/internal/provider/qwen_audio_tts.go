package provider

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"avatarharness/internal/config"
	"github.com/gorilla/websocket"
)

func isQwenAudioTTS(p config.Provider) bool {
	return strings.HasPrefix(p.Model, "qwen-audio-") && strings.Contains(p.Model, "-tts-")
}

// Local mapping reuses the LLM's existing expression; no second model request.
func speechInstruction(input map[string]any) string {
	intent, _ := input["expression_intent"].(map[string]any)
	strength := .6
	if value, ok := intent["intensity"]; ok {
		strength = Number(value)
	}
	if math.IsNaN(strength) || math.IsInf(strength, 0) {
		strength = .6
	}
	style := String(intent["style"])
	if strength <= .05 {
		style = "natural"
	}
	delivery := "语气自然友好、平稳，像面对面聊天，不要播音腔或演讲腔。"
	switch style {
	case "happy":
		delivery = "语气自然欣喜、带笑意，轻快地祝贺或分享喜悦，不要大笑、尖叫或夸张表演。"
	case "sad":
		delivery = "语气低落、柔和，有自然的悲伤与共情，语速稍缓、音调略低，保持清晰，不要夸张哭泣。"
	case "angry":
		delivery = "语气愤怒而克制，坚定有力、重音稍强，表达不满但不要吼叫、辱骂或失去清晰度。"
	case "fearful":
		delivery = "语气担忧、紧张，略带犹疑和不安，保持可理解的自然语速，不要尖叫或夸张颤抖。"
	case "surprised":
		delivery = "语气惊讶、意外，开头音调略扬、重音明显，随后自然回落，不要整句高喊或过度表演。"
	case "disgusted":
		delivery = "语气带嫌恶和排斥，表达不适、语调略低且短促，保持克制，不要做恶心音效或攻击听者。"
	case "concerned":
		delivery = "语气温柔关切、耐心安抚，语速稍慢但不要拖长音，不要哭腔或过度悲伤。"
	case "focused":
		delivery = "语气认真平稳、清晰简洁，适度强调重点，像正常交谈，不要朗诵或演讲。"
	}
	degree := "情感表达适中、克制。"
	if strength < .4 {
		degree = "情感只需轻微流露。"
	}
	if strength > .8 {
		degree = "情感可以明显一些，但保持自然克制。"
	}
	return "请用普通话说话。" + delivery + degree
}

func qwenAudioParameters(p config.Provider, input map[string]any) (map[string]any, int, error) {
	params := map[string]any{"voice": config.BailianTTSVoice, "sample_rate": 24000}
	for _, key := range []string{"voice", "sample_rate", "volume", "rate", "pitch", "seed", "instruction", "language_hints"} {
		if value, ok := p.Parameters[key]; ok {
			params[key] = value
		}
	}
	if voice := String(input["voice"]); voice != "" {
		params["voice"] = voice
	}
	params["format"], params["text_type"], params["enable_ssml"] = "pcm", "PlainText", false
	instruction := String(params["instruction"])
	if value := String(input["instruction"]); value != "" {
		instruction = value
	}
	if _, ok := input["expression_intent"]; ok || instruction == "" {
		instruction += " " + speechInstruction(input)
	}
	params["instruction"] = strings.TrimSpace(instruction)
	rate := int(Number(params["sample_rate"]))
	if rate != 8000 && rate != 16000 && rate != 24000 && rate != 48000 {
		return nil, 0, fmt.Errorf("百炼 TTS PCM 采样率无效")
	}
	return params, rate, nil
}

func speechTaskID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	bytes[6] = bytes[6]&0x0f | 0x40
	bytes[8] = bytes[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", bytes[:4], bytes[4:6], bytes[6:8], bytes[8:10], bytes[10:]), nil
}

func audioTTSSend(conn *websocket.Conn, action, id string, payload map[string]any) error {
	return conn.WriteJSON(map[string]any{"header": map[string]any{"action": action, "task_id": id, "streaming": "duplex"}, "payload": payload})
}

func audioTTSEvent(conn *websocket.Conn, id string) (int, []byte, map[string]any, error) {
	kind, bytes, err := conn.ReadMessage()
	if err != nil {
		return 0, nil, nil, fmt.Errorf("百炼情感 TTS 会话提前结束：%w", err)
	}
	if kind == websocket.BinaryMessage {
		return kind, bytes, nil, nil
	}
	var event map[string]any
	if err := json.Unmarshal(bytes, &event); err != nil {
		return 0, nil, nil, err
	}
	header, _ := event["header"].(map[string]any)
	if String(header["task_id"]) != id {
		return 0, nil, nil, fmt.Errorf("百炼 TTS 返回了不匹配的 task_id")
	}
	if header["event"] == "task-failed" {
		return 0, nil, nil, fmt.Errorf("百炼情感 TTS 错误 %s：%s", String(header["error_code"]), String(header["error_message"]))
	}
	return kind, nil, header, nil
}

// Each text segment is a task on a preconnected lane. Ending the task flushes
// short text immediately; successful tasks reuse the WebSocket with fresh IDs.
func qwenAudioTTS(conn *websocket.Conn, p config.Provider, input map[string]any, emit Emit) (Result, error) {
	params, rate, err := qwenAudioParameters(p, input)
	if err != nil {
		return Result{}, err
	}
	id, err := speechTaskID()
	if err != nil {
		return Result{}, err
	}
	err = audioTTSSend(conn, "run-task", id, map[string]any{"task_group": "audio", "task": "tts", "function": "SpeechSynthesizer", "model": p.Model, "parameters": params, "input": map[string]any{}})
	if err != nil {
		return Result{}, err
	}
	_, _, header, err := audioTTSEvent(conn, id)
	if err != nil {
		return Result{}, err
	}
	if header["event"] != "task-started" {
		return Result{}, fmt.Errorf("百炼 TTS 缺少 task-started")
	}
	if emit != nil {
		intent, _ := input["expression_intent"].(map[string]any)
		if err = emit(map[string]any{"type": "progress", "stage": "tts.expression", "style": intent["style"], "intensity": intent["intensity"], "instruction": params["instruction"]}); err != nil {
			return Result{}, err
		}
	}
	if err = audioTTSSend(conn, "continue-task", id, map[string]any{"input": map[string]any{"text": String(input["text"])}}); err != nil {
		return Result{}, err
	}
	if err = audioTTSSend(conn, "finish-task", id, map[string]any{"input": map[string]any{}}); err != nil {
		return Result{}, err
	}
	var audio, pending []byte
	packets := 0
	for {
		kind, bytes, header, err := audioTTSEvent(conn, id)
		if err != nil {
			return Result{}, err
		}
		if kind == websocket.BinaryMessage {
			pending = append(pending, bytes...)
			// Preserve an odd byte between frames instead of misaligning PCM.
			n := len(pending) / 2 * 2
			if n == 0 {
				continue
			}
			packet := append([]byte(nil), pending[:n]...)
			pending = append(pending[:0], pending[n:]...)
			packets++
			if emit != nil {
				if err = emit(map[string]any{"type": "audio", "output": map[string]any{"audio_base64": base64.StdEncoding.EncodeToString(packet), "format": "pcm_s16le", "sample_rate": rate, "channels": 1}}); err != nil {
					return Result{}, err
				}
			} else {
				audio = append(audio, packet...)
			}
			continue
		}
		if header["event"] == "task-finished" {
			if packets == 0 || len(pending) != 0 {
				return Result{}, fmt.Errorf("百炼 TTS 未返回完整 PCM 音频")
			}
			if emit != nil {
				return OK(nil), emit(map[string]any{"type": "done"})
			}
			return OK(map[string]any{"audio_base64": base64.StdEncoding.EncodeToString(audio), "format": "pcm_s16le", "sample_rate": rate, "channels": 1}), nil
		}
	}
}
