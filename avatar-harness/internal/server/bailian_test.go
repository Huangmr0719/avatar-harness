package server

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"avatarharness/internal/config"
	"avatarharness/internal/provider"
	"github.com/gorilla/websocket"
)

// This uses local protocol fixtures, never cloud inference or physical devices.
func TestBailianVoiceEntryToolLoopAndStreamingAudio(t *testing.T) {
	t.Setenv(config.BailianKeyEnv, "local-protocol-fixture")
	var llmCalls, asrCalls, ttsCalls atomic.Int32
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer local-protocol-fixture" {
			t.Error("shared key was not forwarded")
			http.Error(w, "unauthorized", 401)
			return
		}
		if r.URL.Path == "/chat/completions" {
			llmCalls.Add(1)
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["enable_thinking"] != false || body["model"] != config.BailianLLMModel {
				t.Error("Bailian LLM preset was not applied", body)
			}
			w.Header().Set("Content-Type", "text/event-stream")
			if _, tools := body["tools"]; tools {
				streamDelta(w, map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "time-fixture", "function": map[string]any{"name": "get_current_time", "arguments": `{"timezone":"Asia/Shanghai"}`}}}})
			} else {
				messages := body["messages"].([]any)
				last := messages[len(messages)-1].(map[string]any)
				var result provider.Result
				_ = json.Unmarshal([]byte(provider.String(last["content"])), &result)
				if last["role"] != "tool" || result.Status != "ok" || result.Output["datetime"] == nil {
					t.Error("tool result did not reach LLM")
				}
				streamDelta(w, map[string]any{"content": "已读取当前时间。"})
			}
			fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		asr := r.URL.Query().Get("model") == config.BailianASRModel
		if asr {
			asrCalls.Add(1)
		}
		for {
			var event map[string]any
			if err = conn.ReadJSON(&event); err != nil {
				if asr {
					t.Error(err)
				}
				return
			}
			if event["type"] == "session.update" {
				_ = conn.WriteJSON(map[string]any{"type": "session.updated"})
			}
			if !asr && event["type"] == "input_text_buffer.commit" {
				ttsCalls.Add(1)
				for range 2 {
					_ = conn.WriteJSON(map[string]any{"type": "response.audio.delta", "delta": base64.StdEncoding.EncodeToString(make([]byte, 480))})
				}
				_ = conn.WriteJSON(map[string]any{"type": "response.done", "response": map[string]any{"status": "completed"}})
			}
			if event["type"] == "session.finish" {
				break
			}
		}
		if asr {
			_ = conn.WriteJSON(map[string]any{"type": "conversation.item.input_audio_transcription.completed", "transcript": "现在几点？", "language": "zh"})
		}
		_ = conn.WriteJSON(map[string]any{"type": "session.finished"})
	}))
	defer remote.Close()
	c := config.Default()
	_ = c.UseBailian("")
	for _, role := range []string{"llm", "asr", "tts"} {
		p := c.Providers[role]
		p.BaseURL = remote.URL
		if role != "llm" {
			p.BaseURL = "ws" + strings.TrimPrefix(remote.URL, "http") + "/realtime"
		}
		if role == "tts" {
			p.Model = "qwen3-tts-flash-realtime"
			p.Parameters = map[string]any{"voice": "Cherry", "sample_rate": 24000}
		}
		c.Providers[role] = p
	}
	a := setup(t, &c)
	sid := a.Runtime.Store.Create(c.Characters[0].ID).ID
	response := request(t, a, "POST", "/api/sessions/"+sid+"/audio", map[string]any{"input": map[string]any{"audio_base64": base64.StdEncoding.EncodeToString(make([]byte, 3200)), "sample_rate": 16000, "channels": 1, "format": "pcm_s16le"}})
	if response.Code != 202 || !strings.Contains(response.Body.String(), "现在几点？") {
		t.Fatal(response.Code, response.Body.String())
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		events := a.Runtime.Store.Events(sid, 0)
		finished, audioCount, toolCount := false, 0, 0
		for _, event := range events {
			switch event.Type {
			case "turn.error", "pipeline.error":
				t.Fatal(event)
			case "tool.finished":
				toolCount++
			case "pipeline.audio":
				if event.Data["pts_ms"] != float64(audioCount*10) || event.Data["duration_ms"] != float64(10) {
					t.Fatal("PCM sample clock mismatch", event.Data)
				}
				audioCount++
			case "turn.finished":
				finished = true
			}
		}
		if finished {
			if audioCount != 2 || toolCount != 1 || llmCalls.Load() != 2 || asrCalls.Load() != 1 || ttsCalls.Load() != 1 {
				t.Fatal("voice path incomplete", audioCount, toolCount, llmCalls.Load(), asrCalls.Load(), ttsCalls.Load())
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("protocol fixture did not complete")
		}
		time.Sleep(time.Millisecond)
	}
	state := request(t, a, "GET", "/api/state", nil).Body.String()
	if strings.Contains(state, "local-protocol-fixture") || !strings.Contains(state, c.Providers["tts"].Model) {
		t.Fatal("browser state leaked a key or omitted the selected model")
	}
}
