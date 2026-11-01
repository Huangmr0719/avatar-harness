package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"avatarharness/internal/intervention"
	"avatarharness/internal/store"
)

type supportTurn struct {
	Before         intervention.State
	Next           intervention.State
	Plan           intervention.Plan
	Context        []map[string]any
	User           string
	Released       string
	ExpressionSent bool
}

// Keep only admitted speech as context when a user interrupts. Abandoned
// planning state and unreleased text must not be committed with it.
type approvedSupportHistory struct {
	Commit     []map[string]any
	Text       string
	Expression *Expression
	Structured bool
}

func (r *Runtime) saveApprovedPrefixLocked(t *Turn) {
	h := t.approvedHistory
	if h == nil || h.Text == "" {
		return
	}
	messages := append([]map[string]any{}, h.Commit...)
	messages = append(messages, map[string]any{"role": "assistant", "content": supportHistory(h.Text, h.Expression, h.Structured)})
	r.Store.AddHistory(t.SessionID, messages)
	r.Store.Emit(t.SessionID, t.ID, "intervention.partial", map[string]any{"reason": "interrupted_after_approved_speech", "characters": len([]rune(h.Text))})
	t.approvedHistory = nil
}

func (r *Runtime) SupportCommand(sid string, command intervention.Command) (intervention.State, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.Store.Session(sid); !ok {
		return intervention.State{}, fmt.Errorf("会话不存在")
	}
	s := r.Store.Intervention(sid)
	command.Source = "user_control"
	next, err := intervention.Apply(s, command, time.Now(), store.ID())
	if err != nil {
		return s, err
	}
	if command.Operation == "start" || command.Operation == "stop" || command.Operation == "parameters" || command.Operation == "feedback" {
		r.cancelLocked(sid, "支持方式或反馈已更新")
	}
	if !r.Store.CommitIntervention(sid, s.Version, next, nil) {
		return s, fmt.Errorf("支持状态已更新")
	}
	r.Store.Emit(sid, "", "intervention.state", map[string]any{"state": next})
	return next, nil
}

func (r *Runtime) interruptSupportLocked(sid string) bool {
	s := r.Store.Intervention(sid)
	if s.Execution == nil || (s.Execution.Status != "queued" && s.Execution.Status != "playing") {
		return false
	}
	n := s.Clone()
	n.Version++
	n.Execution.Status = "interrupted"
	n.LastDecision = intervention.Decision{Kind: "interrupt", Reason: "用户停止、切换会话或发起新输入", Source: "input_control", At: time.Now().UnixMilli()}
	if !r.Store.CommitIntervention(sid, s.Version, n, nil) {
		return false
	}
	r.Store.Emit(sid, "", "intervention.state", map[string]any{"state": n})
	return true
}

func supportHistory(text string, expression *Expression, structured bool) string {
	if !structured {
		return text
	}
	if expression == nil {
		expression = &Expression{Style: "neutral", Intensity: .1}
	}
	data, _ := json.Marshal(map[string]any{"emotion": expression.Style, "intensity": expression.Intensity, "speech": "<speak/>" + text})
	return string(data)
}

func (r *Runtime) deliverSupport(t *Turn, s *supportTurn, text string, expression *Expression, commit []map[string]any, structured bool, speech *Speech) error {
	if expression == nil {
		expression = &Expression{Style: "neutral", Intensity: .1, DurationMS: 8000}
	}
	commit = append(commit, map[string]any{"role": "assistant", "content": supportHistory(text, expression, structured)})
	r.mu.Lock()
	valid := !t.Cancelled && t.Context.Err() == nil && r.current[t.SessionID] == t
	if valid {
		valid = r.Store.CommitIntervention(t.SessionID, s.Before.Version, s.Next, commit)
		if valid {
			t.approvedHistory = nil
		}
	}
	r.mu.Unlock()
	if !valid {
		return context.Canceled
	}
	r.Emit(t, "intervention.approved", map[string]any{"risk": s.Plan.Risk, "risk_quote": s.Plan.RiskQuote, "intent": s.Plan.Intent, "card_id": intervention.Select(s.Next, s.Plan.Intent).ID})
	r.Emit(t, "intervention.state", map[string]any{"state": s.Next})
	if !s.ExpressionSent {
		r.Emit(t, "pipeline.expression", map[string]any{"style": expression.Style, "intensity": expression.Intensity, "duration_ms": expression.DurationMS, "source": "llm_structured"})
	}
	r.Emit(t, "assistant.message", map[string]any{"text": text, "source": "llm", "execution_id": func() string {
		if s.Next.Execution != nil {
			return s.Next.Execution.ID
		}
		return ""
	}()})
	r.startMemory(t)
	if speech != nil {
		speech.SetExpression(*expression)
		if s.Released == "" {
			if err := speech.Feed(text, false); err != nil {
				return err
			}
		}
		return speech.Finish()
	}
	return nil
}
