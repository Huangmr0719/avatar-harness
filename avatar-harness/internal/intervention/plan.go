package intervention

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

type Fact struct {
	Key   string `json:"key"`
	Quote string `json:"quote"`
}
type Proposal struct {
	Text         string `json:"text"`
	Minutes      int    `json:"minutes"`
	ConsentQuote string `json:"consent_quote"`
}
type Plan struct {
	Risk          string    `json:"risk"`
	RiskQuote     string    `json:"risk_quote"`
	Intent        string    `json:"intent"`
	Facts         []Fact    `json:"facts"`
	GoalQuote     string    `json:"goal_quote"`
	Feedback      string    `json:"feedback"`
	FeedbackQuote string    `json:"feedback_quote"`
	Action        *Proposal `json:"action"`
}

func ExactQuote(text, quote string) bool {
	return strings.TrimSpace(quote) != "" && strings.Contains(text, quote)
}

func (p Plan) Validate(text string, s State) error {
	if p.Risk != "ordinary" && p.Risk != "research" && p.Risk != "uncertain" && p.Risk != "urgent" {
		return fmt.Errorf("缺少有效风险判定")
	}
	if (p.Risk == "urgent" || p.Risk == "uncertain") && !ExactQuote(text, p.RiskQuote) {
		return fmt.Errorf("风险判定缺少当前原话")
	}
	switch p.Intent {
	case "answer", "listen", "clarify", "explore", "suggest", "review", "stop":
	default:
		return fmt.Errorf("回应意图无效")
	}
	for _, f := range p.Facts {
		switch f.Key {
		case "situation", "thought", "evidence", "alternative", "constraints", "need":
		default:
			return fmt.Errorf("事实字段无效")
		}
		if !ExactQuote(text, f.Quote) {
			return fmt.Errorf("事实缺少当前用户原话")
		}
	}
	if p.GoalQuote != "" && !ExactQuote(text, p.GoalQuote) {
		return fmt.Errorf("目标缺少当前用户原话")
	}
	if p.Feedback != "" && (!ExactQuote(text, p.FeedbackQuote) || (p.Feedback != "helpful" && p.Feedback != "no_help" && p.Feedback != "too_much" && p.Feedback != "worse")) {
		return fmt.Errorf("反馈缺少有效来源")
	}
	if p.Action != nil && (p.Intent != "suggest" || !ExactQuote(text, p.Action.ConsentQuote) || p.Action.Minutes < 1 || p.Action.Minutes > s.Parameters.ActionMinutes || utf8.RuneCountInString(p.Action.Text) > 100 || strings.TrimSpace(p.Action.Text) == "") {
		return fmt.Errorf("行动缺少意愿或超出负担范围")
	}
	return nil
}

func Advance(s State, p Plan, turnID, executionID string, now time.Time) State {
	n := s.Clone()
	if p.Feedback != "" && n.Execution != nil && n.Execution.Presented && !n.Execution.FeedbackRecorded {
		if updated, err := Apply(n, Command{Operation: "feedback", Version: n.Version, ExecutionID: n.Execution.ID, Feedback: p.Feedback, Source: p.FeedbackQuote}, now, ""); err == nil {
			n = updated
		}
	}
	if p.GoalQuote != "" && p.GoalQuote != s.Goal {
		n.Goal, n.Facts, n.Action = p.GoalQuote, map[string]Evidence{}, nil
	}
	for _, f := range p.Facts {
		n.Facts[f.Key] = Evidence{Quote: f.Quote, TurnID: turnID}
	}
	n.DialogueTurns++
	n.Version = s.Version + 1
	if p.Intent == "stop" {
		n.Status = "stopped"
	}
	if p.Risk == "urgent" || p.Risk == "uncertain" {
		n.Status = "paused"
	}
	if n.Status != "paused" && n.Status != "stopped" {
		n.Status = "awaiting_feedback"
	}
	if n.Mode == CBT && p.Intent != "answer" && p.Intent != "listen" {
		n.Stage = "situation"
		if n.Facts["situation"].Quote != "" {
			n.Stage = "thought"
		}
		if n.Facts["thought"].Quote != "" {
			n.Stage = "evidence"
		}
		if n.Facts["evidence"].Quote != "" {
			n.Stage = "alternatives"
		}
		if n.Facts["alternative"].Quote != "" {
			n.Stage = "action"
		}
		if p.Intent == "review" {
			n.Stage = "review"
		}
	}
	card := Select(n, p.Intent)
	n.Execution = &Execution{ID: executionID, TurnID: turnID, Mode: n.Mode, Kind: "dialogue", CardID: card.ID, Status: "generated", Parameters: n.Parameters}
	// A video remains a separate, explicitly started execution. Dialogue about a
	// video is not evidence that the video played.
	if p.Action != nil {
		n.Action = &Action{ID: executionID + "-action", Text: p.Action.Text, Minutes: p.Action.Minutes, Status: "proposed", Source: p.Action.ConsentQuote}
	}
	n.LastDecision = Decision{Kind: p.Intent, Reason: "依据当前原话、已知事实和方法卡选择；风险=" + p.Risk, Source: turnID, At: now.UnixMilli()}
	return n
}

func AllowQuestion(s State, p Plan) bool {
	return s.Status != "paused" && p.Intent != "answer" && p.Intent != "listen" && p.Intent != "stop" && s.Parameters.QuestionDepth > 0 && (s.DialogueTurns-1)%s.Parameters.QuestionEvery == 0
}

func CheckOutput(s State, p Plan, text string) []string {
	issues := []string{}
	if strings.TrimSpace(text) == "" {
		issues = append(issues, "empty")
	}
	if utf8.RuneCountInString(text) > s.Parameters.MaxCharacters {
		issues = append(issues, "character_budget")
	}
	questions := strings.Count(text, "?") + strings.Count(text, "？")
	if questions > 1 || (questions > 0 && !AllowQuestion(s, p) && p.Risk != "uncertain" && p.Risk != "urgent") {
		issues = append(issues, "question_budget")
	}
	if p.Action != nil && !strings.Contains(text, p.Action.Text) {
		issues = append(issues, "action_mismatch")
	}
	return issues
}

const UrgentResponse = "你说自己现在可能受伤，先把安全放在第一位。请立即联系身边可信任的人来陪你，并联系当地急救或前往急诊；如果能安全做到，先远离可能伤害自己的物品。你现在身边有人能过来吗？"
const UncertainResponse = "我想先确认你的安全：你是在描述自己现在可能会伤害自己，还是在讨论别人的经历或研究内容？"
