package runtime

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"avatarharness/internal/config"
	"avatarharness/internal/intervention"
	"avatarharness/internal/provider"
	"avatarharness/internal/store"
)

func TestSessionToolsNativeLoopAndAbandonedAnswers(t *testing.T) {
	for _, tc := range []struct {
		name, tool, args, outcome string
		video                     bool
	}{
		{"video", "start_mindfulness", "{}", "success", true},
		{"listen", "set_support_mode", `{"mode":"humanistic"}`, "success", false},
		{"failed", "start_mindfulness", "{}", "error", false},
		{"cancelled", "start_mindfulness", "{}", "cancel", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := config.Default()
			c.Providers["llm"] = config.Provider{Kind: "internal", Factory: "session-fixture", TimeoutMS: 1000}
			c.Providers["tts"] = config.Provider{Kind: "internal", Factory: "speech-fixture", TimeoutMS: 1000}
			s, err := store.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			b, err := provider.New(&c, nil)
			if err != nil {
				t.Fatal(err)
			}
			var r *Runtime
			spoken := ""
			var speechMu sync.Mutex
			b.Register("speech-fixture", func(ctx context.Context, req provider.Request, emit provider.Emit) (provider.Result, error) {
				speechMu.Lock()
				spoken += provider.String(req.Input["text"])
				speechMu.Unlock()
				return provider.OK(map[string]any{"audio_base64": "AAAAAA==", "format": "pcm_s16le", "sample_rate": 24000, "channels": 1}), nil
			})
			b.Register("session-fixture", func(ctx context.Context, req provider.Request, emit provider.Emit) (provider.Result, error) {
				if len(req.Input["tools"].([]map[string]any)) > 0 {
					return provider.OK(map[string]any{"content": "正在内部安排。", "tool_calls": []any{map[string]any{"id": "control", "type": "function", "function": map[string]any{"name": tc.tool, "arguments": tc.args}}}}), nil
				}
				sid := provider.String(req.Context["session_id"])
				if s.Intervention(sid).Version != 0 {
					t.Error("tool applied before answer admission")
				}
				switch tc.outcome {
				case "error":
					return provider.Result{}, fmt.Errorf("fixture failed")
				case "cancel":
					r.Cancel(sid, "fixture cancelled")
					return provider.Result{}, context.Canceled
				}
				return content("<speak/>我们可以慢慢来，你可以随时停止。")
			})
			r = New(&c, s, b)
			defer r.Shutdown()
			sid := s.Create(c.Characters[0].ID).ID
			turn, err := r.Start(sid, "请帮我", "text", true, nil)
			if err != nil {
				t.Fatal(err)
			}
			awaitTurn(t, turn)
			state := s.Intervention(sid)
			speechMu.Lock()
			defer speechMu.Unlock()
			request, toolOK := false, false
			for _, event := range s.Events(sid, 0) {
				if event.Type == "tool.finished" && event.Data["status"] == "ok" {
					toolOK = true
				}
				if event.Type == "intervention.request" {
					request = true
					if event.TurnID != turn.ID || event.Data["execution_id"] != state.Execution.ID {
						t.Fatal("unbound video request")
					}
				}
			}
			if !toolOK {
				t.Fatal("native control tool did not execute")
			}
			if request != tc.video {
				t.Fatalf("video request=%v", request)
			}
			if tc.outcome == "success" {
				if state.LastDecision.Source != "llm_tool" || state.Version != 1 {
					t.Fatal(state)
				}
				if tc.video && (state.Execution.Status != "queued" || state.Execution.PlayedMS != 0) {
					t.Fatal("scheduling invented playback")
				}
				if spoken != "我们可以慢慢来，你可以随时停止。" {
					t.Fatal(spoken)
				}
			} else if state.Version != 0 {
				t.Fatal("abandoned control applied", state)
			}
			if strings.Contains(spoken, "内部安排") {
				t.Fatal("tool prelude reached TTS")
			}
		})
	}
}

func TestSupportToolKeepsReviewVersionAndRequiresAdmission(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			r, sid := supportFixture(t, func(ctx context.Context, req provider.Request, emit provider.Emit) (provider.Result, error) {
				switch req.Context["phase"] {
				case "support.risk":
					return content(`{"level":"ordinary","quote":""}`)
				case "support.compose":
					if len(req.Input["tools"].([]map[string]any)) > 0 {
						return provider.OK(map[string]any{"tool_calls": []any{map[string]any{"id": "video", "type": "function", "function": map[string]any{"name": "start_mindfulness", "arguments": "{}"}}}}), nil
					}
					return content(composeFixture(intervention.Plan{}, "我会播放练习，你可以随时停止。"))
				case "support.guard", "support.repair":
					if fail {
						return provider.Result{}, fmt.Errorf("review unavailable")
					}
					// A video duration is not a conversational action's integer minutes.
					return content(`{"plan":{"intent":"suggest","facts":[],"action":{"text":"播放练习","minutes":4.4}},"decision":"approve","emotion":"neutral","intensity":0.1,"speech":"<speak/>我会播放练习，你可以随时停止。"}`)
				}
				return provider.Result{}, fmt.Errorf("unexpected phase")
			}, true)
			turn, err := r.Start(sid, "请播放视频", "text", false, nil)
			if err != nil {
				t.Fatal(err)
			}
			awaitTurn(t, turn)
			requested := false
			for _, event := range r.Store.Events(sid, 0) {
				if event.Type == "intervention.request" {
					requested = true
				}
			}
			if requested == fail {
				t.Fatalf("failed review=%v, video requested=%v", fail, requested)
			}
		})
	}
}
