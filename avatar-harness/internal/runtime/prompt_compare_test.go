package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"avatarharness/internal/intervention"
	pc "avatarharness/internal/promptcompare"
	"avatarharness/internal/provider"
	"avatarharness/internal/store"
)

func comparisonScenario() pc.Scenario {
	return pc.Scenario{ID: "fixture", Title: "共同上下文", Mode: intervention.CBT, Signals: map[string]any{"voice": map[string]any{"emotion": "happy", "confidence": nil}}, Turns: []pc.Turn{
		{User: "先听我说。", ReferenceAnswer: "这是两侧共同的参考回答。", Checks: []pc.Check{{ID: "q", Label: "不追问", Kind: "max_questions", Limit: 0}}},
		{User: "沿用刚才的条件。", ReferenceAnswer: "继续共同记录。", Checks: []pc.Check{{ID: "m", Label: "事实有依据", Kind: "manual"}}},
	}}
}
func awaitComparison(t *testing.T, m *PromptComparisons, id string) pc.Run {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	run, err := m.Wait(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return run
}
func TestPromptComparisonFreezesActualInputsAndSharesOnlyRisk(t *testing.T) {
	var risk, compose, guards atomic.Int32
	var mu sync.Mutex
	inputs := map[string][]string{}
	answers := map[string]string{"legacy": "老版回应：我会先听你说完这件事情。", "current": "新版回应：你可以按自己的节奏说下去。"}
	r, _ := supportFixture(t, func(ctx context.Context, req provider.Request, emit provider.Emit) (provider.Result, error) {
		variant := provider.String(req.Context["comparison_variant"])
		switch req.Context["phase"] {
		case "support.risk":
			if variant != "shared" {
				t.Error("risk not shared", variant)
			}
			risk.Add(1)
			return content(`{"level":"ordinary","quote":""}`)
		case "support.compose":
			compose.Add(1)
			messages := req.Input["messages"].([]map[string]any)
			if len(req.Input["tools"].([]map[string]any)) != 0 {
				t.Error("comparison offered tools")
			}
			raw, _ := json.Marshal(messages[1:])
			mu.Lock()
			inputs[variant] = append(inputs[variant], string(raw))
			mu.Unlock()
			return content(composeFixture(intervention.Plan{}, answers[variant]))
		case "support.guard":
			guards.Add(1)
			return content(guardPlanFixture(intervention.Plan{Intent: "listen"}, "approve", answers[variant]))
		}
		return provider.Result{}, fmt.Errorf("unexpected request %v", req.Context)
	})
	beforeSessions := len(r.Store.Sessions())
	m := NewPromptComparisons(r)
	t.Cleanup(m.Close)
	scenario := comparisonScenario()
	job, err := m.Start(pc.Request{Scenario: &scenario})
	if err != nil {
		t.Fatal(err)
	}
	run := awaitComparison(t, m, job.ID)
	if run.Status != "completed" || run.Conditions.ReviewMode != "none" || len(run.Rounds) != 2 || risk.Load() != 2 || compose.Load() != 4 || guards.Load() != 0 {
		t.Fatal(run, risk.Load(), compose.Load(), guards.Load())
	}
	for _, round := range run.Rounds {
		if !round.ConditionsMatch || round.Legacy.Text == round.Current.Text || round.Legacy.Candidate == "" || round.Legacy.FirstTextMS == nil || round.Current.Requests != 1 || round.Current.Revisions != 0 || round.Current.RepairRequests != 0 {
			t.Fatal(round)
		}
	}
	if inputs["legacy"][0] != inputs["current"][0] || inputs["legacy"][1] != inputs["current"][1] {
		t.Fatal("actual provider contexts differ", inputs)
	}
	if !strings.Contains(inputs["current"][1], scenario.Turns[0].ReferenceAnswer) || strings.Contains(inputs["current"][1], "老版回应") || strings.Contains(inputs["current"][1], "新版回应") {
		t.Fatal("generated answers contaminated common history", inputs)
	}
	if len(r.Store.Sessions()) != beforeSessions {
		t.Fatal("comparison polluted normal sessions")
	}
	for _, s := range r.Store.Sessions() {
		if len(r.Store.History(s.ID, 100)) != 0 {
			t.Fatal("comparison wrote user history")
		}
	}
}
func TestPromptComparisonRunsBothCandidatesWhileRiskIsPending(t *testing.T) {
	riskEntered, release, composeEntered := make(chan struct{}), make(chan struct{}), make(chan struct{}, 2)
	var guards atomic.Int32
	answer := "你可以先说完整件事，我会按你的节奏听。"
	r, _ := supportFixture(t, func(ctx context.Context, req provider.Request, emit provider.Emit) (provider.Result, error) {
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
			composeEntered <- struct{}{}
			return content(composeFixture(intervention.Plan{}, answer))
		case "support.guard":
			guards.Add(1)
			return content(guardPlanFixture(intervention.Plan{Intent: "listen"}, "approve", answer))
		}
		return provider.Result{}, fmt.Errorf("unexpected phase")
	})
	m := NewPromptComparisons(r)
	t.Cleanup(m.Close)
	s := comparisonScenario()
	s.Turns = s.Turns[:1]
	job, err := m.Start(pc.Request{Scenario: &s})
	if err != nil {
		t.Fatal(err)
	}
	// The caller cannot alter either model's in-flight context after Start.
	s.Turns[0].User = "调用者后来改写的输入"
	s.Signals["voice"].(map[string]any)["emotion"] = "angry"
	frozen, _ := m.Snapshot(job.ID)
	if frozen.Scenario.Turns[0].User == s.Turns[0].User || frozen.Scenario.Signals["voice"].(map[string]any)["emotion"] != "happy" {
		t.Fatal("caller data changed the common snapshot", frozen.Scenario)
	}
	for _, signal := range []<-chan struct{}{riskEntered, composeEntered, composeEntered} {
		select {
		case <-signal:
		case <-time.After(time.Second):
			t.Fatal("candidates did not overlap")
		}
	}
	if _, err = m.Start(pc.Request{Scenario: &s}); err == nil {
		t.Fatal("overlapping experiment admitted")
	}
	snap, _ := m.Snapshot(job.ID)
	if guards.Load() != 0 || snap.Rounds[0].Legacy.Text != "" || snap.Rounds[0].Current.Text != "" {
		t.Fatal("risk gate bypassed", snap)
	}
	close(release)
	if run := awaitComparison(t, m, job.ID); run.Status != "completed" {
		t.Fatal(run)
	}
}
func TestPromptComparisonRevisionsAreCountedAndCancellationIsIsolated(t *testing.T) {
	entered := make(chan struct{}, 2)
	answer := "当前普通对话可以正常完成。"
	r, sid := supportFixture(t, func(ctx context.Context, req provider.Request, emit provider.Emit) (provider.Result, error) {
		variant := provider.String(req.Context["comparison_variant"])
		switch req.Context["phase"] {
		case "support.risk":
			return content(`{"level":"ordinary","quote":""}`)
		case "support.compose":
			if variant != "" {
				entered <- struct{}{}
				<-ctx.Done()
				return provider.Result{}, ctx.Err()
			}
			return content(composeFixture(intervention.Plan{}, answer))
		case "support.guard":
			return content(guardFixture("approve", answer))
		}
		return provider.Result{}, fmt.Errorf("unexpected")
	})
	m := NewPromptComparisons(r)
	t.Cleanup(m.Close)
	s := comparisonScenario()
	job, err := m.Start(pc.Request{Scenario: &s})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("not started")
		}
	}
	ordinary, err := r.Start(sid, "直接回答当前问题。", "text", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	awaitTurn(t, ordinary)
	m.Stop(job.ID)
	run := awaitComparison(t, m, job.ID)
	if run.Status != "cancelled" || len(run.Rounds) != 1 || ordinary.Cancelled || finalSupportText(r, sid) != answer {
		t.Fatal(run, ordinary.Cancelled)
	}
	// Counters distinguish a semantic rewrite from an additional budget repair.
	var l pc.Leg
	for _, data := range []map[string]any{{"decision": "revise", "phase": "support.guard"}, {"decision": "revise", "phase": "support.repair"}} {
		consumeComparisonEvent(&l, store.Event{Type: "intervention.guard", Data: data}, time.Now())
	}
	consumeComparisonEvent(&l, store.Event{Type: "intervention.constraint_repair"}, time.Now())
	if l.Revisions != 2 || l.RepairRequests != 1 {
		t.Fatal(l)
	}
	consumeComparisonEvent(&l, store.Event{Type: "intervention.fallback", Data: map[string]any{"reason": "compose_unavailable", "detail": "数字人结构化回复不完整"}}, time.Now())
	if !l.Fallback || l.FallbackReason != "compose_unavailable" || l.FallbackDetail == "" {
		t.Fatal("fallback lost diagnostic reason", l)
	}
}

func TestPromptComparisonStreamsGeneratedPrefixWithoutReview(t *testing.T) {
	finish := make(chan struct{})
	defer close(finish)
	first := "这是风险通过后直接发送的第一句话。"
	tail := "第二句话稍后才会完成。"
	r, _ := supportFixture(t, func(ctx context.Context, req provider.Request, emit provider.Emit) (provider.Result, error) {
		switch req.Context["phase"] {
		case "support.risk":
			return streamFixture(emit, `{"level":"ordinary","quote":""}`, nil)
		case "support.compose":
			if err := emit(map[string]any{"type": "text", "text": `{"emotion":"neutral","intensity":0.1,"speech":"<speak/>` + first}); err != nil {
				return provider.Result{}, err
			}
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
	m := NewPromptComparisons(r)
	t.Cleanup(m.Close)
	s := comparisonScenario()
	s.Turns = s.Turns[:1]
	job, err := m.Start(pc.Request{Scenario: &s})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		snap, _ := m.Snapshot(job.ID)
		if len(snap.Rounds) > 0 && snap.Rounds[0].Legacy.Text == first && snap.Rounds[0].Current.Text == first {
			if snap.Rounds[0].Legacy.CompletionMS != nil {
				t.Fatal("not streaming")
			}
			m.Stop(job.ID)
			awaitComparison(t, m, job.ID)
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("generated prefix not observable before generation finishes")
}
