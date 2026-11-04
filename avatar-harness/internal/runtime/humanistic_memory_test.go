package runtime

import (
	"encoding/json"
	"strings"
	"testing"

	"avatarharness/internal/provider"
	"avatarharness/internal/store"
)

func TestHumanisticCorrectionSuppressesOnlyReplacedPhrase(t *testing.T) {
	c := store.Conversation{Archived: []map[string]any{
		{"role": "user", "content": "预算三百元，每天十五分钟。我喜欢边聊边分析。"},
		{"role": "assistant", "content": "需要我帮你分析吗？"},
		{"role": "user", "content": "预算改成五百元。这次先听我说，不要分析。"},
	}}
	raw := `{"entries":[{"kind":"constraint","message":1,"quote":"预算三百元"},{"kind":"constraint","message":1,"quote":"每天十五分钟"},{"kind":"preference","message":1,"quote":"我喜欢边聊边分析"},{"kind":"correction","message":3,"quote":"预算改成五百元","replaces":[{"message":1,"quote":"预算三百元"}]},{"kind":"correction","message":3,"quote":"这次先听我说，不要分析","replaces":[{"message":1,"quote":"我喜欢边聊边分析"}]},{"kind":"support_preference","message":3,"quote":"这次先听我说，不要分析"}]}`
	memory, err := selectMemory(raw, c, 3)
	if err != nil {
		t.Fatal(err)
	}
	c.Memory = memory
	message := (&Runtime{}).memoryMessage(c, "我的预算、时间和交流偏好是什么？")
	text := provider.String(message["content"])
	var decoded struct {
		Entries []store.MemoryEntry `json:"entries"`
		Archive []store.MemoryEntry `json:"archive_quotes"`
	}
	if err := json.Unmarshal([]byte(text), &decoded); err != nil {
		t.Fatal(err)
	}
	// Reference metadata deliberately contains the old phrase; effective quotes
	// must not. Unchanged constraints from the same message must survive.
	quotes := []string{}
	for _, e := range append(decoded.Entries, decoded.Archive...) {
		quotes = append(quotes, e.Quote)
	}
	joined := strings.Join(quotes, "\n")
	if strings.Contains(joined, "预算三百元") || strings.Contains(joined, "我喜欢边聊边分析") || !strings.Contains(joined, "每天十五分钟") || !strings.Contains(joined, "不要分析") || len(memory.Superseded) != 2 {
		t.Fatal("correction leaked or erased an unaffected constraint", quotes, memory)
	}
	// A bounded selector dropping a correction cannot resurrect old utterances.
	next, err := selectMemory(`{"entries":[]}`, c, 3)
	if err != nil {
		t.Fatal(err)
	}
	c.Memory = next
	for _, entry := range archivedMemory(c, "预算三百元 边聊边分析 时间") {
		if strings.Contains(entry.Quote, "预算三百元") || strings.Contains(entry.Quote, "我喜欢边聊边分析") {
			t.Fatal("dropped correction resurrected old memory", entry)
		}
	}
}

func TestHumanisticCorrectionRejectsForgedOrFutureReferences(t *testing.T) {
	c := store.Conversation{Archived: []map[string]any{
		{"role": "user", "content": "我愿意探索。"},
		{"role": "assistant", "content": "你是不是害怕失败？"},
		{"role": "user", "content": "不是害怕失败，是没时间。这次不要分析。"},
	}}
	for _, raw := range []string{
		`{"entries":[{"kind":"correction","message":3,"quote":"不是害怕失败，是没时间","replaces":[{"message":2,"quote":"害怕失败"}]}]}`,
		`{"entries":[{"kind":"correction","message":1,"quote":"我愿意探索","replaces":[{"message":3,"quote":"没时间"}]}]}`,
		`{"entries":[{"kind":"correction","message":3,"quote":"不是害怕失败，是没时间","replaces":[{"message":1,"quote":"我不愿意探索"}]}]}`,
		`{"entries":[{"kind":"support_preference","message":3,"quote":"这次不要分析","replaces":[{"message":1,"quote":"我愿意探索"}]}]}`,
	} {
		if _, err := selectMemory(raw, c, 3); err == nil {
			t.Fatal("invalid correction accepted", raw)
		}
	}
	// Correcting the assistant does not require inventing an old user quote.
	mem, err := selectMemory(`{"entries":[{"kind":"correction","message":3,"quote":"不是害怕失败，是没时间"}]}`, c, 3)
	if err != nil || len(mem.Entries) != 1 || mem.Entries[0].Context != "你是不是害怕失败？" {
		t.Fatal(mem, err)
	}
}

func TestHumanisticCorrectionFiltersOverlappingQuotesByOriginalSource(t *testing.T) {
	c := store.Conversation{
		Archived: []map[string]any{{"role": "user", "content": "原先预算三百元，每天十五分钟。"}},
		Recent:   []map[string]any{{"role": "user", "content": "现在不再用三百元这个预算。"}},
		Memory: store.SessionMemory{
			Entries: []store.MemoryEntry{
				{Kind: "constraint", Message: 1, Quote: "三百元"},
				{Kind: "constraint", Message: 1, Quote: "原先预算三百"},
				{Kind: "constraint", Message: 1, Quote: "每天十五分钟"},
				{Kind: "correction", Message: 2, Quote: "现在不再用三百元这个预算"},
			},
			Superseded: []store.MemoryReference{{Message: 1, Quote: "预算三百元"}},
		},
	}
	entries := activeMemoryEntries(c)
	if len(entries) != 2 || entries[0].Quote != "每天十五分钟" || entries[1].Kind != "correction" {
		t.Fatal("overlap leaked or a recent/unaffected quote was erased", entries)
	}
}
