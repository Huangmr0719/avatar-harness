package runtime

import (
	"encoding/json"
	"strings"

	"avatarharness/internal/intervention"
)

func supportOutputIssues(s *supportTurn, text string) []string {
	issues := intervention.CheckOutput(s.Next, s.Plan, text)
	return append(issues, intervention.CurrentResponseConstraints(s.User).Check(text)...)
}

func supportActionLimits(user string, state intervention.State) intervention.State {
	if minutes := intervention.CurrentResponseConstraints(user).ActionMinutes; minutes > 0 {
		state.Parameters.ActionMinutes = min(minutes, state.Parameters.ActionMinutes)
	}
	return state
}

func resetSupportAction(s *supportTurn) {
	s.Plan.Action = nil
	s.Next.Action = s.Before.Clone().Action
	if s.Plan.GoalQuote != "" && s.Plan.GoalQuote != s.Before.Goal {
		s.Next.Action = nil
	}
}

// Revisions are held until the complete response is checked. Read action
// metadata AFTER speech in that same response, without another model call or
// another state advance. Approved streaming continues using its prefix plan.
func (r *Runtime) reconcileSupportAction(t *Turn, s *supportTurn, raw, speech string) {
	var tail map[string]json.RawMessage
	_ = json.Unmarshal([]byte(raw), &tail)
	value, present := tail["final_action"]
	if !present {
		if s.Plan.Action != nil && !strings.Contains(speech, s.Plan.Action.Text) {
			resetSupportAction(s)
			r.Emit(t, "intervention.action_unrecorded", map[string]any{"reason": "corrected_speech_changed_proposal"})
		}
		return
	}
	resetSupportAction(s)
	var action *intervention.Proposal
	err := json.Unmarshal(value, &action)
	if err == nil && action == nil {
		return
	}
	if err == nil {
		plan := s.Plan
		plan.Action = action
		err = plan.Validate(s.User, supportActionLimits(s.User, s.Before))
	}
	if err != nil || action == nil || !strings.Contains(speech, action.Text) {
		r.Emit(t, "intervention.action_unrecorded", map[string]any{"reason": "invalid_final_proposal"})
		return
	}
	s.Plan.Action = action
	s.Next.Action = &intervention.Action{ID: s.Next.Execution.ID + "-action", Text: action.Text, Minutes: action.Minutes, Status: "proposed", Source: action.ConsentQuote}
}
