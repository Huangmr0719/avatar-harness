package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"avatarharness/internal/config"
	"avatarharness/internal/provider"
	"github.com/gorilla/websocket"
)

func liveFixture(t *testing.T, holdFinal <-chan struct{}) (*App, *websocket.Conn, string) {
	t.Helper()
	t.Setenv(config.BailianKeyEnv, "local-live-fixture")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		sent := false
		for {
			var event map[string]any
			if conn.ReadJSON(&event) != nil {
				return
			}
			switch event["type"] {
			case "session.update":
				_ = conn.WriteJSON(map[string]any{"type": "session.updated"})
			case "input_audio_buffer.append":
				if !sent {
					sent = true
					_ = conn.WriteJSON(map[string]any{"type": "conversation.item.input_audio_transcription.text", "text": "正在", "stash": "说话"})
				}
			case "session.finish":
				if holdFinal != nil {
					select {
					case <-holdFinal:
					case <-time.After(2 * time.Second):
						return
					}
				}
				_ = conn.WriteJSON(map[string]any{"type": "conversation.item.input_audio_transcription.completed", "transcript": "实时语音测试。"})
				_ = conn.WriteJSON(map[string]any{"type": "session.finished"})
				return
			}
		}
	}))
	t.Cleanup(upstream.Close)
	c := config.Default()
	_ = c.UseBailian("")
	p := c.Providers["asr"]
	p.BaseURL = "ws" + strings.TrimPrefix(upstream.URL, "http")
	c.Providers["asr"] = p
	c.Providers["llm"] = config.Provider{Kind: "internal", Factory: "live-test-llm", Streaming: true, TimeoutMS: 2000}
	c.Providers["tts"] = config.Provider{Kind: "internal", Factory: "live-test-tts", Streaming: true, TimeoutMS: 2000}
	a := setup(t, &c)
	a.Runtime.Bus.Register("live-test-llm", func(ctx context.Context, req provider.Request, emit provider.Emit) (provider.Result, error) {
		_ = emit(map[string]any{"type": "text", "text": "<speak/>已收到你的问题。"})
		return provider.OK(nil), emit(map[string]any{"type": "done", "tool_calls": []any{}})
	})
	a.Runtime.Bus.Register("live-test-tts", func(ctx context.Context, req provider.Request, emit provider.Emit) (provider.Result, error) {
		return provider.OK(nil), emit(map[string]any{"type": "audio", "output": map[string]any{"audio_base64": "AAAAAA==", "format": "pcm_s16le", "sample_rate": 24000, "channels": 1}})
	})
	local := httptest.NewServer(a.Handler)
	t.Cleanup(local.Close)
	sid := a.Runtime.Store.Create(c.Characters[0].ID).ID
	url := "ws" + strings.TrimPrefix(local.URL, "http") + "/api/sessions/" + sid + "/voice"
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	awaitVoice(t, conn, "ready")
	return a, conn, sid
}

func awaitVoice(t *testing.T, conn *websocket.Conn, kind string) map[string]any {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		var event map[string]any
		if err := conn.ReadJSON(&event); err != nil {
			t.Fatal(kind, err)
		}
		if event["type"] == "error" {
			t.Fatal(event)
		}
		if event["type"] == kind {
			return event
		}
	}
}

func TestLiveVoicePartialRecognitionAndBargeIn(t *testing.T) {
	a, conn, sid := liveFixture(t, nil)
	started, cancelled := make(chan struct{}), make(chan struct{})
	a.Runtime.Bus.Register("live-test-llm", func(ctx context.Context, req provider.Request, emit provider.Emit) (provider.Result, error) {
		if req.Context["input_id"] == "first" {
			close(started)
			<-ctx.Done()
			close(cancelled)
			return provider.Result{}, ctx.Err()
		}
		_ = emit(map[string]any{"type": "text", "text": "<speak/>新的输入已收到。"})
		return provider.OK(nil), emit(map[string]any{"type": "done", "tool_calls": []any{}})
	})
	_ = conn.WriteJSON(map[string]any{"type": "start", "input_id": "first"})
	_ = conn.WriteMessage(websocket.BinaryMessage, make([]byte, 3200))
	partial := awaitVoice(t, conn, "transcript")
	if partial["text"] != "正在说话" || partial["final"] != false {
		t.Fatal("partial recognition was not returned while speaking", partial)
	}
	_ = conn.WriteJSON(map[string]any{"type": "commit", "input_id": "first", "end_age_ms": 400})
	awaitVoice(t, conn, "turn")
	<-started
	_ = conn.WriteJSON(map[string]any{"type": "start", "input_id": "second"})
	_ = conn.WriteMessage(websocket.BinaryMessage, make([]byte, 3200))
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("new speech did not cancel in-flight LLM")
	}
	partial = awaitVoice(t, conn, "transcript")
	if partial["input_id"] != "second" {
		t.Fatal("old transcript escaped the generation guard", partial)
	}
	_ = conn.WriteJSON(map[string]any{"type": "commit", "input_id": "second", "end_age_ms": 400})
	awaitVoice(t, conn, "turn")
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		for _, event := range a.Runtime.Store.Events(sid, 0) {
			if event.Type == "pipeline.audio" {
				return
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("new input did not produce streaming audio")
}

func TestStopDuringRecognitionRejectsLateFinal(t *testing.T) {
	release := make(chan struct{})
	a, conn, sid := liveFixture(t, release)
	_ = conn.WriteJSON(map[string]any{"type": "start", "input_id": "cancelled-input"})
	_ = conn.WriteMessage(websocket.BinaryMessage, make([]byte, 3200))
	awaitVoice(t, conn, "transcript")
	_ = conn.WriteJSON(map[string]any{"type": "commit", "input_id": "cancelled-input", "end_age_ms": 400})
	_ = conn.WriteJSON(map[string]any{"type": "cancel", "request_id": "stop-1"})
	ack := awaitVoice(t, conn, "cancelled")
	if ack["request_id"] != "stop-1" {
		t.Fatal("stop was not acknowledged")
	}
	close(release)
	awaitVoice(t, conn, "ready")
	for _, event := range a.Runtime.Store.Events(sid, 0) {
		if event.Type == "turn.started" {
			t.Fatal("cancelled ASR result started a new response", event)
		}
	}
	_ = conn.WriteJSON(map[string]any{"type": "start", "input_id": "next-input"})
	_ = conn.WriteMessage(websocket.BinaryMessage, make([]byte, 3200))
	awaitVoice(t, conn, "transcript")
	_ = conn.WriteJSON(map[string]any{"type": "commit", "input_id": "next-input", "end_age_ms": 400})
	awaitVoice(t, conn, "turn")
}

func TestHTTPStopAcknowledgesActiveRecognition(t *testing.T) {
	a, conn, sid := liveFixture(t, nil)
	_ = conn.WriteJSON(map[string]any{"type": "start", "input_id": "recording-input"})
	_ = conn.WriteMessage(websocket.BinaryMessage, make([]byte, 3200))
	awaitVoice(t, conn, "transcript")
	result := request(t, a, "POST", "/api/sessions/"+sid+"/stop", nil)
	if result.Code != 200 || !strings.Contains(result.Body.String(), `"stopped":true`) {
		t.Fatal("HTTP stop did not acknowledge cancelled ASR", result.Body.String())
	}
}
