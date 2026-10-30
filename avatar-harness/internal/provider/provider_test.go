package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"avatarharness/internal/config"
)

func TestFastAPIContractAndSchema(t *testing.T) {
	var calls atomic.Int32
	t.Setenv("PROVIDER_TEST_KEY", "test-only")
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var request Request
		if json.NewDecoder(r.Body).Decode(&request) != nil {
			t.Error("bad JSON")
		}
		if request.Capability != "stats" || request.Context["turn_id"] != "turn-test" || r.Header.Get("Authorization") != "Bearer test-only" {
			t.Error("bad provider contract")
		}
		_ = json.NewEncoder(w).Encode(OK(map[string]any{"characters": len([]rune(String(request.Input["text"])))}))
	}))
	defer remote.Close()
	c := config.Default()
	c.Tools = append(c.Tools, config.Tool{Name: "stats", Kind: "fastapi", Description: "test", BaseURL: remote.URL, APIKeyEnv: "PROVIDER_TEST_KEY", TimeoutMS: 500, Parameters: map[string]any{"type": "object", "properties": map[string]any{"text": map[string]any{"type": "string"}}, "required": []any{"text"}, "additionalProperties": false}})
	bus, err := New(&c, nil)
	if err != nil {
		t.Fatal(err)
	}
	result := bus.Invoke(context.Background(), "stats", map[string]any{"text": "中文测试"}, map[string]any{"turn_id": "turn-test"})
	if result.Status != "ok" || Number(result.Output["characters"]) != 4 {
		t.Fatal(result)
	}
	if bus.Invoke(context.Background(), "stats", map[string]any{"text": 12}, nil).Status != "error" || calls.Load() != 1 {
		t.Fatal("invalid schema executed")
	}
}
func TestInternalAndHTTPTimeoutCancel(t *testing.T) {
	c := config.Default()
	c.Providers["asr"] = config.Provider{Kind: "internal", Factory: "slow", TimeoutMS: 20}
	b, _ := New(&c, nil)
	b.Register("slow", func(ctx context.Context, r Request, e Emit) (Result, error) { <-ctx.Done(); return Result{}, ctx.Err() })
	started := time.Now()
	result := b.Invoke(context.Background(), "asr", map[string]any{}, nil)
	if result.Status != "timeout" || time.Since(started) > time.Second {
		t.Fatal(result)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if b.Invoke(ctx, "asr", map[string]any{}, nil).Status != "cancelled" {
		t.Fatal("cancel not propagated")
	}
}
func TestOpenAIFragmentedToolsAndNoToolsFinal(t *testing.T) {
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if _, ok := body["tools"]; !ok {
			t.Error("tools missing")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, delta := range []map[string]any{{"tool_calls": []any{map[string]any{"index": 0, "id": "c1", "function": map[string]any{"name": "get_current_time", "arguments": `{"time`}}}}, {"tool_calls": []any{map[string]any{"index": 0, "function": map[string]any{"arguments": `zone":"Asia/Shanghai"}`}}}}} {
			raw, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": delta}}})
			w.Write([]byte("data: " + string(raw) + "\n\n"))
		}
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer remote.Close()
	c := config.Default()
	c.Providers["llm"] = config.Provider{Kind: "openai", BaseURL: remote.URL, Model: "protocol-fixture", TimeoutMS: 1000}
	b, _ := New(&c, nil)
	var calls []any
	err := b.LLM(context.Background(), map[string]any{"messages": []any{}, "tools": b.Spec()}, nil, func(e map[string]any) error {
		if e["type"] == "done" {
			calls = e["tool_calls"].([]any)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	fn := calls[0].(map[string]any)["function"].(map[string]any)
	if fn["arguments"] != `{"timezone":"Asia/Shanghai"}` {
		t.Fatal(fn)
	}
}
func TestFastAPIStreaming(t *testing.T) {
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/stream" {
			t.Error(r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Write([]byte("{\"type\":\"text\",\"text\":\"真实协议流\"}\n{\"type\":\"done\",\"tool_calls\":[]}\n"))
	}))
	defer remote.Close()
	c := config.Default()
	c.Providers["llm"] = config.Provider{Kind: "fastapi", BaseURL: remote.URL, Streaming: true, TimeoutMS: 1000}
	b, _ := New(&c, nil)
	var output strings.Builder
	err := b.LLM(context.Background(), map[string]any{}, nil, func(e map[string]any) error { output.WriteString(String(e["text"])); return nil })
	if err != nil || output.String() != "真实协议流" {
		t.Fatal(err, output.String())
	}
}

func TestLateInternalStreamCannotWriteAfterCancellation(t *testing.T) {
	c := config.Default()
	c.Providers["tts"] = config.Provider{Kind: "internal", Factory: "late-fixture", Streaming: true, TimeoutMS: 20}
	b, _ := New(&c, nil)
	release := make(chan struct{})
	finished := make(chan struct{})
	var delivered atomic.Int32
	b.Register("late-fixture", func(ctx context.Context, r Request, e Emit) (Result, error) {
		<-release
		_ = e(map[string]any{"type": "audio", "output": map[string]any{"audio_base64": "AAAA"}})
		close(finished)
		return OK(nil), nil
	})
	err := b.Stream(context.Background(), "tts", map[string]any{}, nil, func(e map[string]any) error { delivered.Add(1); return nil })
	if err == nil {
		t.Fatal("expected timeout")
	}
	close(release)
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("fixture did not finish")
	}
	if delivered.Load() != 0 {
		t.Fatal("late output delivered")
	}
}
