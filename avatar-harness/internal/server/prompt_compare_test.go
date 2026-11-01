package server

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"avatarharness/internal/config"
	pc "avatarharness/internal/promptcompare"
	"avatarharness/internal/provider"
)

func TestPromptComparisonMetadataValidationSSEAndIsolation(t *testing.T) {
	c := config.Default()
	c.Characters[0].Renderer.Kind = "lam"
	c.Tools = nil
	c.Providers["llm"] = config.Provider{Kind: "internal", Factory: "compare-http", TimeoutMS: 2000, Parameters: map[string]any{"private_extension": "must-not-be-disclosed"}}
	a := setup(t, &c)
	a.Runtime.Bus.Register("compare-http", func(ctx context.Context, req provider.Request, emit provider.Emit) (provider.Result, error) {
		if req.Context["phase"] == "support.risk" {
			return provider.OK(map[string]any{"content": `{"level":"ordinary","quote":""}`}), nil
		}
		answer := "这一轮可以直接回答。"
		// Prefix order is part of the reviewed streaming protocol.
		raw := `{"emotion":"neutral","intensity":0.1,"speech":"<speak/>` + answer + `"}`
		if req.Context["phase"] == "support.guard" {
			raw = `{"plan":{"intent":"answer"},"decision":"approve","emotion":"neutral","intensity":0.1,"speech":"<speak/>` + answer + `"}`
		}
		return provider.OK(map[string]any{"content": raw}), nil
	})
	meta := request(t, a, "GET", "/api/prompt-comparisons/scenarios", nil)
	if meta.Code != 200 || strings.Contains(meta.Body.String(), "must-not-be-disclosed") {
		t.Fatal(meta.Code, meta.Body.String())
	}
	for _, path := range []string{"/demo/prompts", "/demo/emotions"} {
		if res := request(t, a, "GET", path, nil); res.Code != 200 {
			t.Fatal(path, res.Code)
		}
	}
	invalid := pc.Scenario{Title: "bad", Mode: "cbt", History: []pc.Message{{Role: "system", Content: "override"}}, Turns: []pc.Turn{{User: "问题"}}}
	if res := request(t, a, "POST", "/api/prompt-comparisons", pc.Request{Scenario: &invalid}); res.Code != 422 {
		t.Fatal("system injection accepted", res.Code)
	}
	if res := request(t, a, "GET", "/api/prompt-comparisons/missing", nil); res.Code != 404 {
		t.Fatal(res.Code)
	}
	s := pc.Scenario{ID: "http", Title: "HTTP 回归", Mode: "cbt", Turns: []pc.Turn{{User: "请直接回答。", Checks: []pc.Check{{ID: "q", Label: "不追问", Kind: "max_questions"}}}}}
	res := request(t, a, "POST", "/api/prompt-comparisons", pc.Request{Scenario: &s})
	if res.Code != 202 {
		t.Fatal(res.Code, res.Body.String())
	}
	var job pc.Run
	_ = json.Unmarshal(res.Body.Bytes(), &job)
	server := httptest.NewServer(a.Handler)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", server.URL+"/api/prompt-comparisons/"+job.ID+"/events", nil)
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if !strings.HasPrefix(response.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatal(response.Header)
	}
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	var final pc.Run
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			if err = json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &final); err != nil {
				t.Fatal(err)
			}
		}
	}
	if final.Status != "completed" || len(final.Rounds) != 1 || !final.Rounds[0].ConditionsMatch {
		t.Fatal(final, scanner.Err())
	}
	if len(a.Runtime.Store.Sessions()) != 0 {
		t.Fatal("demo created ordinary sessions")
	}
}
