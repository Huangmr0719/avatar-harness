package intervention

import (
	"fmt"
	"math"
	"time"
)

type Mode string

const (
	Mindfulness Mode = "mindfulness"
	Humanistic  Mode = "humanistic"
	CBT         Mode = "cbt"
)

func ValidMode(mode Mode) bool { return mode == Mindfulness || mode == Humanistic || mode == CBT }

// Parameters describe the intervention's actual burden. Avatar affect has its
// own emotion/intensity protocol and never changes these parameters.
type Parameters struct {
	DurationSeconds int     `json:"duration_seconds"`
	PlaybackRate    float64 `json:"playback_rate"`
	MaxCharacters   int     `json:"max_characters"`
	QuestionDepth   int     `json:"question_depth"`
	QuestionEvery   int     `json:"question_every"`
	ActionMinutes   int     `json:"action_minutes"`
}

func Defaults(mode Mode) Parameters {
	p := Parameters{DurationSeconds: 60, PlaybackRate: 1, MaxCharacters: 160, QuestionDepth: 1, QuestionEvery: 2, ActionMinutes: 2}
	if mode == CBT {
		p.MaxCharacters, p.QuestionDepth, p.QuestionEvery = 200, 2, 1
	}
	return p
}

func (p Parameters) Validate() error {
	if math.IsNaN(p.PlaybackRate) || math.IsInf(p.PlaybackRate, 0) {
		return fmt.Errorf("支持参数必须是有限数值")
	}
	if p.DurationSeconds < 15 || p.DurationSeconds > 90 || p.PlaybackRate < .75 || p.PlaybackRate > 1.25 || p.MaxCharacters < 60 || p.MaxCharacters > 400 || p.QuestionDepth < 0 || p.QuestionDepth > 2 || p.QuestionEvery < 1 || p.QuestionEvery > 4 || p.ActionMinutes < 1 || p.ActionMinutes > 5 {
		return fmt.Errorf("支持参数超出允许范围")
	}
	return nil
}

type Evidence struct {
	Quote  string `json:"quote"`
	TurnID string `json:"turn_id"`
}

type Action struct {
	ID      string `json:"id"`
	Text    string `json:"text"`
	Minutes int    `json:"minutes"`
	Status  string `json:"status"`
	Source  string `json:"source"`
}

type Execution struct {
	ID               string     `json:"id"`
	TurnID           string     `json:"turn_id,omitempty"`
	Mode             Mode       `json:"mode"`
	Kind             string     `json:"kind"`
	CardID           string     `json:"card_id"`
	Status           string     `json:"status"`
	Parameters       Parameters `json:"parameters"`
	Presented        bool       `json:"presented"`
	PlayedMS         int        `json:"played_ms"`
	FeedbackRecorded bool       `json:"feedback_recorded"`
	StartedAt        int64      `json:"started_at,omitempty"`
}

type Decision struct {
	Kind   string `json:"kind"`
	Reason string `json:"reason"`
	Source string `json:"source"`
	At     int64  `json:"at"`
}

type State struct {
	Version        uint64              `json:"version"`
	Mode           Mode                `json:"mode"`
	Status         string              `json:"status"`
	Goal           string              `json:"goal"`
	Stage          string              `json:"stage"`
	Parameters     Parameters          `json:"parameters"`
	Facts          map[string]Evidence `json:"facts"`
	DialogueTurns  int                 `json:"dialogue_turns"`
	NoHelpCount    int                 `json:"no_help_count"`
	SuggestedMode  Mode                `json:"suggested_mode,omitempty"`
	Execution      *Execution          `json:"execution,omitempty"`
	Action         *Action             `json:"action,omitempty"`
	LastDecision   Decision            `json:"last_decision"`
	LastFeedback   string              `json:"last_feedback,omitempty"`
	LastAdjustment int                 `json:"last_adjustment_turn"`
}

func (s State) Active() bool { return ValidMode(s.Mode) && s.Status != "stopped" && s.Status != "idle" }

func (s State) Clone() State {
	facts := map[string]Evidence{}
	for key, value := range s.Facts {
		facts[key] = value
	}
	s.Facts = facts
	if s.Execution != nil {
		value := *s.Execution
		s.Execution = &value
	}
	if s.Action != nil {
		value := *s.Action
		s.Action = &value
	}
	return s
}

type Command struct {
	Operation   string      `json:"operation"`
	Version     uint64      `json:"version"`
	Mode        Mode        `json:"mode,omitempty"`
	Goal        string      `json:"goal,omitempty"`
	Parameters  *Parameters `json:"parameters,omitempty"`
	ExecutionID string      `json:"execution_id,omitempty"`
	Feedback    string      `json:"feedback,omitempty"`
	Event       string      `json:"event,omitempty"`
	PlayedMS    int         `json:"played_ms,omitempty"`
	Source      string      `json:"source,omitempty"`
}

func Apply(s State, command Command, now time.Time, executionID string) (State, error) {
	if command.Version != s.Version {
		return s, fmt.Errorf("支持状态已更新，请刷新后重试")
	}
	next := s.Clone()
	next.Version++
	next.LastDecision = Decision{Kind: command.Operation, Source: command.Source, At: now.UnixMilli()}
	switch command.Operation {
	case "start":
		if !ValidMode(command.Mode) {
			return s, fmt.Errorf("请选择有效的支持方式")
		}
		next.Mode, next.Status, next.Stage = command.Mode, "ready", "listening"
		next.Parameters = Defaults(command.Mode)
		if command.Parameters != nil {
			next.Parameters = *command.Parameters
		}
		if err := next.Parameters.Validate(); err != nil {
			return s, err
		}
		if command.Goal != "" {
			if command.Goal != s.Goal {
				next.Facts, next.Action = map[string]Evidence{}, nil
			}
			next.Goal = command.Goal
		}
		next.SuggestedMode, next.NoHelpCount, next.LastFeedback = "", 0, ""
		next.Execution = nil
		if command.Mode == CBT {
			next.Stage = "situation"
		}
		if command.Mode == Mindfulness {
			next.Stage = "attention"
			next.Execution = &Execution{ID: executionID, Mode: Mindfulness, Kind: "video", CardID: "attention-observation", Status: "queued", Parameters: next.Parameters}
		}
		next.LastDecision.Reason = "用户选择支持方式与可接受参数"
	case "stop":
		next.Status, next.SuggestedMode = "stopped", ""
		if next.Execution != nil {
			next.Execution.Status = "interrupted"
		}
		next.LastDecision.Reason = "用户停止支持路径"
	case "parameters":
		if !s.Active() || command.Parameters == nil {
			return s, fmt.Errorf("请先选择支持方式")
		}
		if err := command.Parameters.Validate(); err != nil {
			return s, err
		}
		next.Parameters = *command.Parameters
		next.Status = "ready"
		if next.Execution != nil {
			next.Execution.Status = "interrupted"
		}
		next.LastDecision.Reason = "用户明确调整干预参数"
	case "presented", "playback":
		if next.Execution == nil || next.Execution.ID != command.ExecutionID || next.Execution.Status == "interrupted" || !s.Active() {
			return s, fmt.Errorf("该次支持已结束或被替换")
		}
		e := next.Execution
		if command.Operation == "presented" {
			if e.Kind == "video" {
				return s, fmt.Errorf("视频需要实际播放记录")
			}
			e.Presented, e.Status = true, "presented"
		} else {
			if e.Kind != "video" || command.PlayedMS < e.PlayedMS || command.PlayedMS > e.Parameters.DurationSeconds*1000+1500 {
				return s, fmt.Errorf("播放记录与执行参数不匹配")
			}
			switch command.Event {
			case "playing":
				if e.Status != "queued" {
					return s, fmt.Errorf("该次视频已经开始或结束")
				}
				e.Status, e.Presented, e.StartedAt = "playing", true, now.UnixMilli()
			case "completed":
				if e.Status != "playing" || command.PlayedMS < e.Parameters.DurationSeconds*1000-500 || now.UnixMilli()-e.StartedAt < int64(command.PlayedMS)-1000 {
					return s, fmt.Errorf("尚未观察到足够的实际播放时间")
				}
				e.Status = "completed"
			case "stopped":
				e.Status = "interrupted"
			default:
				return s, fmt.Errorf("播放事件无效")
			}
			e.PlayedMS = command.PlayedMS
		}
		next.Status = "awaiting_feedback"
		next.LastDecision.Reason = "客户端确认内容呈现或实际播放"
	case "feedback":
		if next.Execution == nil || next.Execution.ID != command.ExecutionID || !next.Execution.Presented || next.Execution.FeedbackRecorded || !s.Active() {
			return s, fmt.Errorf("请对当前已呈现的支持内容反馈一次")
		}
		if command.Feedback != "helpful" && command.Feedback != "no_help" && command.Feedback != "too_much" && command.Feedback != "worse" {
			return s, fmt.Errorf("反馈类型无效")
		}
		next.Execution.FeedbackRecorded = true
		if next.Execution.Status == "playing" {
			next.Execution.Status = "interrupted"
		}
		next.LastFeedback, next.Status = command.Feedback, "ready"
		if command.Feedback != "no_help" {
			next.NoHelpCount = 0
			next.SuggestedMode = ""
		}
		switch command.Feedback {
		case "helpful":
			next.NoHelpCount = 0
			next.SuggestedMode = ""
			next.LastDecision.Reason = "用户表示有帮助，保持当前方法与负担"
		case "no_help":
			next.NoHelpCount++
			next.LastDecision.Reason = "用户表示无帮助，先回顾适配性"
			if next.NoHelpCount >= 2 {
				next.SuggestedMode = Humanistic
				if next.Mode == Humanistic {
					next.SuggestedMode = CBT
				}
				next.LastDecision.Reason = "连续反馈无帮助，建议协商其他方式，等待用户选择"
			}
		case "too_much":
			if next.Mode == Mindfulness {
				next.Parameters.DurationSeconds = max(15, next.Parameters.DurationSeconds-15)
			} else if next.Mode == CBT && next.Parameters.QuestionDepth > 0 {
				next.Parameters.QuestionDepth--
			} else {
				next.Parameters.MaxCharacters = max(60, next.Parameters.MaxCharacters-40)
			}
			next.LastAdjustment = next.DialogueTurns
			next.LastDecision.Reason = "用户明确表示负担过大，降低一个可执行参数"
		case "worse":
			next.Status = "paused"
			next.LastDecision.Reason = "用户反馈体验变差，暂停并重新确认需要"
		}
	case "action_agree", "action_done", "action_not_done":
		if next.Action == nil || command.ExecutionID != next.Action.ID || !s.Active() {
			return s, fmt.Errorf("当前没有可更新的行动")
		}
		switch command.Operation {
		case "action_agree":
			if next.Action.Status != "proposed" {
				return s, fmt.Errorf("该行动已处理")
			}
			next.Action.Status = "agreed"
		case "action_done", "action_not_done":
			if next.Action.Status != "agreed" {
				return s, fmt.Errorf("请先确认愿意尝试该行动")
			}
			next.Action.Status = "reported_done"
			if command.Operation == "action_not_done" {
				next.Action.Status = "reported_not_done"
			}
			next.Stage = "review"
		}
		next.Action.Source = command.Source
		next.LastDecision.Reason = "依据用户明确认可或完成反馈更新行动"
	default:
		return s, fmt.Errorf("支持操作无效")
	}
	return next, nil
}
