package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"avatarharness/internal/config"
	"avatarharness/internal/provider"
	"avatarharness/internal/store"
)

func TestMemorySelectionRequiresUserEvidence(t *testing.T) {
	c := store.Conversation{Archived: []map[string]any{
		{"role": "user", "content": "我每天只有十五分钟。"},
		{"role": "assistant", "content": `{"emotion":"happy","intensity":0.3,"speech":"<speak/>今晚先读一页。"}`},
		{"role": "user", "content": "好，就这样。"},
		{"role": "assistant", "content": "那我们下次回顾。"},
	}}
	memory, err := selectMemory(`{"entries":[{"kind":"constraint","message":1,"quote":"每天只有十五分钟"},{"kind":"agreed_action","message":3,"quote":"好，就这样。","context":"model invented this"}]}`, c, 4)
	if err != nil || len(memory.Entries) != 2 || memory.Entries[1].Context != "今晚先读一页。" {
		t.Fatal(memory, err)
	}
	for _, raw := range []string{
		`{"entries":[{"kind":"constraint","message":1,"quote":"每天有两小时"}]}`,
		`{"entries":[{"kind":"constraint","message":2,"quote":"今晚先读一页"}]}`,
		`{"entries":[{"kind":"diagnosis","message":1,"quote":"十五分钟"}]}`,
		`{"entries":[{"kind":"preference","message":9,"quote":"十五分钟"}]}`,
		`{"entries":[{"kind":"preference","message":1,"quote":""}]}`,
		`{"entries":null}`,
		`{"entries":[`,
	} {
		if _, err := selectMemory(raw, c, 4); err == nil {
			t.Fatalf("ungrounded memory accepted: %s", raw)
		}
	}
}

func TestArchivedMemorySearchesOmittedFactsAndLongUtterances(t *testing.T) {
	long := strings.Repeat("这是项目的背景信息。", 100) + "预算只能三百元，设备只有手机。"
	c := store.Conversation{Memory: store.SessionMemory{Through: 40}, Archived: []map[string]any{{"role": "user", "content": long}, {"role": "assistant", "content": "收到。"}}}
	for i := 0; i < 25; i++ {
		c.Archived = append(c.Archived, map[string]any{"role": "user", "content": fmt.Sprintf("我们稍后再聊话题%d。", i)}, map[string]any{"role": "assistant", "content": "好的。"})
	}
	entries := archivedMemory(c, "我说过的预算和设备限制是什么？")
	found := false
	for _, entry := range entries {
		if entry.Message == 1 && strings.Contains(entry.Quote, "预算只能三百元") && strings.Contains(entry.Quote, "设备只有手机") {
			found = true
		}
	}
	if !found || len(entries) > 8 {
		t.Fatal(entries)
	}
}

func TestMemoryInjectedBeforeRecentHistoryAndNotInheritedContext(t *testing.T) {
	c := config.Default()
	c.Tools = nil
	c.Limits.MaxHistoryMessages = 2
	c.Providers["llm"] = config.Provider{Kind: "internal", Factory: "memory-capture", TimeoutMS: 1000}
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	b, _ := provider.New(&c, nil)
	var captured []map[string]any
	b.Register("memory-capture", func(ctx context.Context, req provider.Request, emit provider.Emit) (provider.Result, error) {
		captured = req.Input["messages"].([]map[string]any)
		return provider.OK(map[string]any{"content": "<speak/>每天十五分钟，预算三百元。"}), nil
	})
	r := New(&c, s, b)
	defer r.Shutdown()
	sid := s.Create(c.Characters[0].ID).ID
	s.AddHistory(sid, []map[string]any{{"role": "user", "content": "每天十五分钟，预算三百元。"}, {"role": "assistant", "content": "记住了。"}, {"role": "user", "content": "换个话题。"}, {"role": "assistant", "content": "好的。"}})
	turn, _ := r.Start(sid, "我的限制是什么？", "text", false, nil)
	<-turn.Done
	if len(captured) != 5 || !strings.Contains(provider.String(captured[1]["content"]), "每天十五分钟") || captured[2]["content"] != "换个话题。" || captured[4]["content"] != "我的限制是什么？" {
		t.Fatal(captured)
	}
	inherited := []map[string]any{{"role": "user", "content": "由调用者提供的历史。"}}
	turn, _ = r.Start(sid, "由调用者提供的历史。", "text", false, inherited)
	<-turn.Done
	if len(captured) != 2 || captured[1]["content"] != "由调用者提供的历史。" {
		t.Fatal("session memory contaminated caller-owned context", captured)
	}
}

func TestBackgroundMemoryCommitsGroundedState(t *testing.T) {
	c := config.Default()
	c.Tools = nil
	c.Limits.MaxHistoryMessages = 2
	c.Providers["llm"] = config.Provider{Kind: "internal", Factory: "memory-select", TimeoutMS: 1000}
	s, _ := store.Open(t.TempDir())
	defer s.Close()
	b, _ := provider.New(&c, nil)
	b.Register("memory-select", func(ctx context.Context, req provider.Request, emit provider.Emit) (provider.Result, error) {
		if strings.HasSuffix(provider.String(req.Context["request_id"]), "-memory") {
			messages := req.Input["messages"].([]map[string]any)
			if strings.Contains(provider.String(messages[1]["content"]), "user_perception") || len(req.Input["tools"].([]map[string]any)) != 0 {
				t.Error("ephemeral observations or tools reached memory selection")
			}
			return provider.OK(map[string]any{"content": `{"entries":[{"kind":"constraint","message":1,"quote":"每天十五分钟"}]}`}), nil
		}
		return provider.OK(map[string]any{"content": "<speak/>我记得你的时间限制。"}), nil
	})
	r := New(&c, s, b)
	defer r.Shutdown()
	sid := s.Create(c.Characters[0].ID).ID
	seedMemoryHistory(s, sid)
	turn, _ := r.StartVoice(sid, "还记得吗？", "voice-memory", time.Now(), map[string]any{"emotion": "sad"})
	<-turn.Done
	deadline := time.After(time.Second)
	for s.Conversation(sid, 2).Memory.Through == 0 {
		select {
		case <-deadline:
			t.Fatal("background memory did not finish")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	memory := s.Conversation(sid, 2).Memory
	if len(memory.Entries) != 1 || memory.Entries[0].Quote != "每天十五分钟" || memory.Through != 10 {
		t.Fatal(memory)
	}
	for _, event := range s.Events(sid, 0) {
		raw, _ := json.Marshal(event.Data)
		if strings.Contains(string(raw), `"entries":[`) {
			t.Fatal("memory contents leaked into UI events")
		}
	}
}

func TestBackgroundMemoryDoesNotBlockTurnOrSurviveCancellation(t *testing.T) {
	for _, nextInput := range []string{"/help", "/clear"} {
		t.Run(nextInput, func(t *testing.T) {
			c := config.Default()
			c.Tools = nil
			c.Limits.MaxHistoryMessages = 2
			c.Providers["llm"] = config.Provider{Kind: "internal", Factory: "slow-memory", TimeoutMS: 1000}
			s, _ := store.Open(t.TempDir())
			defer s.Close()
			b, _ := provider.New(&c, nil)
			entered := make(chan struct{})
			release := make(chan struct{})
			defer close(release)
			b.Register("slow-memory", func(ctx context.Context, req provider.Request, emit provider.Emit) (provider.Result, error) {
				if strings.HasSuffix(provider.String(req.Context["request_id"]), "-memory") {
					close(entered)
					<-release // A late provider deliberately ignores cancellation.
					return provider.OK(map[string]any{"content": `{"entries":[{"kind":"constraint","message":1,"quote":"每天十五分钟"}]}`}), nil
				}
				return provider.OK(map[string]any{"content": "<speak/>好的。"}), nil
			})
			r := New(&c, s, b)
			defer r.Shutdown()
			sid := s.Create(c.Characters[0].ID).ID
			seedMemoryHistory(s, sid)
			turn, _ := r.Start(sid, "记住我的约束。", "text", false, nil)
			select {
			case <-turn.Done:
			case <-time.After(time.Second):
				t.Fatal("memory blocked the completed response")
			}
			<-entered
			r.mu.Lock()
			job := r.memoryJobs[sid]
			r.mu.Unlock()
			next, _ := r.Start(sid, nextInput, "text", false, nil)
			<-next.Done
			<-job.Done
			if memory := s.Conversation(sid, 2).Memory; memory.Through != 0 || len(memory.Entries) != 0 {
				t.Fatal("cancelled memory updated current state", memory)
			}
		})
	}
}

func seedMemoryHistory(s *store.Store, sid string) {
	messages := []map[string]any{{"role": "user", "content": "每天十五分钟，预算三百元。"}, {"role": "assistant", "content": "记住了。"}}
	for i := 0; i < 4; i++ {
		messages = append(messages, map[string]any{"role": "user", "content": "换个话题。"}, map[string]any{"role": "assistant", "content": "好的。"})
	}
	s.AddHistory(sid, messages)
}
