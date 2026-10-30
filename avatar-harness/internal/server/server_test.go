package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"avatarharness/internal/config"
	"avatarharness/internal/provider"
	"avatarharness/internal/store"
)

func TestLAMZipAssetIsServedOnlyForLAMRenderer(t *testing.T) {
	for _, kind := range []string{"lam", "scene"} {
		t.Run(kind, func(t *testing.T) {
			c := config.Default()
			p := filepath.Join(t.TempDir(), "avatar.zip")
			if err := os.WriteFile(p, []byte("PK asset fixture"), 0600); err != nil {
				t.Fatal(err)
			}
			c.Characters[0].AssetPath = p
			c.Characters[0].Renderer = config.Renderer{Kind: kind}
			a := setup(t, &c)
			res := request(t, a, "GET", "/api/characters/"+c.Characters[0].ID+"/asset/avatar.zip", nil)
			if kind == "lam" {
				if res.Code != 200 || res.Header().Get("Content-Type") != "application/zip" || res.Body.String() != "PK asset fixture" {
					t.Fatal(res.Code, res.Body.String())
				}
			} else if res.Code != 422 {
				t.Fatal(res.Code)
			}
		})
	}
}

func setup(t *testing.T, c *config.Config) *App {
	t.Helper()
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	b, err := provider.New(c, nil)
	if err != nil {
		t.Fatal(err)
	}
	a := New(c, s, b)
	t.Cleanup(func() {
		a.Close()
		if err = s.Close(); err != nil {
			t.Error(err)
		}
	})
	return a
}
func request(t *testing.T, a *App, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(body)
	r := httptest.NewRequest(method, path, bytes.NewReader(raw))
	w := httptest.NewRecorder()
	a.Handler.ServeHTTP(w, r)
	return w
}
func run(t *testing.T, a *App, sid, text string) []store.Event {
	t.Helper()
	turn, err := a.Runtime.Start(sid, text, "text", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-turn.Done:
	case <-time.After(3 * time.Second):
		t.Fatal("turn timeout")
	}
	return a.Runtime.Store.Events(sid, 0)
}
func streamDelta(w http.ResponseWriter, delta map[string]any) {
	raw, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": delta}}})
	fmt.Fprintf(w, "data: %s\n\n", raw)
}
func toolModel(t *testing.T, invalid bool) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var count atomic.Int32
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "text/event-stream")
		if _, ok := body["tools"]; ok {
			streamDelta(w, map[string]any{"content": "工具前言，不能先说出口。"})
			arguments := `{"timezone":"Asia/Shanghai"}`
			if invalid {
				arguments = `{"broken"`
			}
			streamDelta(w, map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "clock-call", "function": map[string]any{"name": "get_current_time", "arguments": arguments[:5]}}}})
			streamDelta(w, map[string]any{"tool_calls": []any{map[string]any{"index": 0, "function": map[string]any{"arguments": arguments[5:]}}}})
		} else {
			messages := body["messages"].([]any)
			last := messages[len(messages)-1].(map[string]any)
			var result provider.Result
			_ = json.Unmarshal([]byte(last["content"].(string)), &result)
			if invalid {
				if result.Status != "error" || !strings.Contains(result.Error, "参数解析失败") {
					t.Error(result)
				}
				streamDelta(w, map[string]any{"content": "参数无效，未执行工具。"})
			} else {
				if result.Status != "ok" {
					t.Error(result)
				}
				streamDelta(w, map[string]any{"content": "实际时间：" + provider.String(result.Output["datetime"])})
			}
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	return remote, &count
}
func TestLocalToolsAndNoFakeUnconfiguredAnswer(t *testing.T) {
	c := config.Default()
	a := setup(t, &c)
	sid := a.Runtime.Store.Create(c.Characters[0].ID).ID
	events := run(t, a, sid, `/tool get_current_time {"timezone":"Asia/Shanghai"}`)
	found := false
	for _, event := range events {
		if event.Type == "tool.finished" {
			out := event.Data["output"].(map[string]any)
			got, err := time.Parse(time.RFC3339Nano, provider.String(out["datetime"]))
			if err != nil || time.Since(got) > 3*time.Second {
				t.Fatal(out)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("tool not executed")
	}
	sid = a.Runtime.Store.Create(c.Characters[0].ID).ID
	events = run(t, a, sid, "你好")
	errorFound := false
	for _, event := range events {
		if event.Type == "assistant.message" || event.Type == "pipeline.audio" {
			t.Fatal("fake model answer")
		}
		if event.Type == "turn.error" {
			errorFound = true
		}
	}
	if !errorFound {
		t.Fatal("unconfigured model not reported")
	}
	health := request(t, a, "GET", "/api/health", nil)
	if !strings.Contains(health.Body.String(), `"runtime":"go"`) {
		t.Fatal(health.Body.String())
	}
}
func TestRealToolLoopAndMalformedArguments(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		t.Run(fmt.Sprint(invalid), func(t *testing.T) {
			remote, count := toolModel(t, invalid)
			defer remote.Close()
			c := config.Default()
			c.Providers["llm"] = config.Provider{Kind: "openai", BaseURL: remote.URL, Model: "protocol-test-only", TimeoutMS: 1000}
			a := setup(t, &c)
			sid := a.Runtime.Store.Create(c.Characters[0].ID).ID
			events := run(t, a, sid, "现在几点？")
			answer := ""
			started := 0
			for _, event := range events {
				if event.Type == "tool.started" {
					started++
				}
				if event.Type == "assistant.message" {
					answer = provider.String(event.Data["text"])
				}
			}
			if count.Load() != 2 || answer == "" || strings.Contains(answer, "工具前言") {
				t.Fatal(count.Load(), answer)
			}
			if invalid && started != 0 {
				t.Fatal("malformed call executed")
			}
			history := a.Runtime.Store.History(sid, 20)
			if len(history) != 4 || history[1]["role"] != "assistant" || history[2]["role"] != "tool" {
				t.Fatal(history)
			}
		})
	}
}
func TestOACBridgeWithAndWithoutTools(t *testing.T) {
	for _, withTools := range []bool{false, true} {
		t.Run(fmt.Sprint(withTools), func(t *testing.T) {
			var remote *httptest.Server
			if withTools {
				remote, _ = toolModel(t, false)
			} else {
				remote = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					streamDelta(w, map[string]any{"content": "你好，可以开始了。"})
					fmt.Fprint(w, "data: [DONE]\n\n")
				}))
			}
			defer remote.Close()
			c := config.Default()
			c.Providers["llm"] = config.Provider{Kind: "openai", BaseURL: remote.URL, Model: "protocol-test-only", TimeoutMS: 1000}
			a := setup(t, &c)
			response := request(t, a, "POST", "/oac/v1/chat/completions", map[string]any{"stream": true, "messages": []any{map[string]any{"role": "user", "content": "你好"}}})
			text := response.Body.String()
			if response.Code != 200 || strings.Contains(text, "工具前言") || !strings.Contains(text, "data: [DONE]") {
				t.Fatal(response.Code, text)
			}
			if !withTools && strings.Count(text, "你好，可以开始了。") != 1 {
				t.Fatal(text)
			}
		})
	}
}
func TestSSELiveAudioAndShutdown(t *testing.T) {
	c := config.Default()
	a := setup(t, &c)
	sid := a.Runtime.Store.Create(c.Characters[0].ID).ID
	httpServer := httptest.NewServer(a.Handler)
	defer httpServer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", httpServer.URL+"/api/sessions/"+sid+"/events", nil)
	res, err := httpServer.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	a.Runtime.Store.Emit(sid, "live", "pipeline.audio", map[string]any{"audio_base64": "AAAA", "pts_ms": 0})
	scanner := bufio.NewScanner(res.Body)
	found := false
	for scanner.Scan() {
		if strings.Contains(scanner.Text(), "audio_base64") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("live audio missing")
	}
	if data := a.Runtime.Store.Events(sid, 0)[0].Data; data["audio_base64"] != nil {
		t.Fatal("persisted audio")
	}
	a.cancel()
	for scanner.Scan() {
	}
	if ctx.Err() != nil {
		t.Fatal("SSE did not close on shutdown")
	}
}
func TestAvatarDescribeAPIAndConfiguredStateRedaction(t *testing.T) {
	t.Setenv("AVATAR_TEST_KEY", "test-only-not-real")
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body provider.Request
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Capability != "avatar" || body.Input["operation"] != "describe" {
			t.Error(body)
		}
		_ = json.NewEncoder(w).Encode(provider.OK(map[string]any{"renderer": map[string]any{"kind": "scene", "asset_url": "https://assets.example/avatar.glb"}}))
	}))
	defer remote.Close()
	c := config.Default()
	c.Providers["avatar"] = config.Provider{Kind: "fastapi", BaseURL: remote.URL, APIKeyEnv: "AVATAR_TEST_KEY", TimeoutMS: 1000}
	a := setup(t, &c)
	sid := a.Runtime.Store.Create(c.Characters[0].ID).ID
	res := request(t, a, "POST", "/api/sessions/"+sid+"/avatar", map[string]any{})
	if res.Code != 200 || !strings.Contains(res.Body.String(), "avatar.glb") {
		t.Fatal(res.Code, res.Body.String())
	}
	state := request(t, a, "GET", "/api/state", nil)
	if strings.Contains(state.Body.String(), "test-only-not-real") {
		t.Fatal("secret exposed")
	}
}
func TestCancelledASRDoesNotStartLateTurn(t *testing.T) {
	c := config.Default()
	c.Providers["asr"] = config.Provider{Kind: "internal", Factory: "asr-fixture", TimeoutMS: 1000}
	a := setup(t, &c)
	started := make(chan struct{})
	a.Runtime.Bus.Register("asr-fixture", func(ctx context.Context, req provider.Request, emit provider.Emit) (provider.Result, error) {
		close(started)
		<-ctx.Done()
		return provider.Result{}, ctx.Err()
	})
	sid := a.Runtime.Store.Create(c.Characters[0].ID).ID
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest("POST", "/api/sessions/"+sid+"/audio", strings.NewReader(`{"input":{"audio_base64":"AAAA"}}`)).WithContext(ctx)
	done := make(chan struct{})
	go func() { a.Handler.ServeHTTP(httptest.NewRecorder(), req); close(done) }()
	<-started
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("ASR cancel timed out")
	}
	for _, e := range a.Runtime.Store.Events(sid, 0) {
		if e.Type == "turn.started" {
			t.Fatal("late ASR started a turn")
		}
	}
}

type deadlineWriter struct {
	*httptest.ResponseRecorder
	deadlines []time.Time
}

func (w *deadlineWriter) SetWriteDeadline(value time.Time) error {
	w.deadlines = append(w.deadlines, value)
	return nil
}
func TestSSEWriteDeadlineDoesNotExpireAnIdleConnection(t *testing.T) {
	w := &deadlineWriter{ResponseRecorder: httptest.NewRecorder()}
	if err := send(w, store.Event{Seq: 1, Type: "diagnostic", Data: map[string]any{}}); err != nil {
		t.Fatal(err)
	}
	if len(w.deadlines) != 2 || !w.deadlines[1].IsZero() {
		t.Fatal("deadline left on idle SSE connection")
	}
}
