package runtime

import (
	"encoding/json"
	"strings"
	"time"

	"avatarharness/internal/intervention"
)

// Only deterministic limits are checked here. Optional model annotations do
// not become a second semantic gate for speech already being streamed.
func supportOutputIssues(s *supportTurn, text string) []string {
	preview := intervention.Plan{Intent: "explore", Risk: s.Plan.Risk}
	issues := intervention.CheckOutput(s.Next, preview, text)
	return append(issues, intervention.CurrentResponseConstraints(s.User).Check(text)...)
}

func supportActionLimits(user string, state intervention.State) intervention.State {
	if minutes := intervention.CurrentResponseConstraints(user).ActionMinutes; minutes > 0 {
		state.Parameters.ActionMinutes = min(minutes, state.Parameters.ActionMinutes)
	}
	return state
}

// Metadata is an optional tail of the SAME generation, never another request.
// Missing or invalid annotations do not rewrite or block a valid answer.
func (r *Runtime) applySupportMetadata(t *Turn, s *supportTurn, raw json.RawMessage, speech, executionID string) {
	p := intervention.Plan{Intent: "answer"}
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &p); err != nil {
			p = intervention.Plan{Intent: "answer"}
			r.Emit(t, "intervention.plan_filtered", map[string]any{"fields": []string{"support"}})
		} else {
			normalized, err := r.normalizeSupportPlan(t, s.User, p, s.Before)
			if err != nil {
				p = intervention.Plan{Intent: "answer"}
				r.Emit(t, "intervention.plan_filtered", map[string]any{"fields": []string{"support.intent"}})
			} else {
				p = normalized
			}
		}
	}
	if p.Action != nil && !strings.Contains(speech, p.Action.Text) {
		p.Action = nil
		r.Emit(t, "intervention.action_unrecorded", map[string]any{"reason": "proposal_missing_from_speech"})
	}
	// A scheduled video remains a separate execution, not a life-action proposal.
	if control := r.scheduledControl(t); control != nil && control.PlayVideo {
		p.Intent, p.Action = "answer", nil
	}
	p.Risk, p.RiskQuote = s.Plan.Risk, s.Plan.RiskQuote
	s.Plan = p
	s.Next = intervention.Advance(s.Before, p, t.ID, executionID, time.Now())
}
