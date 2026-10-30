package runtime

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"avatarharness/internal/config"
	"avatarharness/internal/provider"
	"avatarharness/internal/store"
)

func TestAnswerGateHandlesFragmentedMarkerAndUnmarkedDraft(t *testing.T) {
	var gate AnswerGate
	if gate.Feed("\n<sp", false) != "" || gate.Committed {
		t.Fatal("partial control marker leaked")
	}
	if got := gate.Feed("eak/>第一句。", false); got != "第一句。" || !gate.Committed {
		t.Fatal(got)
	}
	var plain AnswerGate
	if got := plain.Feed("查一下。", false); got != "查一下。" || plain.Committed {
		t.Fatal("unmarked tool prelude became spoken content")
	}
}

func TestSpeechStartsBeforeLLMFinishesWithToolsAvailable(t *testing.T) {
	c := config.Default()
	c.Providers["llm"] = config.Provider{Kind: "internal", Factory: "streaming-answer", Streaming: true, TimeoutMS: 2000}
	c.Providers["tts"] = config.Provider{Kind: "internal", Factory: "streaming-audio", Streaming: true, TimeoutMS: 2000}
	s, _ := store.Open(t.TempDir())
	defer s.Close()
	b, _ := provider.New(&c, nil)
	release := make(chan struct{})
	b.Register("streaming-answer", func(ctx context.Context, req provider.Request, emit provider.Emit) (provider.Result, error) {
		for _, text := range []string{"<sp", "eak/>先回答你的问题。"} {
			if err := emit(map[string]any{"type": "text", "text": text}); err != nil {
				return provider.Result{}, err
			}
		}
		select {
		case <-release:
		case <-ctx.Done():
			return provider.Result{}, ctx.Err()
		}
		_ = emit(map[string]any{"type": "text", "text": "这是后续补充。"})
		return provider.OK(nil), emit(map[string]any{"type": "done", "tool_calls": []any{}})
	})
	b.Register("streaming-audio", func(ctx context.Context, req provider.Request, emit provider.Emit) (provider.Result, error) {
		if strings.Contains(provider.String(req.Input["text"]), "<speak") {
			t.Error("control marker reached TTS")
		}
		return provider.OK(nil), emit(map[string]any{"type": "audio", "output": map[string]any{"audio_base64": "AAAAAA==", "format": "pcm_s16le", "sample_rate": 24000, "channels": 1}})
	})
	r := New(&c, s, b)
	defer r.Shutdown()
	sid := s.Create(c.Characters[0].ID).ID
	turn, _ := r.StartVoice(sid, "测试流式输出", "input-test", time.Now().Add(-400*time.Millisecond))
	deadline := time.Now().Add(time.Second)
	found := false
	for !found && time.Now().Before(deadline) {
		for _, event := range s.Events(sid, 0) {
			if event.Type == "pipeline.audio" {
				found = true
			}
		}
		time.Sleep(time.Millisecond)
	}
	close(release)
	<-turn.Done
	if !found {
		t.Fatal("first audio waited for the full LLM response")
	}
}

func TestAudioPlaybackMustBeObservedAndKeepsSeparateAVMetric(t *testing.T) {
	c := config.Default()
	s, _ := store.Open(t.TempDir())
	defer s.Close()
	b, _ := provider.New(&c, nil)
	r := New(&c, s, b)
	defer r.Shutdown()
	sid := s.Create(c.Characters[0].ID).ID
	turn, _ := r.Start(sid, "/help", "voice", false, nil)
	<-turn.Done
	result, _ := r.Report(sid, turn.ID, map[string]any{"modality": "audio", "meaningful": true, "elapsed_ms": 1200})
	if result["audio_within_budget"] != false {
		t.Fatal("queued audio counted as played")
	}
	result, _ = r.Report(sid, turn.ID, map[string]any{"modality": "audio", "meaningful": true, "playback_observed": true, "elapsed_ms": 1200})
	if result["audio_within_budget"] != true || result["qualifies"] != false || result["within_budget"] != false {
		t.Fatal("voice and AV measurements were conflated", result)
	}
}

func TestToolDecisionPreludeNeverReachesTTS(t *testing.T) {
	c := config.Default()
	c.Providers["llm"] = config.Provider{Kind: "internal", Factory: "tool-decision", TimeoutMS: 1000}
	c.Providers["tts"] = config.Provider{Kind: "internal", Factory: "record-speech", TimeoutMS: 1000}
	s, _ := store.Open(t.TempDir())
	defer s.Close()
	b, _ := provider.New(&c, nil)
	var mu sync.Mutex
	spoken := ""
	b.Register("tool-decision", func(ctx context.Context, req provider.Request, emit provider.Emit) (provider.Result, error) {
		if tools, _ := req.Input["tools"].([]map[string]any); len(tools) > 0 {
			return provider.OK(map[string]any{"content": "我先查一下，马上就好。", "tool_calls": []any{map[string]any{"id": "time-call", "type": "function", "function": map[string]any{"name": "get_current_time", "arguments": "{}"}}}}), nil
		}
		return provider.OK(map[string]any{"content": "<speak/>工具已经返回有效结果。"}), nil
	})
	b.Register("record-speech", func(ctx context.Context, req provider.Request, emit provider.Emit) (provider.Result, error) {
		mu.Lock()
		spoken += provider.String(req.Input["text"])
		mu.Unlock()
		return provider.OK(map[string]any{"audio_base64": "AAAAAA==", "format": "pcm_s16le", "sample_rate": 24000, "channels": 1}), nil
	})
	r := New(&c, s, b)
	defer r.Shutdown()
	turn, _ := r.StartVoice(s.Create(c.Characters[0].ID).ID, "查询时间", "tool-speech-test", time.Now())
	<-turn.Done
	mu.Lock()
	defer mu.Unlock()
	if strings.Contains(spoken, "先查一下") || spoken != "工具已经返回有效结果。" {
		t.Fatal("tool-decision speech leaked or final answer was not synthesized", spoken)
	}
}

func TestShutdownCannotBeFollowedByLateRecognitionTurn(t *testing.T) {
	c := config.Default()
	s, _ := store.Open(t.TempDir())
	defer s.Close()
	b, _ := provider.New(&c, nil)
	r := New(&c, s, b)
	sid := s.Create(c.Characters[0].ID).ID
	r.Shutdown()
	if _, err := r.StartVoice(sid, "迟到的识别", "late", time.Now()); err == nil {
		t.Fatal("late recognition restarted a stopped runtime")
	}
}

func TestTimedChunkingCannotReleaseAnUncommittedToolPrelude(t *testing.T) {
	c := config.Default()
	c.Limits.TTSFirstChunkMS = 50
	c.Providers["llm"] = config.Provider{Kind: "internal", Factory: "paused-tool-decision", Streaming: true, TimeoutMS: 2000}
	c.Providers["tts"] = config.Provider{Kind: "internal", Factory: "timed-gate-speech", TimeoutMS: 1000}
	s, _ := store.Open(t.TempDir())
	defer s.Close()
	b, _ := provider.New(&c, nil)
	draftReady, release := make(chan struct{}), make(chan struct{})
	spoken := make(chan string, 4)
	b.Register("paused-tool-decision", func(ctx context.Context, req provider.Request, emit provider.Emit) (provider.Result, error) {
		if tools, _ := req.Input["tools"].([]map[string]any); len(tools) > 0 {
			if err := emit(map[string]any{"type": "text", "text": "我先核对一下当前时间然后回答"}); err != nil {
				return provider.Result{}, err
			}
			close(draftReady)
			select {
			case <-release:
			case <-ctx.Done():
				return provider.Result{}, ctx.Err()
			}
			return provider.OK(nil), emit(map[string]any{"type": "done", "tool_calls": []any{map[string]any{"id": "timed-clock", "type": "function", "function": map[string]any{"name": "get_current_time", "arguments": "{}"}}}})
		}
		if err := emit(map[string]any{"type": "text", "text": "<speak/>这是查询后的实际回答。"}); err != nil {
			return provider.Result{}, err
		}
		return provider.OK(nil), emit(map[string]any{"type": "done", "tool_calls": []any{}})
	})
	b.Register("timed-gate-speech", func(ctx context.Context, req provider.Request, emit provider.Emit) (provider.Result, error) {
		spoken <- provider.String(req.Input["text"])
		return provider.OK(map[string]any{"audio_base64": "AQABAA==", "format": "pcm_s16le", "sample_rate": 24000, "channels": 1}), nil
	})
	r := New(&c, s, b)
	defer r.Shutdown()
	turn, err := r.StartVoice(s.Create(c.Characters[0].ID).ID, "查询当前时间", "timed-gate", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	<-draftReady
	select {
	case text := <-spoken:
		t.Fatal("chunk deadline bypassed the answer-commit gate", text)
	case <-time.After(150 * time.Millisecond):
	}
	close(release)
	<-turn.Done
	select {
	case text := <-spoken:
		if text != "这是查询后的实际回答。" {
			t.Fatal(text)
		}
	default:
		t.Fatal("confirmed answer never reached TTS")
	}
}
