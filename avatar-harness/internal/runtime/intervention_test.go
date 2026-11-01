package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"avatarharness/internal/config"
	"avatarharness/internal/intervention"
	"avatarharness/internal/provider"
	"avatarharness/internal/store"
)

func supportFixture(t *testing.T, fn provider.Internal, tools ...bool) (*Runtime, string) {
	t.Helper()
	c := config.Default()
	c.Characters[0].Renderer.Kind = "lam"
	if len(tools) == 0 || !tools[0] {
		c.Tools = nil
	}
	c.Limits.MaxTurnMS = 10000
	c.Providers["llm"] = config.Provider{Kind: "internal", Factory: "support-fixture", TimeoutMS: 5000}
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	b, err := provider.New(&c, nil)
	if err != nil {
		t.Fatal(err)
	}
	b.Register("support-fixture", fn)
	r := New(&c, s, b)
	t.Cleanup(func() { r.Shutdown(); s.Close() })
	sid := s.Create(c.Characters[0].ID).ID
	if _, err = r.SupportCommand(sid, intervention.Command{Operation: "start", Mode: intervention.CBT}); err != nil {
		t.Fatal(err)
	}
	return r, sid
}
func content(value string) (provider.Result, error) {
	return provider.OK(map[string]any{"content": value}), nil
}
func ordinaryPlan(intent string) string {
	raw, _ := json.Marshal(intervention.Plan{Risk: "ordinary", Intent: intent})
	return string(raw)
}
func awaitTurn(t *testing.T, turn *Turn) {
	t.Helper()
	select {
	case <-turn.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("turn did not finish")
	}
}

func composeFixture(_ intervention.Plan, text string) string {
	raw, _ := json.Marshal(struct {
		Emotion   string  `json:"emotion"`
		Intensity float64 `json:"intensity"`
		Speech    string  `json:"speech"`
	}{"neutral", .1, answerMarker + text})
	return string(raw)
}
func guardFixture(decision, text string) string {
	return guardPlanFixture(intervention.Plan{Intent: "answer"}, decision, text)
}
func guardPlanFixture(plan intervention.Plan, decision, text string) string {
	raw, _ := json.Marshal(struct {
		Plan      intervention.Plan `json:"plan"`
		Decision  string            `json:"decision"`
		Emotion   string            `json:"emotion"`
		Intensity float64           `json:"intensity"`
		Speech    string            `json:"speech"`
	}{plan, decision, "neutral", .1, answerMarker + text})
	return string(raw)
}
func streamFixture(emit provider.Emit, text string, calls []any) (provider.Result, error) {
	if text != "" {
		if err := emit(map[string]any{"type": "text", "text": text}); err != nil {
			return provider.Result{}, err
		}
	}
	if err := emit(map[string]any{"type": "done", "tool_calls": calls}); err != nil {
		return provider.Result{}, err
	}
	return provider.OK(nil), nil
}
func waitSignal(t *testing.T, ch <-chan struct{}, turn *Turn, label string) {
	t.Helper()
	select {
	case <-ch:
	case <-turn.Done:
		t.Fatal(label, "turn finished before signal")
	case <-time.After(2 * time.Second):
		t.Fatal(label, "timeout")
	}
}
func finalSupportText(r *Runtime, sid string) string {
	for _, e := range r.Store.Events(sid, 0) {
		if e.Type == "assistant.message" {
			return provider.String(e.Data["text"])
		}
	}
	return ""
}

func TestSupportRiskAndComposeOverlapWithoutEarlyRelease(t *testing.T) {
	riskEntered, composeEntered, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	answer := "你希望先把今天的经历讲清楚，我在听。"
	var guarded atomic.Int32
	r, sid := supportFixture(t, func(ctx context.Context, req provider.Request, emit provider.Emit) (provider.Result, error) {
		switch req.Context["phase"] {
		case "support.risk":
			close(riskEntered)
			select {
			case <-release:
			case <-ctx.Done():
				return provider.Result{}, ctx.Err()
			}
			return content(`{"level":"ordinary","quote":""}`)
		case "support.compose":
			close(composeEntered)
			return content(composeFixture(intervention.Plan{Intent: "listen"}, answer))
		case "support.guard":
			guarded.Add(1)
			return content(guardFixture("approve", answer))
		default:
			return provider.Result{}, fmt.Errorf("unexpected phase %v", req.Context["phase"])
		}
	})
	turn, _ := r.Start(sid, "我先想讲讲今天的经历。", "text", false, nil)
	waitSignal(t, riskEntered, turn, "risk")
	waitSignal(t, composeEntered, turn, "compose")
	for _, e := range r.Store.Events(sid, 0) {
		if e.Type == "assistant.delta" || e.Type == "assistant.message" || e.Type == "pipeline.expression" || e.Type == "pipeline.audio" {
			t.Fatal("risk pending leaked output", e)
		}
	}
	if guarded.Load() != 0 {
		t.Fatal("guard ran before risk result")
	}
	close(release)
	awaitTurn(t, turn)
	if finalSupportText(r, sid) != answer || len(r.Store.History(sid, 20)) != 2 {
		t.Fatal(r.Store.Events(sid, 0))
	}
}

func TestSupportApprovedSpeechStreamsBeforeGuardFinishes(t *testing.T) {
	first := "你希望把今天的具体经历讲清楚，我会按你愿意的节奏听。"
	tail := "接下来的部分也由你决定。"
	answer := first + tail
	guardStarted, finish, spoken := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	r, sid := supportFixture(t, func(ctx context.Context, req provider.Request, emit provider.Emit) (provider.Result, error) {
		switch req.Context["phase"] {
		case "support.risk":
			return streamFixture(emit, `{"level":"ordinary","quote":""}`, nil)
		case "support.compose":
			return streamFixture(emit, composeFixture(intervention.Plan{Intent: "listen"}, answer), nil)
		case "support.guard":
			if err := emit(map[string]any{"type": "text", "text": `{"plan":{"intent":"answer"},"decision":"approve","emotion":"neutral","intensity":0.1,"speech":"<speak/>` + first}); err != nil {
				return provider.Result{}, err
			}
			close(guardStarted)
			select {
			case <-finish:
			case <-ctx.Done():
				return provider.Result{}, ctx.Err()
			}
			return streamFixture(emit, tail+`"}`, nil)
		}
		return provider.Result{}, fmt.Errorf("unexpected phase")
	})
	p := r.Config.Providers["llm"]
	p.Streaming = true
	r.Config.Providers["llm"] = p
	r.Config.Providers["tts"] = config.Provider{Kind: "internal", Factory: "stream-speech-spy", TimeoutMS: 1000}
	r.Bus.Register("stream-speech-spy", func(ctx context.Context, req provider.Request, emit provider.Emit) (provider.Result, error) {
		once.Do(func() { close(spoken) })
		return provider.OK(nil), nil
	})
	turn, _ := r.Start(sid, "先听我说。", "text", true, nil)
	waitSignal(t, guardStarted, turn, "first approved sentence")
	waitSignal(t, spoken, turn, "TTS before guard completion")
	if len(r.Store.History(sid, 20)) != 0 {
		t.Fatal("unfinished answer committed")
	}
	delta := ""
	for _, e := range r.Store.Events(sid, 0) {
		if e.Type == "assistant.delta" {
			delta += provider.String(e.Data["text"])
		}
	}
	if delta != first {
		t.Fatal("wrong streamed prefix", delta)
	}
	close(finish)
	awaitTurn(t, turn)
	if finalSupportText(r, sid) != answer || len(r.Store.History(sid, 20)) != 2 {
		t.Fatal("stream not committed exactly once", r.Store.Events(sid, 0))
	}
}

func TestSupportRevisionUsesOneGuardAndHoldsReplacementUntilComplete(t *testing.T) {
	entered, finish := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	r, sid := supportFixture(t, func(ctx context.Context, req provider.Request, emit provider.Emit) (provider.Result, error) {
		calls.Add(1)
		switch req.Context["phase"] {
		case "support.risk":
			return streamFixture(emit, `{"level":"ordinary","quote":""}`, nil)
		case "support.compose":
			return streamFixture(emit, composeFixture(intervention.Plan{Intent: "answer"}, "因为准备不足才会失败。"), nil)
		case "support.guard":
			if err := emit(map[string]any{"type": "text", "text": `{"plan":{"intent":"answer"},"decision":"revise","emotion":"neutral","intensity":0.1,"speech":"<speak/>答案是`}); err != nil {
				return provider.Result{}, err
			}
			close(entered)
			select {
			case <-finish:
			case <-ctx.Done():
				return provider.Result{}, ctx.Err()
			}
			return streamFixture(emit, `十二。"}`, nil)
		}
		return provider.Result{}, fmt.Errorf("extra model call")
	})
	p := r.Config.Providers["llm"]
	p.Streaming = true
	r.Config.Providers["llm"] = p
	turn, _ := r.Start(sid, "直接回答三乘四。", "text", false, nil)
	waitSignal(t, entered, turn, "revision")
	for _, e := range r.Store.Events(sid, 0) {
		if e.Type == "assistant.delta" || e.Type == "pipeline.expression" {
			t.Fatal("unvalidated replacement escaped", e)
		}
	}
	close(finish)
	awaitTurn(t, turn)
	if finalSupportText(r, sid) != "答案是十二。" || calls.Load() != 3 {
		t.Fatal("revision added a serial loop", calls.Load(), r.Store.Events(sid, 0))
	}
	if strings.Contains(provider.String(r.Store.History(sid, 20)[1]["content"]), "准备不足") {
		t.Fatal("rejected inference entered history")
	}
}

func TestSupportUrgentRiskCancelsSpeculativeAnswer(t *testing.T) {
	started, cancelled := make(chan struct{}), make(chan struct{})
	r, sid := supportFixture(t, func(ctx context.Context, req provider.Request, emit provider.Emit) (provider.Result, error) {
		switch req.Context["phase"] {
		case "support.risk":
			<-started
			return content(`{"level":"urgent","quote":"我现在可能伤害自己"}`)
		case "support.compose":
			close(started)
			<-ctx.Done()
			close(cancelled)
			return provider.Result{}, ctx.Err()
		default:
			t.Error("ordinary content guard must not run")
			return provider.Result{}, fmt.Errorf("unexpected call")
		}
	})
	turn, _ := r.Start(sid, "我现在可能伤害自己", "text", false, nil)
	awaitTurn(t, turn)
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("speculation was not cancelled")
	}
	if finalSupportText(r, sid) != intervention.UrgentResponse || r.Store.Intervention(sid).Status != "paused" {
		t.Fatal(r.Store.Events(sid, 0))
	}
}

func TestSupportRiskFailureClosesGate(t *testing.T) {
	r, sid := supportFixture(t, func(ctx context.Context, req provider.Request, emit provider.Emit) (provider.Result, error) {
		if req.Context["phase"] == "support.risk" {
			return content(`{"level":"unknown"}`)
		}
		return content(composeFixture(intervention.Plan{Intent: "listen"}, "不应释放这条候选。"))
	})
	turn, _ := r.Start(sid, "先听我说。", "text", false, nil)
	awaitTurn(t, turn)
	if strings.Contains(finalSupportText(r, sid), "不应释放") || r.Store.Intervention(sid).Status != "paused" {
		t.Fatal(r.Store.Events(sid, 0))
	}
	for _, e := range r.Store.Events(sid, 0) {
		if e.Type == "assistant.delta" {
			t.Fatal("risk failure released candidate")
		}
	}
}

func TestSupportCancelDropsLateParallelResults(t *testing.T) {
	entered, release, late := make(chan struct{}), make(chan struct{}), make(chan struct{})
	r, sid := supportFixture(t, func(ctx context.Context, req provider.Request, emit provider.Emit) (provider.Result, error) {
		if req.Context["phase"] == "support.risk" {
			close(entered)
			<-release
			defer close(late)
			return content(`{"level":"ordinary","quote":""}`)
		}
		return content(composeFixture(intervention.Plan{Intent: "listen"}, "旧轮候选。"))
	})
	turn, _ := r.Start(sid, "先说说。", "text", false, nil)
	waitSignal(t, entered, turn, "risk")
	before := r.Store.Intervention(sid)
	r.Cancel(sid, "test cancel")
	close(release)
	awaitTurn(t, turn)
	<-late
	if len(r.Store.History(sid, 20)) != 0 || r.Store.Intervention(sid).Version != before.Version {
		t.Fatal("cancelled work committed")
	}
	for _, e := range r.Store.Events(sid, 0) {
		if e.Type == "assistant.message" || e.Type == "assistant.delta" || e.Type == "pipeline.expression" {
			t.Fatal("late output", e)
		}
	}
}

func TestSupportBadOptionalQuotesNeverBecomeFacts(t *testing.T) {
	answer := "你现在只想说说，我在听。"
	r, sid := supportFixture(t, func(ctx context.Context, req provider.Request, emit provider.Emit) (provider.Result, error) {
		switch req.Context["phase"] {
		case "support.risk":
			raw, _ := json.Marshal(req.Input["messages"])
			if strings.Contains(string(raw), "happy") || strings.Contains(string(raw), "0.99") {
				t.Error("sensor entered risk checker")
			}
			return content(`{"level":"ordinary","quote":""}`)
		case "support.compose":
			return content(composeFixture(intervention.Plan{Intent: "listen", Facts: []intervention.Fact{{Key: "need", Quote: "只想说说"}, {Key: "situation", Quote: "happy 0.99"}}, GoalQuote: "编造目标"}, answer))
		case "support.guard":
			return content(guardPlanFixture(intervention.Plan{Intent: "listen", Facts: []intervention.Fact{{Key: "need", Quote: "只想说说"}, {Key: "situation", Quote: "happy 0.99"}}, GoalQuote: "编造目标"}, "approve", answer))
		}
		return provider.Result{}, fmt.Errorf("unexpected phase")
	})
	context := supportContext([]map[string]any{{"role": "system", "content": "rules"}, {"role": "user", "content": `{"type":"user_perception","signals":{"expression":"happy","score":0.99}}`}, {"role": "user", "content": "我只想说说"}})
	if len(context) != 1 {
		t.Fatal("transient perception retained", context)
	}
	turn, _ := r.Start(sid, "我只想说说", "text", false, nil)
	awaitTurn(t, turn)
	s := r.Store.Intervention(sid)
	if len(s.Facts) != 1 || s.Facts["need"].Quote != "只想说说" || s.Goal == "编造目标" || finalSupportText(r, sid) != answer {
		t.Fatal(s, r.Store.Events(sid, 0))
	}
}

func TestSupportToolDecisionWaitsForRiskAndNeverLeaksPrelude(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var invoked atomic.Int32
	answer := "当前结果已读取。"
	r, sid := supportFixture(t, func(ctx context.Context, req provider.Request, emit provider.Emit) (provider.Result, error) {
		switch req.Context["phase"] {
		case "support.risk":
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
				return provider.Result{}, ctx.Err()
			}
			return content(`{"level":"ordinary","quote":""}`)
		case "support.compose":
			messages := req.Input["messages"].([]map[string]any)
			if messages[len(messages)-1]["role"] != "tool" {
				return provider.OK(map[string]any{"content": "工具决策前言，不可播报。", "tool_calls": []any{map[string]any{"id": "call", "type": "function", "function": map[string]any{"name": "get_current_time", "arguments": "{}"}}}}), nil
			}
			return content(composeFixture(intervention.Plan{Intent: "answer"}, answer))
		case "support.guard":
			return content(guardFixture("approve", answer))
		}
		return provider.Result{}, fmt.Errorf("unexpected call")
	}, true)
	r.Bus.Register("get_current_time", func(ctx context.Context, req provider.Request, emit provider.Emit) (provider.Result, error) {
		invoked.Add(1)
		return provider.OK(map[string]any{"datetime": "fixture"}), nil
	})
	turn, _ := r.Start(sid, "读取当前时间。", "text", false, nil)
	waitSignal(t, entered, turn, "risk")
	if invoked.Load() != 0 {
		t.Fatal("tool executed before risk")
	}
	close(release)
	awaitTurn(t, turn)
	if invoked.Load() != 1 || finalSupportText(r, sid) != answer {
		t.Fatal(r.Store.Events(sid, 0))
	}
	for _, e := range r.Store.Events(sid, 0) {
		if e.Type == "assistant.delta" && strings.Contains(provider.String(e.Data["text"]), "前言") {
			t.Fatal("tool prelude escaped")
		}
	}
}

func TestSupportBoundedGuardFailureAndPartialHistory(t *testing.T) {
	for _, partial := range []bool{false, true} {
		t.Run(fmt.Sprint(partial), func(t *testing.T) {
			first := "你希望先说说今天的经历，我会按你的节奏听。"
			answer := first + "后续也由你决定。"
			var guards atomic.Int32
			r, sid := supportFixture(t, func(ctx context.Context, req provider.Request, emit provider.Emit) (provider.Result, error) {
				switch req.Context["phase"] {
				case "support.risk":
					return streamFixture(emit, `{"level":"ordinary","quote":""}`, nil)
				case "support.compose":
					return streamFixture(emit, composeFixture(intervention.Plan{Intent: "listen"}, answer), nil)
				case "support.guard":
					guards.Add(1)
					if partial {
						if err := emit(map[string]any{"type": "text", "text": `{"plan":{"intent":"answer"},"decision":"approve","emotion":"neutral","intensity":0.1,"speech":"<speak/>` + first}); err != nil {
							return provider.Result{}, err
						}
						return provider.Result{}, errors.New("interrupted stream")
					}
					return streamFixture(emit, `{"plan":{"intent":"answer"},"decision":"block","speech":""}`, nil)
				}
				return provider.Result{}, fmt.Errorf("unexpected call")
			})
			p := r.Config.Providers["llm"]
			p.Streaming = true
			r.Config.Providers["llm"] = p
			turn, _ := r.Start(sid, "先听我说。", "text", false, nil)
			awaitTurn(t, turn)
			if guards.Load() != 1 {
				t.Fatal("guard retried")
			}
			if partial {
				if finalSupportText(r, sid) != first || r.Store.Intervention(sid).Execution.Status != "partial" {
					t.Fatal(r.Store.Events(sid, 0))
				}
			} else if !strings.Contains(finalSupportText(r, sid), "未通过内容检查") {
				t.Fatal("blocked candidate released")
			}
		})
	}
}

func TestSupportEveryPhaseMeetsHTTPJSONContract(t *testing.T) {
	answer := "我在听。"
	var calls atomic.Int32
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		raw, _ := json.Marshal(body["messages"])
		if !strings.Contains(strings.ToLower(string(raw)), "json") {
			w.WriteHeader(400)
			fmt.Fprint(w, `{"error":{"message":"messages must contain json"}}`)
			return
		}
		calls.Add(1)
		messages := body["messages"].([]any)
		system := provider.String(messages[0].(map[string]any)["content"])
		value := composeFixture(intervention.Plan{Intent: "listen"}, answer)
		if strings.Contains(system, "独立的风险") {
			value = `{"level":"ordinary","quote":""}`
		} else if strings.Contains(system, "最终审校器") {
			value = guardFixture("approve", answer)
		}
		chunk, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": value}}}})
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", chunk)
	}))
	defer remote.Close()
	r, sid := supportFixture(t, func(context.Context, provider.Request, provider.Emit) (provider.Result, error) {
		return provider.OK(nil), nil
	})
	r.Config.Providers["llm"] = config.Provider{Kind: "openai", BaseURL: remote.URL, Model: "contract-fixture", TimeoutMS: 1000}
	turn, _ := r.Start(sid, "我先想说说。", "text", false, nil)
	awaitTurn(t, turn)
	if calls.Load() != 3 || finalSupportText(r, sid) != answer {
		t.Fatal(calls.Load(), r.Store.Events(sid, 0))
	}
}

func TestSupportFirstSpeechTimingIncludesRiskAndEarlyExits(t *testing.T) {
	for _, route := range []string{"dialogue", "urgent", "risk_fallback"} {
		t.Run(route, func(t *testing.T) {
			answer := "我在听。"
			r, sid := supportFixture(t, func(ctx context.Context, req provider.Request, emit provider.Emit) (provider.Result, error) {
				switch req.Context["phase"] {
				case "support.risk":
					time.Sleep(25 * time.Millisecond)
					if route == "urgent" {
						return content(`{"level":"urgent","quote":"当前这一句"}`)
					}
					if route == "risk_fallback" {
						return content(`{"level":"invalid"}`)
					}
					return content(`{"level":"ordinary","quote":""}`)
				case "support.compose":
					return content(composeFixture(intervention.Plan{Intent: "listen"}, answer))
				case "support.guard":
					return content(guardFixture("approve", answer))
				}
				return provider.Result{}, fmt.Errorf("unexpected call")
			})
			turn, _ := r.Start(sid, "当前这一句", "text", false, nil)
			awaitTurn(t, turn)
			var risk, first float64
			found := false
			for _, e := range r.Store.Events(sid, 0) {
				if e.Type == "intervention.check" && e.Data["phase"] == "support.risk" {
					risk = float64(e.Data["elapsed_ms"].(int64))
				}
				if e.Type == "pipeline.timing" && e.Data["stage"] == "llm.first_speech" {
					first = provider.Number(e.Data["elapsed_ms"])
					found = true
				}
			}
			if risk < 20 || !found || first < risk {
				t.Fatal("risk omitted from timing", risk, first, found)
			}
		})
	}
}

func TestSupportRestartAndShutdownInterruptVideo(t *testing.T) {
	c := config.Default()
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	sid := s.Create(c.Characters[0].ID).ID
	next, _ := intervention.Apply(intervention.State{}, intervention.Command{Operation: "start", Mode: intervention.Mindfulness}, time.Now(), "video")
	s.CommitIntervention(sid, 0, next, nil)
	next, _ = intervention.Apply(next, intervention.Command{Operation: "playback", Version: next.Version, ExecutionID: "video", Event: "playing"}, time.Now(), "")
	s.CommitIntervention(sid, next.Version-1, next, nil)
	b, _ := provider.New(&c, nil)
	r := New(&c, s, b)
	if s.Intervention(sid).Execution.Status != "interrupted" {
		t.Fatal("restart retained playback")
	}
	next, err = r.SupportCommand(sid, intervention.Command{Operation: "start", Version: s.Intervention(sid).Version, Mode: intervention.Mindfulness})
	if err != nil {
		t.Fatal(err)
	}
	r.Shutdown()
	if s.Intervention(sid).Execution.Status != "interrupted" || s.Intervention(sid).Version <= next.Version {
		t.Fatal("shutdown retained playback")
	}
}

func TestSupportKeepsArchivedMemoryAndLatestUserMessage(t *testing.T) {
	answer := "按你现在的限制继续梳理。"
	var captured atomic.Bool
	r, sid := supportFixture(t, func(ctx context.Context, req provider.Request, emit provider.Emit) (provider.Result, error) {
		switch req.Context["phase"] {
		case "support.risk":
			return content(`{"level":"ordinary","quote":""}`)
		case "support.compose":
			messages := req.Input["messages"].([]map[string]any)
			raw, _ := json.Marshal(messages)
			if strings.Contains(string(raw), "预算三百元") && messages[len(messages)-1]["content"] == "预算现在改成五百元，沿用之前的设备限制。" {
				captured.Store(true)
			}
			return content(composeFixture(intervention.Plan{Intent: "answer", Facts: []intervention.Fact{{Key: "constraints", Quote: "预算现在改成五百元"}}}, answer))
		case "support.guard":
			return content(guardPlanFixture(intervention.Plan{Intent: "answer", Facts: []intervention.Fact{{Key: "constraints", Quote: "预算现在改成五百元"}}}, "approve", answer))
		default:
			return content(`{"entries":[]}`)
		}
	})
	history := []map[string]any{{"role": "user", "content": "预算三百元，设备只有手机。"}, {"role": "assistant", "content": "收到。"}}
	for i := 0; i < 14; i++ {
		history = append(history, map[string]any{"role": "user", "content": fmt.Sprintf("其他话题%d", i)}, map[string]any{"role": "assistant", "content": "好的。"})
	}
	r.Store.AddHistory(sid, history)
	turn, _ := r.Start(sid, "预算现在改成五百元，沿用之前的设备限制。", "text", false, nil)
	awaitTurn(t, turn)
	if !captured.Load() || r.Store.Intervention(sid).Facts["constraints"].Quote != "预算现在改成五百元" {
		t.Fatal("memory or latest correction missing", r.Store.Events(sid, 0))
	}
}

func TestSupportRejectsUncommittedAndDivergentApprovedSpeech(t *testing.T) {
	for _, kind := range []string{"no_commit", "divergent", "bad_budget"} {
		t.Run(kind, func(t *testing.T) {
			candidate := "当前候选是有依据的简短回答。"
			plan := intervention.Plan{Intent: "answer"}
			if kind == "bad_budget" {
				candidate = strings.Repeat("很长的正文。", 60)
			}
			r, sid := supportFixture(t, func(ctx context.Context, req provider.Request, emit provider.Emit) (provider.Result, error) {
				switch req.Context["phase"] {
				case "support.risk":
					return content(`{"level":"ordinary","quote":""}`)
				case "support.compose":
					return content(composeFixture(plan, candidate))
				case "support.guard":
					if kind == "no_commit" {
						return content(`{"plan":{"intent":"answer"},"decision":"approve","emotion":"neutral","intensity":0.1,"speech":"当前候选是有依据的简短回答。"}`)
					}
					if kind == "divergent" {
						return content(guardFixture("approve", "我断定了一个新的原因。"))
					}
					return content(guardFixture("approve", candidate))
				}
				return provider.Result{}, fmt.Errorf("unexpected phase")
			})
			turn, _ := r.Start(sid, "请直接回答。", "text", false, nil)
			awaitTurn(t, turn)
			for _, e := range r.Store.Events(sid, 0) {
				if e.Type == "assistant.delta" {
					t.Fatal("invalid approved stream escaped", e)
				}
			}
			if !strings.Contains(finalSupportText(r, sid), "未通过内容检查") {
				t.Fatal("invalid stream was presented as success")
			}
		})
	}
}

func TestSupportInterruptedApprovedPrefixRemainsInConversation(t *testing.T) {
	first := "你希望先把今天的经历说清楚，我会按你的节奏听。"
	tail := "后面的部分先不说。"
	ready, finish := make(chan struct{}), make(chan struct{})
	r, sid := supportFixture(t, func(ctx context.Context, req provider.Request, emit provider.Emit) (provider.Result, error) {
		switch req.Context["phase"] {
		case "support.risk":
			return streamFixture(emit, `{"level":"ordinary","quote":""}`, nil)
		case "support.compose":
			return streamFixture(emit, composeFixture(intervention.Plan{}, first+tail), nil)
		case "support.guard":
			if err := emit(map[string]any{"type": "text", "text": `{"plan":{"intent":"listen"},"decision":"approve","emotion":"neutral","intensity":0.1,"speech":"<speak/>` + first}); err != nil {
				return provider.Result{}, err
			}
			close(ready)
			select {
			case <-finish:
			case <-ctx.Done():
				return provider.Result{}, ctx.Err()
			}
			return streamFixture(emit, tail+`"}`, nil)
		}
		return provider.Result{}, fmt.Errorf("unexpected phase")
	})
	p := r.Config.Providers["llm"]
	p.Streaming = true
	r.Config.Providers["llm"] = p
	turn, _ := r.Start(sid, "我先想说说。", "text", false, nil)
	waitSignal(t, ready, turn, "approved prefix")
	before := r.Store.Intervention(sid)
	r.Cancel(sid, "new input")
	close(finish)
	awaitTurn(t, turn)
	history := r.Store.History(sid, 20)
	if len(history) != 2 || !strings.Contains(provider.String(history[1]["content"]), first) || strings.Contains(provider.String(history[1]["content"]), tail) {
		t.Fatal("approved context lost or abandoned tail saved", history)
	}
	if r.Store.Intervention(sid).Version != before.Version {
		t.Fatal("cancelled state advanced")
	}
}

func TestSupportNormalizesOnlyTrailingSpeakWrapper(t *testing.T) {
	answer := "你希望先把事情说完，我会按你的节奏听。"
	r, sid := supportFixture(t, func(ctx context.Context, req provider.Request, emit provider.Emit) (provider.Result, error) {
		switch req.Context["phase"] {
		case "support.risk":
			return streamFixture(emit, `{"level":"ordinary","quote":""}`, nil)
		case "support.compose":
			return streamFixture(emit, composeFixture(intervention.Plan{}, answer+"</speak>"), nil)
		case "support.guard":
			header := `{"plan":{"intent":"listen"},"decision":"approve","emotion":"neutral","intensity":0.1,"speech":"<speak/>` + answer
			if err := emit(map[string]any{"type": "text", "text": header}); err != nil {
				return provider.Result{}, err
			}
			for _, ch := range "</speak>" {
				if err := emit(map[string]any{"type": "text", "text": string(ch)}); err != nil {
					return provider.Result{}, err
				}
			}
			return streamFixture(emit, `"}`, nil)
		}
		return provider.Result{}, fmt.Errorf("unexpected phase")
	})
	p := r.Config.Providers["llm"]
	p.Streaming = true
	r.Config.Providers["llm"] = p
	turn, _ := r.Start(sid, "我先想说说。", "text", false, nil)
	awaitTurn(t, turn)
	if finalSupportText(r, sid) != answer {
		t.Fatal(r.Store.Events(sid, 0))
	}
	for _, e := range r.Store.Events(sid, 0) {
		if e.Type == "assistant.delta" && strings.Contains(provider.String(e.Data["text"]), "speak") {
			t.Fatal("control tag reached speech")
		}
	}
}

func TestSupportQuoteValidationUsesActualInputAfterContextFiltering(t *testing.T) {
	user := `{"type":"user_perception","question":"我的预算是三百元"}`
	answer := "按三百元预算理解这份输入。"
	r, sid := supportFixture(t, func(ctx context.Context, req provider.Request, emit provider.Emit) (provider.Result, error) {
		switch req.Context["phase"] {
		case "support.risk":
			return content(`{"level":"ordinary","quote":""}`)
		case "support.compose":
			return content(composeFixture(intervention.Plan{}, answer))
		case "support.guard":
			var input struct {
				Current string `json:"current_user"`
			}
			messages := req.Input["messages"].([]map[string]any)
			json.Unmarshal([]byte(provider.String(messages[1]["content"])), &input)
			if input.Current != user {
				t.Error("current input lost", input.Current)
			}
			return content(guardPlanFixture(intervention.Plan{Intent: "answer", Facts: []intervention.Fact{{Key: "constraints", Quote: "我的预算是三百元"}, {Key: "need", Quote: "旧助手的错误结论"}}}, "approve", answer))
		}
		return provider.Result{}, fmt.Errorf("unexpected phase")
	})
	r.Store.AddHistory(sid, []map[string]any{{"role": "user", "content": "之前的问题。"}, {"role": "assistant", "content": "旧助手的错误结论"}})
	turn, _ := r.Start(sid, user, "text", false, nil)
	awaitTurn(t, turn)
	facts := r.Store.Intervention(sid).Facts
	if len(facts) != 1 || facts["constraints"].Quote != "我的预算是三百元" || facts["constraints"].TurnID != turn.ID {
		t.Fatal("wrong source accepted", facts, r.Store.Events(sid, 0))
	}
}

func TestSupportMechanicalRepairIsBoundedAndKeepsValidatedPlan(t *testing.T) {
	original := "今天事情很多，你想先把经历讲完。你现在最想说哪件事？"
	fixed := "今天事情很多，你想先把经历讲完，我在听。"
	var guards, repairs atomic.Int32
	r, sid := supportFixture(t, func(ctx context.Context, req provider.Request, emit provider.Emit) (provider.Result, error) {
		switch req.Context["phase"] {
		case "support.risk":
			return content(`{"level":"ordinary","quote":""}`)
		case "support.compose":
			return content(composeFixture(intervention.Plan{}, original))
		case "support.guard":
			guards.Add(1)
			return content(guardPlanFixture(intervention.Plan{Intent: "listen", Facts: []intervention.Fact{{Key: "need", Quote: "先听我说"}}}, "approve", original))
		case "support.repair":
			repairs.Add(1)
			var input map[string]any
			messages := req.Input["messages"].([]map[string]any)
			json.Unmarshal([]byte(provider.String(messages[1]["content"])), &input)
			if provider.Number(input["hard_limits"].(map[string]any)["question_limit"]) != 0 {
				t.Error("repair lost question limit")
			}
			return content(guardPlanFixture(intervention.Plan{Intent: "explore", Facts: []intervention.Fact{{Key: "need", Quote: "编造的来源"}}}, "revise", fixed))
		}
		return provider.Result{}, fmt.Errorf("extra call")
	})
	turn, _ := r.Start(sid, "今天事情很多，先听我说。", "text", false, nil)
	awaitTurn(t, turn)
	if finalSupportText(r, sid) != fixed || guards.Load() != 1 || repairs.Load() != 1 {
		t.Fatal("bounded repair failed", r.Store.Events(sid, 0))
	}
	s := r.Store.Intervention(sid)
	if s.LastDecision.Kind != "listen" || s.Facts["need"].Quote != "先听我说" || s.DialogueTurns != 1 {
		t.Fatal("repair changed validated state", s)
	}
}

func TestSupportMechanicalRepairFailureDoesNotRetryOrRelease(t *testing.T) {
	invalid := "你想先讲完今天的经历。现在最想说哪件事？"
	var guards, repairs atomic.Int32
	r, sid := supportFixture(t, func(ctx context.Context, req provider.Request, emit provider.Emit) (provider.Result, error) {
		switch req.Context["phase"] {
		case "support.risk":
			return content(`{"level":"ordinary","quote":""}`)
		case "support.compose":
			return content(composeFixture(intervention.Plan{}, invalid))
		case "support.guard":
			guards.Add(1)
			return content(guardPlanFixture(intervention.Plan{Intent: "listen"}, "approve", invalid))
		case "support.repair":
			repairs.Add(1)
			return content(guardFixture("revise", invalid))
		}
		return provider.Result{}, fmt.Errorf("extra call")
	})
	turn, _ := r.Start(sid, "先听我说，不要问问题。", "text", false, nil)
	awaitTurn(t, turn)
	if guards.Load() != 1 || repairs.Load() != 1 || !strings.Contains(finalSupportText(r, sid), "未通过内容检查") {
		t.Fatal("repair retried or bypassed constraints", r.Store.Events(sid, 0))
	}
	for _, e := range r.Store.Events(sid, 0) {
		if e.Type == "assistant.delta" && strings.Contains(provider.String(e.Data["text"]), "最想说哪件事") {
			t.Fatal("invalid repair reached speech", e)
		}
	}
}
