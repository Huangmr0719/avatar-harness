package provider

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"avatarharness/internal/config"
	"github.com/gorilla/websocket"
)

func speechFixture(t *testing.T, role string, serve func(*websocket.Conn)) *Bus {
	t.Helper()
	t.Setenv(config.BailianKeyEnv, "protocol-fixture-only")
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer protocol-fixture-only" || r.URL.Query().Get("model") == "" {
			t.Error("missing model or bearer authentication")
			http.Error(w, "unauthorized", 401)
			return
		}
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		serve(conn)
	}))
	t.Cleanup(remote.Close)
	c := config.Default()
	_ = c.UseBailian("")
	p := c.Providers[role]
	p.BaseURL = "ws" + strings.TrimPrefix(remote.URL, "http") + "/realtime"
	p.TimeoutMS = 2000
	if role == "tts" {
		p.Model = "qwen3-tts-flash-realtime"
		p.Parameters = map[string]any{"voice": "Cherry", "sample_rate": 24000}
	}
	c.Providers[role] = p
	bus, err := New(&c, nil)
	if err != nil {
		t.Fatal(err)
	}
	return bus
}

func receiveFixture(conn *websocket.Conn, kind string) (map[string]any, error) {
	var event map[string]any
	if err := conn.ReadJSON(&event); err != nil {
		return nil, err
	}
	if event["type"] != kind || String(event["event_id"]) == "" {
		return nil, errors.New("unexpected or unidentified client event: " + String(event["type"]))
	}
	return event, nil
}

func acceptSpeechInput(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	event, err := receiveFixture(conn, "session.update")
	if err != nil {
		t.Error(err)
		return
	}
	session := event["session"].(map[string]any)
	if session["mode"] != "commit" || session["response_format"] != "pcm" || session["voice"] != "Ethan" {
		t.Error(session)
	}
	_ = conn.WriteJSON(map[string]any{"type": "session.created"})
	_ = conn.WriteJSON(map[string]any{"type": "session.updated"})
	for _, kind := range []string{"input_text_buffer.append", "input_text_buffer.commit", "session.finish"} {
		if _, err = receiveFixture(conn, kind); err != nil {
			t.Error(err)
			return
		}
	}
}

func TestBailianASRConvertsMicrophoneWAVAndKeepsFinalTranscript(t *testing.T) {
	bus := speechFixture(t, "asr", func(conn *websocket.Conn) {
		event, err := receiveFixture(conn, "session.update")
		if err != nil {
			t.Error(err)
			return
		}
		session := event["session"].(map[string]any)
		if session["sample_rate"] != float64(16000) || session["input_audio_format"] != "pcm" || session["turn_detection"] != nil {
			t.Error("expected normalized PCM with no second VAD wait", session)
		}
		_ = conn.WriteJSON(map[string]any{"type": "session.updated"})
		var audio []byte
		for {
			var part map[string]any
			if err = conn.ReadJSON(&part); err != nil {
				t.Error(err)
				return
			}
			if part["type"] == "input_audio_buffer.commit" {
				break
			}
			if part["type"] != "input_audio_buffer.append" {
				t.Error(part)
				return
			}
			raw, err := base64.StdEncoding.DecodeString(String(part["audio"]))
			if err != nil {
				t.Error(err)
			}
			audio = append(audio, raw...)
		}
		if len(audio) != 3200 || int16(binary.LittleEndian.Uint16(audio[:2])) != 2000 {
			t.Error("48k stereo WAV was not converted to 100ms mono 16k PCM")
		}
		if _, err = receiveFixture(conn, "session.finish"); err != nil {
			t.Error(err)
		}
		_ = conn.WriteJSON(map[string]any{"type": "conversation.item.input_audio_transcription.text", "text": "现在", "stash": "几"})
		_ = conn.WriteJSON(map[string]any{"type": "conversation.item.input_audio_transcription.completed", "transcript": "现在几点？", "language": "zh", "emotion": "neutral"})
		_ = conn.WriteJSON(map[string]any{"type": "session.finished"})
	})
	// Deliberately incorrect outer metadata: the valid WAV header is authoritative.
	input := map[string]any{"audio_base64": base64.StdEncoding.EncodeToString(waveFixture(48000, 2, 4800)), "format": "wav", "sample_rate": 8000}
	result := bus.Invoke(context.Background(), "asr", input, nil)
	if result.Status != "ok" || result.Output["text"] != "现在几点？" || result.Output["emotion"] != "neutral" {
		t.Fatal(result)
	}
}

func TestBailianTTSStreamsBeforeCompletionAndCombinesSyncAudio(t *testing.T) {
	for _, streaming := range []bool{true, false} {
		t.Run(map[bool]string{true: "stream", false: "invoke"}[streaming], func(t *testing.T) {
			release := make(chan struct{})
			bus := speechFixture(t, "tts", func(conn *websocket.Conn) {
				acceptSpeechInput(t, conn)
				_ = conn.WriteJSON(map[string]any{"type": "response.audio.delta", "delta": "AQACAAMA"})
				if streaming {
					select {
					case <-release:
					case <-time.After(time.Second):
						t.Error("first audio buffered until completion")
					}
				}
				_ = conn.WriteJSON(map[string]any{"type": "response.audio.delta", "delta": "BAAFAA=="})
				_ = conn.WriteJSON(map[string]any{"type": "response.done", "response": map[string]any{"status": "completed"}})
				_ = conn.WriteJSON(map[string]any{"type": "session.finished"})
			})
			input := map[string]any{"text": "这是协议测试文本。", "voice": "Ethan"}
			var audio []byte
			done := false
			if streaming {
				packets := 0
				err := bus.Stream(context.Background(), "tts", input, nil, func(e map[string]any) error {
					if e["type"] == "done" {
						done = true
						return nil
					}
					packet := e["output"].(map[string]any)
					if packet["format"] != "pcm_s16le" || packet["sample_rate"] != 24000 || packet["channels"] != 1 {
						t.Error(packet)
					}
					raw, _ := base64.StdEncoding.DecodeString(String(packet["audio_base64"]))
					audio = append(audio, raw...)
					packets++
					if packets == 1 {
						close(release)
					}
					return nil
				})
				if err != nil || !done || packets != 2 {
					t.Fatal(err, done, packets)
				}
			} else {
				result := bus.Invoke(context.Background(), "tts", input, nil)
				if result.Status != "ok" {
					t.Fatal(result)
				}
				audio, _ = base64.StdEncoding.DecodeString(String(result.Output["audio_base64"]))
			}
			if !bytes.Equal(audio, []byte{1, 0, 2, 0, 3, 0, 4, 0, 5, 0}) {
				t.Fatal("PCM chunks were reordered or lost", audio)
			}
		})
	}
}

func TestBailianTTSErrorAndTruncationCannotBecomeSuccess(t *testing.T) {
	for _, mode := range []string{"api-error", "invalid-audio", "no-audio", "truncated", "failed-response"} {
		t.Run(mode, func(t *testing.T) {
			bus := speechFixture(t, "tts", func(conn *websocket.Conn) {
				acceptSpeechInput(t, conn)
				switch mode {
				case "api-error":
					_ = conn.WriteJSON(map[string]any{"type": "error", "error": map[string]any{"code": "InvalidApiKey", "message": "fixture rejection"}})
				case "invalid-audio":
					_ = conn.WriteJSON(map[string]any{"type": "response.audio.delta", "delta": "not-base64"})
				case "no-audio":
					_ = conn.WriteJSON(map[string]any{"type": "session.finished"})
				case "truncated":
					_ = conn.WriteJSON(map[string]any{"type": "response.audio.delta", "delta": "AAA="})
				case "failed-response":
					_ = conn.WriteJSON(map[string]any{"type": "response.done", "response": map[string]any{"status": "failed"}})
				}
			})
			result := bus.Invoke(context.Background(), "tts", map[string]any{"text": "测试", "voice": "Ethan"}, nil)
			if result.Status != "error" || result.Error == "" || len(result.Output) != 0 {
				t.Fatal("bad response accepted", result)
			}
		})
	}
}

func TestBailianCancellationClosesSocket(t *testing.T) {
	started, closed := make(chan struct{}), make(chan struct{})
	bus := speechFixture(t, "tts", func(conn *websocket.Conn) {
		acceptSpeechInput(t, conn)
		close(started)
		_, _, _ = conn.ReadMessage()
		close(closed)
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan Result, 1)
	go func() { result <- bus.Invoke(ctx, "tts", map[string]any{"text": "测试", "voice": "Ethan"}, nil) }()
	<-started
	cancel()
	select {
	case output := <-result:
		if output.Status != "cancelled" {
			t.Fatal(output)
		}
	case <-time.After(time.Second):
		t.Fatal("cancel did not interrupt socket read")
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("upstream connection remained open after cancellation")
	}
}

func TestBailianMissingKeyAndInvalidInputNeverConnect(t *testing.T) {
	var attempts atomic.Int32
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { attempts.Add(1); w.WriteHeader(401) }))
	defer remote.Close()
	c := config.Default()
	_ = c.UseBailian("")
	p := c.Providers["asr"]
	p.BaseURL = "ws" + strings.TrimPrefix(remote.URL, "http")
	c.Providers["asr"] = p
	bus, _ := New(&c, nil)
	t.Setenv(config.BailianKeyEnv, "")
	if result := bus.Invoke(context.Background(), "asr", map[string]any{}, nil); result.Status != "error" || !strings.Contains(result.Error, "缺少百炼密钥") {
		t.Fatal(result)
	}
	t.Setenv(config.BailianKeyEnv, "fixture")
	if result := bus.Invoke(context.Background(), "asr", map[string]any{"format": "wav", "audio_base64": "AAAA"}, nil); result.Status != "error" {
		t.Fatal(result)
	}
	if attempts.Load() != 0 {
		t.Fatal("invalid request opened billable session")
	}
	input := map[string]any{"format": "wav", "audio_base64": base64.StdEncoding.EncodeToString(waveFixture(16000, 1, 1600))}
	if result := bus.Invoke(context.Background(), "asr", input, nil); result.Status != "error" || !strings.Contains(result.Error, "HTTP 401") {
		t.Fatal(result)
	}
}

func waveFixture(rate, channels, frames int) []byte {
	raw := make([]byte, 44+frames*channels*2)
	copy(raw, "RIFF")
	binary.LittleEndian.PutUint32(raw[4:], uint32(len(raw)-8))
	copy(raw[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(raw[16:], 16)
	binary.LittleEndian.PutUint16(raw[20:], 1)
	binary.LittleEndian.PutUint16(raw[22:], uint16(channels))
	binary.LittleEndian.PutUint32(raw[24:], uint32(rate))
	binary.LittleEndian.PutUint32(raw[28:], uint32(rate*channels*2))
	binary.LittleEndian.PutUint16(raw[32:], uint16(channels*2))
	binary.LittleEndian.PutUint16(raw[34:], 16)
	copy(raw[36:], "data")
	binary.LittleEndian.PutUint32(raw[40:], uint32(len(raw)-44))
	for index := 0; index < frames; index++ {
		for channel := 0; channel < channels; channel++ {
			binary.LittleEndian.PutUint16(raw[44+(index*channels+channel)*2:], uint16(1000+2000*channel))
		}
	}
	return raw
}

func TestASRPCMResamplingAndBadContainers(t *testing.T) {
	for _, rate := range []int{8000, 16000, 44100, 48000} {
		input := map[string]any{"format": "wav", "audio_base64": base64.StdEncoding.EncodeToString(waveFixture(rate, 1, rate/10))}
		raw, err := asrPCM(input)
		if err != nil || len(raw) != 3200 || int16(binary.LittleEndian.Uint16(raw[:2])) != 1000 {
			t.Fatal(rate, len(raw), err)
		}
	}
	broken := waveFixture(16000, 1, 1600)
	for _, raw := range [][]byte{broken[:12], broken[:len(broken)-1], {0, 1, 2}} {
		if _, err := asrPCM(map[string]any{"format": "wav", "audio_base64": base64.StdEncoding.EncodeToString(raw)}); err == nil {
			t.Fatal("bad WAV accepted")
		}
	}
}
