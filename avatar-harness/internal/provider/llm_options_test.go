package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"avatarharness/internal/config"
)

func TestLLMRequestOptionsAreIsolatedAndCapped(t *testing.T) {
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		messages := body["messages"].([]any)
		internal := messages[0].(map[string]any)["content"] == "internal"
		if internal {
			if body["enable_thinking"] != false || Number(body["max_tokens"]) != 128 || body["reasoning_effort"] != nil {
				t.Error("internal options not applied", body)
			}
		} else if body["enable_thinking"] != true || Number(body["max_tokens"]) != 2048 || body["reasoning_effort"] != "low" {
			t.Error("main request changed", body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer remote.Close()
	c := config.Default()
	c.Providers["llm"] = config.Provider{Kind: "openai", BaseURL: remote.URL, Model: "fixture", TimeoutMS: 1000, Parameters: map[string]any{"enable_thinking": true, "reasoning_effort": "low", "max_tokens": 2048}}
	b, _ := New(&c, nil)
	var wg sync.WaitGroup
	for _, internal := range []bool{true, false, true, false} {
		wg.Add(1)
		go func(internal bool) {
			defer wg.Done()
			label := "main"
			options := LLMOptions{}
			if internal {
				label = "internal"
				options = LLMOptions{DisableThinking: true, MaxTokens: 128}
			}
			if err := b.LLMWithOptions(context.Background(), map[string]any{"messages": []map[string]any{{"role": "user", "content": label}}}, nil, options, func(map[string]any) error { return nil }); err != nil {
				t.Error(err)
			}
		}(internal)
	}
	wg.Wait()
	if c.Providers["llm"].Parameters["enable_thinking"] != true || c.Providers["llm"].Parameters["max_tokens"] != 2048 {
		t.Fatal("configuration mutated")
	}
}
