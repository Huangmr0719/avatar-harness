package runtime

import (
	"context"
	"math"
	"sync/atomic"
	"testing"
	"time"

	"avatarharness/internal/config"
	"avatarharness/internal/provider"
	"avatarharness/internal/store"
)

func TestFirstSpeechTimingCoversUnmarkedFinalAnswer(t *testing.T) {
	for _, tc := range []struct {
		name       string
		structured bool
		toolRound  bool
	}{
		{name: "plain"},
		{name: "structured", structured: true},
		{name: "after-tool", toolRound: true},
		{name: "structured-after-tool", structured: true, toolRound: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := config.Default()
			c.Limits.MaxToolBatches = 2 // Keep tools available after the first tool round too.
			if tc.structured {
				c.Characters[0].Renderer = config.Renderer{Kind: "lam", AssetURL: "/models/lam/avatar.zip"}
			}
			c.Providers["llm"] = config.Provider{Kind: "internal", Factory: "unmarked-answer", Streaming: true, TimeoutMS: 2000}
			c.Providers["tts"] = config.Provider{Kind: "internal", Factory: "timing-audio", TimeoutMS: 1000}
			s, err := store.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			b, err := provider.New(&c, nil)
			if err != nil {
				t.Fatal(err)
			}
			var llmCalls atomic.Int32
			b.Register("unmarked-answer", func(ctx context.Context, req provider.Request, emit provider.Emit) (provider.Result, error) {
				call := llmCalls.Add(1)
				if tools, _ := req.Input["tools"].([]map[string]any); len(tools) == 0 {
					t.Error("repro requires tools to be available while the answer is uncommitted")
				}
				if tc.toolRound && call == 1 {
					return provider.OK(nil), emit(map[string]any{"type": "done", "tool_calls": []any{map[string]any{"id": "clock", "type": "function", "function": map[string]any{"name": "get_current_time", "arguments": "{}"}}}})
				}
				text := "这是确认后的回答。"
				if tc.structured {
					text = `{"emotion":"neutral","intensity":0.6,"speech":"` + text + `"}`
				}
				if err := emit(map[string]any{"type": "text", "text": text}); err != nil {
					return provider.Result{}, err
				}
				return provider.OK(nil), emit(map[string]any{"type": "done", "tool_calls": []any{}})
			})
			b.Register("timing-audio", func(ctx context.Context, req provider.Request, emit provider.Emit) (provider.Result, error) {
				return provider.OK(map[string]any{"audio_base64": "AQABAA==", "format": "pcm_s16le", "sample_rate": 24000, "channels": 1}), nil
			})
			r := New(&c, s, b)
			defer r.Shutdown()
			sid := s.Create(c.Characters[0].ID).ID
			turn, err := r.StartVoice(sid, "测试延迟显示", "timing-input", time.Now())
			if err != nil {
				t.Fatal(err)
			}
			<-turn.Done
			timings, audio := 0, false
			for _, event := range s.Events(sid, 0) {
				if event.Type == "turn.error" || event.Type == "pipeline.error" {
					t.Fatal(event.Data)
				}
				if event.Type == "pipeline.timing" && event.Data["stage"] == "llm.first_speech" {
					timings++
					elapsed, ok := event.Data["elapsed_ms"].(float64)
					if !ok || elapsed < 0 || math.IsNaN(elapsed) || math.IsInf(elapsed, 0) {
						t.Fatal("invalid LLM first-speech timing", event.Data)
					}
				}
				if event.Type == "pipeline.audio" {
					audio = true
					if timings != 1 {
						t.Fatalf("LLM delay missing when answer audio arrives: timings=%d", timings)
					}
				}
			}
			if !audio || timings != 1 {
				t.Fatalf("answer audio=%v, LLM first-speech timings=%d", audio, timings)
			}
		})
	}
}
