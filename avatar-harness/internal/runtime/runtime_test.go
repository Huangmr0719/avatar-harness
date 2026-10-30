package runtime

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"avatarharness/internal/config"
	"avatarharness/internal/provider"
	"avatarharness/internal/store"
)

func TestModelCannotCallAnUnregisteredProviderAsTool(t *testing.T) {
	c := config.Default()
	c.Providers["llm"] = config.Provider{Kind: "internal", Factory: "registry-fixture", TimeoutMS: 1000}
	c.Providers["tts"] = config.Provider{Kind: "internal", Factory: "tts-spy", TimeoutMS: 1000}
	s, _ := store.Open(t.TempDir())
	defer s.Close()
	b, _ := provider.New(&c, nil)
	var invoked atomic.Int32
	b.Register("tts-spy", func(ctx context.Context, req provider.Request, emit provider.Emit) (provider.Result, error) {
		invoked.Add(1)
		return provider.OK(nil), nil
	})
	b.Register("registry-fixture", func(ctx context.Context, req provider.Request, emit provider.Emit) (provider.Result, error) {
		tools, _ := req.Input["tools"].([]map[string]any)
		if len(tools) > 0 {
			return provider.OK(map[string]any{"content": "", "tool_calls": []map[string]any{{"id": "blocked-call", "type": "function", "function": map[string]any{"name": "tts", "arguments": "{}"}}}}), nil
		}
		return provider.OK(map[string]any{"content": "未注册的调用已经拒绝。"}), nil
	})
	r := New(&c, s, b)
	defer r.Shutdown()
	sid := s.Create(c.Characters[0].ID).ID
	turn, _ := r.Start(sid, "测试工具注册边界", "text", false, nil)
	<-turn.Done
	if invoked.Load() != 0 {
		t.Fatal("unregistered provider was executed")
	}
	found := false
	for _, event := range s.Events(sid, 0) {
		if event.Type == "tool.finished" && strings.Contains(provider.String(event.Data["error"]), "工具未注册") {
			found = true
		}
	}
	if !found {
		t.Fatal("missing registry error")
	}
}

func TestInternalGoLLMNativeToolArray(t *testing.T) {
	c := config.Default()
	c.Providers["llm"] = config.Provider{Kind: "internal", Factory: "native-fixture", TimeoutMS: 1000}
	s, _ := store.Open(t.TempDir())
	defer s.Close()
	b, _ := provider.New(&c, nil)
	b.Register("native-fixture", func(ctx context.Context, req provider.Request, emit provider.Emit) (provider.Result, error) {
		tools, _ := req.Input["tools"].([]map[string]any)
		if len(tools) > 0 {
			return provider.OK(map[string]any{"content": "", "tool_calls": []map[string]any{{"id": "native-call", "type": "function", "function": map[string]any{"name": "get_current_time", "arguments": "{}"}}}}), nil
		}
		messages := req.Input["messages"].([]map[string]any)
		var result provider.Result
		_ = json.Unmarshal([]byte(provider.String(messages[len(messages)-1]["content"])), &result)
		return provider.OK(map[string]any{"content": provider.String(result.Output["datetime"]), "tool_calls": []map[string]any{}}), nil
	})
	r := New(&c, s, b)
	defer r.Shutdown()
	sid := s.Create(c.Characters[0].ID).ID
	turn, _ := r.Start(sid, "读取时间", "text", false, nil)
	<-turn.Done
	found := false
	for _, event := range s.Events(sid, 0) {
		if event.Type == "assistant.message" {
			if _, err := time.Parse(time.RFC3339Nano, provider.String(event.Data["text"])); err != nil {
				t.Fatal(err)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("native Go tool call array was lost")
	}
}

func TestSentenceSegmentsPreserveNumbersAcrossChunks(t *testing.T) {
	var s Segmenter
	if parts := s.Feed("测量值是3.", false); len(parts) != 0 {
		t.Fatal(parts)
	}
	if got := strings.Join(s.Feed("14米。还有下一句", false), ""); got != "测量值是3.14米。" {
		t.Fatal(got)
	}
	if got := strings.Join(s.Feed("", true), ""); got != "还有下一句" {
		t.Fatal(got)
	}
}
func TestOrderedTTSPrefetch(t *testing.T) {
	c := config.Default()
	c.Providers["tts"] = config.Provider{Kind: "internal", Factory: "tts-fixture", TimeoutMS: 1000}
	b, _ := provider.New(&c, nil)
	release := make(chan struct{})
	secondReady := make(chan struct{})
	b.Register("tts-fixture", func(ctx context.Context, r provider.Request, e provider.Emit) (provider.Result, error) {
		value := uint16(111)
		if strings.HasPrefix(provider.String(r.Input["text"]), "第一") {
			select {
			case <-release:
			case <-ctx.Done():
				return provider.Result{}, ctx.Err()
			}
		} else {
			value = 222
			close(secondReady)
		}
		raw := make([]byte, 480)
		for i := 0; i < len(raw); i += 2 {
			binary.LittleEndian.PutUint16(raw[i:i+2], value)
		}
		return provider.OK(map[string]any{"audio_base64": base64.StdEncoding.EncodeToString(raw), "format": "pcm_s16le", "sample_rate": 24000, "channels": 1}), nil
	})
	var mu sync.Mutex
	audio := []map[string]any{}
	s := NewSpeech(context.Background(), &c, c.Characters[0], b, nil, func(kind string, data map[string]any) {
		if kind == "pipeline.audio" {
			mu.Lock()
			audio = append(audio, data)
			mu.Unlock()
		}
	})
	defer s.Stop()
	if err := s.Feed("第一句话。第二句话。", false); err != nil {
		t.Fatal(err)
	}
	<-secondReady
	mu.Lock()
	count := len(audio)
	mu.Unlock()
	if count != 0 {
		t.Fatal("second sentence played before first")
	}
	close(release)
	if err := s.Finish(); err != nil {
		t.Fatal(err)
	}
	if len(audio) != 2 {
		t.Fatal(len(audio))
	}
	for i, expected := range []uint16{111, 222} {
		raw, _ := base64.StdEncoding.DecodeString(provider.String(audio[i]["audio_base64"]))
		if binary.LittleEndian.Uint16(raw[:2]) != expected || audio[i]["pts_ms"] != float64(i*10) {
			t.Fatal(audio)
		}
	}
}
func TestCancelRejectsLateEventsAndTextCannotQualifyVoice(t *testing.T) {
	c := config.Default()
	s, _ := store.Open(t.TempDir())
	defer s.Close()
	b, _ := provider.New(&c, nil)
	r := New(&c, s, b)
	defer r.Shutdown()
	sid := s.Create(c.Characters[0].ID).ID
	turn, err := r.Start(sid, "/help", "text", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	<-turn.Done
	report, err := r.Report(sid, turn.ID, map[string]any{"elapsed_ms": 100, "modality": "audio_video", "meaningful": true, "av_skew_ms": 0})
	if err != nil || report["within_budget"] != false || report["diagnostic_within_budget"] != true {
		t.Fatal(report, err)
	}
	r.Cancel(sid, "stop")
	before := len(s.Events(sid, 0))
	if r.Emit(turn, "pipeline.audio", map[string]any{"audio_base64": "late"}) || len(s.Events(sid, 0)) != before {
		t.Fatal("late audio emitted")
	}
}
func TestNewInputCancelsOldModelAndKeepsHistoryComplete(t *testing.T) {
	c := config.Default()
	c.Providers["llm"] = config.Provider{Kind: "internal", Factory: "model-fixture", TimeoutMS: 1000}
	s, _ := store.Open(t.TempDir())
	defer s.Close()
	b, _ := provider.New(&c, nil)
	entered := make(chan struct{})
	b.Register("model-fixture", func(ctx context.Context, req provider.Request, e provider.Emit) (provider.Result, error) {
		close(entered)
		<-ctx.Done()
		return provider.Result{}, ctx.Err()
	})
	r := New(&c, s, b)
	defer r.Shutdown()
	sid := s.Create(c.Characters[0].ID).ID
	old, _ := r.Start(sid, "旧请求", "text", false, nil)
	<-entered
	next, _ := r.Start(sid, "/help", "text", false, nil)
	select {
	case <-old.Done:
	case <-time.After(time.Second):
		t.Fatal("old turn not cancelled")
	}
	<-next.Done
	if len(s.History(sid, 20)) != 0 {
		t.Fatal("partial old tool history committed")
	}
}
