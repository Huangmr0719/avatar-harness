package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"avatarharness/internal/config"
	"avatarharness/internal/intervention"
	pc "avatarharness/internal/promptcompare"
	"avatarharness/internal/provider"
	"avatarharness/internal/store"
)

type comparisonJob struct {
	mu     sync.Mutex
	value  pc.Run
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	subs   map[chan struct{}]bool
}

func (j *comparisonJob) update(fn func(*pc.Run)) {
	j.mu.Lock()
	defer j.mu.Unlock()
	fn(&j.value)
	j.value.Revision++
	for ch := range j.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}
func (j *comparisonJob) snapshot() pc.Run {
	j.mu.Lock()
	defer j.mu.Unlock()
	raw, _ := json.Marshal(j.value)
	var out pc.Run
	_ = json.Unmarshal(raw, &out)
	return out
}

// PromptComparisons owns only ephemeral comparison runs. The normal store,
// media runtime, user sessions and provider configuration are never mutated.
type PromptComparisons struct {
	parent *Runtime
	mu     sync.Mutex
	jobs   map[string]*comparisonJob
	order  []string
	closed bool
}

func NewPromptComparisons(parent *Runtime) *PromptComparisons {
	return &PromptComparisons{parent: parent, jobs: map[string]*comparisonJob{}}
}

func (m *PromptComparisons) Conditions(characterID string) (pc.Conditions, error) {
	if characterID == "" {
		characterID = m.parent.Config.Characters[0].ID
	}
	ch, ok := m.parent.Character(characterID)
	if !ok {
		return pc.Conditions{}, fmt.Errorf("角色不存在")
	}
	p := m.parent.Config.Providers["llm"]
	safe := map[string]any{}
	for _, key := range []string{"temperature", "top_p", "max_tokens", "seed", "enable_thinking", "reasoning_effort"} {
		if value, ok := p.Parameters[key]; ok {
			safe[key] = value
		}
	}
	tokens := map[string]int{"risk": 160, "compose": 1024}
	if cap := int(provider.Number(p.Parameters["max_tokens"])); cap > 0 {
		for key, n := range tokens {
			tokens[key] = min(cap, n)
		}
	}
	if p.Kind == "openai" {
		if _, present := p.Parameters["enable_thinking"]; present {
			safe["enable_thinking"] = false
			delete(safe, "reasoning_effort")
		}
		safe["max_tokens"] = tokens["compose"]
	}
	return pc.Conditions{Model: p.Model, Parameters: safe, ProviderParametersHash: pc.Digest(p), CharacterID: ch.ID, PersonaHash: pc.Digest(ch.Persona), CommonProtocolHash: comparisonProtocolHash(), HistoryLimit: m.parent.Config.Limits.MaxHistoryMessages, Tokens: tokens, ReviewMode: "none", Protocol: "frozen-reference-history/v2; shared-risk; direct-stream; no-model-review; same-memory-retrieval; fixed-signals; tools-off; text-only; per-request-thinking-off"}, nil
}

func (m *PromptComparisons) Start(req pc.Request) (pc.Run, error) {
	if (req.Scenario == nil) == (req.ScenarioID == "") {
		return pc.Run{}, fmt.Errorf("选择一个示例或提供自定义对话")
	}
	var scenario pc.Scenario
	if req.Scenario != nil {
		scenario = *req.Scenario
	} else {
		for _, s := range pc.Scenarios() {
			if s.ID == req.ScenarioID {
				scenario = s
				break
			}
		}
	}
	if err := scenario.Validate(); err != nil {
		return pc.Run{}, err
	}
	if req.Repeats == 0 {
		req.Repeats = 1
	}
	if req.Repeats < 1 || req.Repeats > 3 {
		return pc.Run{}, fmt.Errorf("重复次数需为 1–3")
	}
	conditions, err := m.Conditions(req.CharacterID)
	if err != nil {
		return pc.Run{}, err
	}
	if config.State(m.parent.Config.Providers["llm"])["configured"] != true {
		return pc.Run{}, fmt.Errorf("请先配置对话模型")
	}
	// Freeze client-owned maps and slices before launching any worker.
	raw, _ := json.Marshal(scenario)
	var frozen pc.Scenario
	if err := json.Unmarshal(raw, &frozen); err != nil {
		return pc.Run{}, fmt.Errorf("无法冻结对话输入：%w", err)
	}
	scenario = frozen
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return pc.Run{}, fmt.Errorf("服务正在停止")
	}
	for _, j := range m.jobs {
		select {
		case <-j.done:
		default:
			return pc.Run{}, fmt.Errorf("已有对照正在运行，请完成或停止后再试")
		}
	}
	for len(m.order) >= 12 {
		id := m.order[0]
		m.order = m.order[1:]
		delete(m.jobs, id)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	j := &comparisonJob{ctx: ctx, cancel: cancel, done: make(chan struct{}), subs: map[chan struct{}]bool{}, value: pc.Run{ID: store.ID(), Revision: 1, Status: "running", Scenario: scenario, Repeats: req.Repeats, Conditions: conditions, Variants: PromptVariants(), Rounds: []pc.Round{}, CreatedAt: time.Now().UTC().Format(time.RFC3339)}}
	m.jobs[j.value.ID] = j
	m.order = append(m.order, j.value.ID)
	go m.run(j)
	return j.snapshot(), nil
}
func (m *PromptComparisons) job(id string) *comparisonJob {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.jobs[id]
}
func (m *PromptComparisons) Snapshot(id string) (pc.Run, bool) {
	j := m.job(id)
	if j == nil {
		return pc.Run{}, false
	}
	return j.snapshot(), true
}
func (m *PromptComparisons) Stop(id string) bool {
	j := m.job(id)
	if j == nil {
		return false
	}
	j.cancel()
	return true
}
func (m *PromptComparisons) Subscribe(id string) (<-chan struct{}, func(), bool) {
	j := m.job(id)
	if j == nil {
		return nil, nil, false
	}
	ch := make(chan struct{}, 1)
	j.mu.Lock()
	j.subs[ch] = true
	j.mu.Unlock()
	return ch, func() { j.mu.Lock(); delete(j.subs, ch); j.mu.Unlock() }, true
}
func (m *PromptComparisons) Close() {
	m.mu.Lock()
	m.closed = true
	jobs := make([]*comparisonJob, 0, len(m.jobs))
	for _, j := range m.jobs {
		jobs = append(jobs, j)
	}
	m.mu.Unlock()
	for _, j := range jobs {
		j.cancel()
	}
	for _, j := range jobs {
		<-j.done
	}
}

func (m *PromptComparisons) run(j *comparisonJob) {
	defer close(j.done)
	defer j.cancel()
	var runErr error
	defer func() {
		j.update(func(v *pc.Run) {
			if runErr != nil {
				v.Status = "error"
				v.Error = runErr.Error()
			} else if j.ctx.Err() != nil {
				v.Status = "cancelled"
			} else {
				v.Status = "completed"
				for _, r := range v.Rounds {
					if r.Legacy.Status == "error" || r.Current.Status == "error" {
						v.Status = "error"
						v.Error = "部分回答未完成，详情保留在对应列中"
					}
				}
			}
		})
	}()
	scenario := j.value.Scenario
	for repeat := 1; repeat <= j.value.Repeats; repeat++ {
		history := append([]pc.Message{}, scenario.History...)
		for n, step := range scenario.Turns {
			if j.ctx.Err() != nil {
				return
			}
			index := len(j.snapshot().Rounds)
			j.update(func(v *pc.Run) {
				v.Rounds = append(v.Rounds, pc.Round{Index: index, Repeat: repeat, Turn: n + 1, User: step.User, History: append([]pc.Message{}, history...), ReferenceAnswer: step.ReferenceAnswer, Legacy: pc.Leg{Variant: "legacy", Status: "running"}, Current: pc.Leg{Variant: "current", Status: "running"}})
			})
			if err := m.runPair(j, index, history, step, n); err != nil {
				runErr = err
				return
			}
			// Neither model's answer becomes the next turn's context. A fixed
			// reference reply (or explicit acknowledgement) is shared by both.
			reference := step.ReferenceAnswer
			if reference == "" {
				reference = "收到。"
			}
			history = append(history, pc.Message{Role: "user", Content: step.User}, pc.Message{Role: "assistant", Content: reference})
		}
	}
}

func (m *PromptComparisons) runPair(j *comparisonJob, index int, history []pc.Message, step pc.Turn, turnNumber int) error {
	dir, err := os.MkdirTemp("", "avatar-prompt-comparison-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	shared := &riskTask{done: make(chan struct{})}
	type legRuntime struct {
		runtime *Runtime
		store   *store.Store
		sid     string
	}
	legs := make([]legRuntime, 0, 2)
	defer func() {
		for _, leg := range legs {
			leg.runtime.Shutdown()
			_ = leg.store.Close()
		}
	}()
	scenario := j.value.Scenario
	for _, variant := range []string{"legacy", "current"} {
		s, err := store.Open(filepath.Join(dir, variant))
		if err != nil {
			return err
		}
		sid := s.Create(j.value.Conditions.CharacterID).ID
		messages := make([]map[string]any, 0, len(history))
		for _, v := range history {
			messages = append(messages, map[string]any{"role": v.Role, "content": v.Content})
		}
		s.AddHistory(sid, messages)
		parameters := intervention.Defaults(scenario.Mode)
		if scenario.Parameters != nil {
			parameters = *scenario.Parameters
		}
		state := intervention.State{Version: 1, Mode: scenario.Mode, Status: "ready", Goal: scenario.Goal, Stage: "listening", Parameters: parameters, Facts: map[string]intervention.Evidence{}, DialogueTurns: turnNumber}
		if !s.CommitIntervention(sid, 0, state, nil) {
			_ = s.Close()
			return fmt.Errorf("无法建立共同状态")
		}
		r := New(m.parent.Config, s, m.parent.Bus)
		r.comparison = &comparisonSettings{Variant: variant, Signals: scenario.Signals, Risk: shared}
		legs = append(legs, legRuntime{r, s, sid})
	}
	var riskJob sync.WaitGroup
	riskJob.Add(1)
	go func() {
		defer riskJob.Done()
		defer close(shared.done)
		ctx, cancel := context.WithTimeout(j.ctx, 4*time.Second)
		defer cancel()
		contextMessages := []map[string]any{}
		for _, v := range history {
			contextMessages = append(contextMessages, map[string]any{"role": v.Role, "content": v.Content})
		}
		contextMessages = append(contextMessages, map[string]any{"role": "user", "content": step.User})
		t := &Turn{Context: ctx, Metadata: map[string]any{"request_id": j.value.ID + fmt.Sprint(index) + "-risk", "comparison_variant": "shared"}}
		start := time.Now()
		shared.value, shared.err = legs[0].runtime.checkSupportRisk(ctx, t, step.User, contextMessages)
		j.update(func(v *pc.Run) {
			r := &v.Rounds[index]
			r.Risk = shared.value.Level
			r.RiskMS = float64(time.Since(start).Microseconds()) / 1000
			if shared.err != nil {
				r.RiskError = shared.err.Error()
			}
		})
	}()
	var wg sync.WaitGroup
	wg.Add(2)
	for _, leg := range legs {
		go func(leg legRuntime) {
			defer wg.Done()
			_, events, unsubscribe := leg.store.Subscribe(leg.sid)
			defer unsubscribe()
			turn, err := leg.runtime.Start(leg.sid, step.User, "text", false, nil)
			if err != nil {
				j.update(func(v *pc.Run) {
					l := comparisonLeg(&v.Rounds[index], leg.runtime.comparison.Variant)
					l.Status = "error"
					l.Error = err.Error()
				})
				return
			}
			cancelWatchDone := make(chan struct{})
			defer close(cancelWatchDone)
			go func() {
				select {
				case <-j.ctx.Done():
					leg.runtime.Cancel(leg.sid, "停止对照演示")
				case <-cancelWatchDone:
				}
			}()
			lastSeq := int64(0)
			consume := func(e store.Event) {
				if e.Seq <= lastSeq || e.TurnID != turn.ID {
					return
				}
				lastSeq = e.Seq
				j.update(func(v *pc.Run) {
					consumeComparisonEvent(comparisonLeg(&v.Rounds[index], leg.runtime.comparison.Variant), e, turn.Started)
				})
			}
			for {
				select {
				case e := <-events:
					consume(e)
				case <-turn.Done:
					for _, e := range leg.store.Events(leg.sid, 0) {
						consume(e)
					}
					state := leg.store.Intervention(leg.sid)
					j.update(func(v *pc.Run) {
						l := comparisonLeg(&v.Rounds[index], leg.runtime.comparison.Variant)
						l.State = &state
						if j.ctx.Err() != nil {
							l.Status = "cancelled"
						} else if l.Text == "" || l.Error != "" {
							l.Status = "error"
						} else {
							l.Status = "completed"
						}
						complete := l.Status == "completed"
						l.Checks = pc.Evaluate(step.Checks, l.Text, &state, v.Rounds[index].Risk, complete)
						if l.Candidate != "" && complete {
							l.CandidateChecks = pc.Evaluate(step.Checks, l.Candidate, &state, v.Rounds[index].Risk, true)
						}
					})
					return
				}
			}
		}(leg)
	}
	wg.Wait()
	riskJob.Wait()
	j.update(func(v *pc.Run) {
		r := &v.Rounds[index]
		r.ConditionsMatch = r.Legacy.ConditionsHash != "" && r.Legacy.ConditionsHash == r.Current.ConditionsHash
	})
	if j.ctx.Err() != nil {
		return nil
	}
	return nil
}
func comparisonLeg(r *pc.Round, variant string) *pc.Leg {
	if variant == "legacy" {
		return &r.Legacy
	}
	return &r.Current
}
func consumeComparisonEvent(l *pc.Leg, e store.Event, started time.Time) {
	switch e.Type {
	case "comparison.input":
		l.ConditionsHash = provider.String(e.Data["conditions_hash"])
	case "comparison.candidate":
		l.Candidate = provider.String(e.Data["text"])
	case "assistant.delta":
		if e.Data["draft"] != true {
			l.Text += provider.String(e.Data["text"])
		}
	case "assistant.message":
		l.Text = provider.String(e.Data["text"])
	case "pipeline.timing":
		if e.Data["stage"] == "llm.first_speech" {
			n := provider.Number(e.Data["elapsed_ms"])
			l.FirstTextMS = &n
		}
	case "intervention.guard":
		if e.Data["decision"] == "revise" {
			l.Revisions++
		}
	case "intervention.constraint_repair":
		l.RepairRequests++
	case "intervention.check":
		l.Requests++
		l.Phases = append(l.Phases, pc.Phase{Name: provider.String(e.Data["phase"]), OK: e.Data["ok"] == true, ElapsedMS: provider.Number(e.Data["elapsed_ms"])})
	case "intervention.fallback":
		l.Fallback = true
		l.FallbackReason = provider.String(e.Data["reason"])
		l.FallbackDetail = provider.String(e.Data["detail"])
	case "intervention.partial":
		l.Error = "仅放行了部分回答"
	case "turn.error":
		l.Error = provider.String(e.Data["message"])
	case "turn.finished":
		n := provider.Number(e.Data["duration_ms"])
		l.CompletionMS = &n
		if l.FirstTextMS == nil && l.Text != "" {
			n := e.At*1000 - float64(started.UnixMilli())
			l.FirstTextMS = &n
		}
	}
}

// Wait is useful for deterministic integration tests and CLI evaluation.
func (m *PromptComparisons) Wait(ctx context.Context, id string) (pc.Run, error) {
	j := m.job(id)
	if j == nil {
		return pc.Run{}, fmt.Errorf("对照记录不存在")
	}
	select {
	case <-j.done:
		return j.snapshot(), nil
	case <-ctx.Done():
		return pc.Run{}, ctx.Err()
	}
}
