// Package promptcompare defines the controlled comparison protocol. It does
// not select prompts, call providers, or change a user's conversation.
package promptcompare

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"avatarharness/internal/intervention"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type Check struct {
	ID    string   `json:"id"`
	Label string   `json:"label"`
	Kind  string   `json:"kind"`
	Terms []string `json:"terms,omitempty"`
	Limit int      `json:"limit,omitempty"`
}
type Turn struct {
	User            string  `json:"user"`
	ReferenceAnswer string  `json:"reference_answer"`
	Checks          []Check `json:"checks"`
}
type Scenario struct {
	ID         string                   `json:"id"`
	Title      string                   `json:"title"`
	Mode       intervention.Mode        `json:"mode"`
	Goal       string                   `json:"goal,omitempty"`
	History    []Message                `json:"history,omitempty"`
	Signals    map[string]any           `json:"signals,omitempty"`
	Parameters *intervention.Parameters `json:"parameters,omitempty"`
	Turns      []Turn                   `json:"turns"`
}
type Request struct {
	ScenarioID  string    `json:"scenario_id,omitempty"`
	Scenario    *Scenario `json:"scenario,omitempty"`
	CharacterID string    `json:"character_id,omitempty"`
	Repeats     int       `json:"repeats,omitempty"`
}
type CheckResult struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Kind   string `json:"kind"`
	Status string `json:"status"` // pass, fail, manual, not_run
	Detail string `json:"detail"`
}
type Phase struct {
	Name      string  `json:"name"`
	OK        bool    `json:"ok"`
	ElapsedMS float64 `json:"elapsed_ms"`
}
type Leg struct {
	Variant         string              `json:"variant"`
	Status          string              `json:"status"`
	Text            string              `json:"text"`
	Candidate       string              `json:"candidate,omitempty"`
	CandidateChecks []CheckResult       `json:"candidate_checks,omitempty"`
	Checks          []CheckResult       `json:"checks,omitempty"`
	FirstTextMS     *float64            `json:"first_text_ms,omitempty"`
	CompletionMS    *float64            `json:"completion_ms,omitempty"`
	Revisions       int                 `json:"revisions"`
	RepairRequests  int                 `json:"repair_requests"`
	Requests        int                 `json:"requests"` // excludes the risk request shared by both legs
	Fallback        bool                `json:"fallback"`
	Error           string              `json:"error,omitempty"`
	ConditionsHash  string              `json:"conditions_hash,omitempty"`
	State           *intervention.State `json:"state,omitempty"`
	Phases          []Phase             `json:"phases,omitempty"`
}
type Round struct {
	Index           int       `json:"index"`
	Repeat          int       `json:"repeat"`
	Turn            int       `json:"turn"`
	User            string    `json:"user"`
	History         []Message `json:"history"`
	ReferenceAnswer string    `json:"reference_answer"`
	Risk            string    `json:"risk,omitempty"`
	RiskError       string    `json:"risk_error,omitempty"`
	RiskMS          float64   `json:"risk_ms"`
	ConditionsMatch bool      `json:"conditions_match"`
	Legacy          Leg       `json:"legacy"`
	Current         Leg       `json:"current"`
}
type Variant struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Behavior string `json:"behavior"`
	Voice    string `json:"voice"`
	SHA256   string `json:"sha256"`
}
type Conditions struct {
	Model                  string         `json:"model"`
	Parameters             map[string]any `json:"parameters"`
	ProviderParametersHash string         `json:"provider_parameters_hash"`
	CharacterID            string         `json:"character_id"`
	PersonaHash            string         `json:"persona_hash"`
	CommonProtocolHash     string         `json:"common_protocol_hash"`
	HistoryLimit           int            `json:"history_limit"`
	Tokens                 map[string]int `json:"tokens"`
	Protocol               string         `json:"protocol"`
}
type Run struct {
	ID         string     `json:"id"`
	Revision   int        `json:"revision"`
	Status     string     `json:"status"`
	Scenario   Scenario   `json:"scenario"`
	Repeats    int        `json:"repeats"`
	Conditions Conditions `json:"conditions"`
	Variants   []Variant  `json:"variants"`
	Rounds     []Round    `json:"rounds"`
	Error      string     `json:"error,omitempty"`
	CreatedAt  string     `json:"created_at"`
}

func Digest(value any) string {
	raw, _ := json.Marshal(value)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func (s Scenario) Validate() error {
	if strings.TrimSpace(s.Title) == "" || len([]rune(s.Title)) > 100 || !intervention.ValidMode(s.Mode) || len(s.Turns) < 1 || len(s.Turns) > 6 || len(s.History) > 40 || len([]rune(s.Goal)) > 160 {
		return fmt.Errorf("对话需有标题、有效支持方式和 1–6 轮输入")
	}
	if s.Parameters != nil {
		if err := s.Parameters.Validate(); err != nil {
			return err
		}
	}
	for _, m := range s.History {
		if (m.Role != "user" && m.Role != "assistant") || strings.TrimSpace(m.Content) == "" || utf8.RuneCountInString(m.Content) > 2000 {
			return fmt.Errorf("共同历史只接受有内容的用户/助手消息，每条最多 2000 字")
		}
	}
	for _, t := range s.Turns {
		if strings.TrimSpace(t.User) == "" || strings.HasPrefix(strings.TrimSpace(t.User), "/") || utf8.RuneCountInString(t.User) > 2000 || utf8.RuneCountInString(t.ReferenceAnswer) > 2000 || len(t.Checks) > 10 {
			return fmt.Errorf("每轮需有非命令输入，最多 2000 字、10 个检查项")
		}
		seen := map[string]bool{}
		for _, c := range t.Checks {
			if c.ID == "" || seen[c.ID] || c.Label == "" || len(c.Label) > 400 || len(c.Terms) > 20 {
				return fmt.Errorf("检查项需有唯一 ID 与说明")
			}
			seen[c.ID] = true
			switch c.Kind {
			case "contains_all", "contains_any", "excludes", "one_of", "risk_is", "state_is":
				if len(c.Terms) == 0 {
					return fmt.Errorf("%s 缺少检查内容", c.Label)
				}
			case "max_questions", "sentences", "max_characters", "action_budget":
				if c.Limit < 0 || c.Limit > 2000 {
					return fmt.Errorf("检查数值无效")
				}
			case "manual":
			default:
				return fmt.Errorf("不支持的检查类型：%s", c.Kind)
			}
			for _, term := range c.Terms {
				if term == "" || utf8.RuneCountInString(term) > 160 {
					return fmt.Errorf("检查词需为 1–160 字")
				}
			}
		}
	}
	if raw, err := json.Marshal(s.Signals); err != nil || len(raw) > 16<<10 {
		return fmt.Errorf("感知夹具过大")
	}
	return nil
}

func normalized(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || unicode.IsPunct(r) {
			return -1
		}
		return r
	}, s)
}
func sentenceCount(s string) int {
	n, pending := 0, false
	for _, r := range s {
		if strings.ContainsRune("。！？!?", r) {
			if pending {
				n++
				pending = false
			}
		} else if !unicode.IsSpace(r) {
			pending = true
		}
	}
	if pending {
		n++
	}
	return n
}

// Semantic checks stay explicitly manual. Program counts never certify empathy
// or absence of invented causes, and a missing answer cannot pass a prohibition.
func Evaluate(checks []Check, text string, state *intervention.State, risk string, complete bool) []CheckResult {
	out := make([]CheckResult, 0, len(checks))
	for _, c := range checks {
		r := CheckResult{ID: c.ID, Label: c.Label, Kind: c.Kind, Status: "not_run", Detail: "尚未完成"}
		if !complete {
			out = append(out, r)
			continue
		}
		if c.Kind == "manual" {
			r.Status = "manual"
			r.Detail = "需人工结合原话与上下文评阅"
			out = append(out, r)
			continue
		}
		ok := strings.TrimSpace(text) != ""
		detail := ""
		switch c.Kind {
		case "contains_all":
			for _, v := range c.Terms {
				ok = ok && strings.Contains(text, v)
			}
			detail = "需包含：" + strings.Join(c.Terms, "、")
		case "contains_any":
			found := false
			for _, v := range c.Terms {
				found = found || strings.Contains(text, v)
			}
			ok = ok && found
			detail = "至少包含：" + strings.Join(c.Terms, " / ")
		case "excludes":
			for _, v := range c.Terms {
				ok = ok && !strings.Contains(text, v)
			}
			detail = "禁用表述：" + strings.Join(c.Terms, "、")
		case "one_of":
			found := false
			for _, v := range c.Terms {
				found = found || normalized(text) == normalized(v)
			}
			ok = ok && found
			detail = "只回答：" + strings.Join(c.Terms, " / ")
		case "max_questions":
			n := strings.Count(text, "?") + strings.Count(text, "？")
			ok = ok && n <= c.Limit
			detail = fmt.Sprintf("问号计数 %d，上限 %d；复合问句另需人工检查", n, c.Limit)
		case "sentences":
			n := sentenceCount(text)
			ok = ok && n == c.Limit
			detail = fmt.Sprintf("按中文句末标点计数 %d，要求 %d", n, c.Limit)
		case "max_characters":
			n := utf8.RuneCountInString(text)
			ok = ok && n <= c.Limit
			detail = fmt.Sprintf("%d 字，上限 %d", n, c.Limit)
		case "action_budget":
			ok = ok && state != nil && state.Action != nil && state.Action.Minutes <= c.Limit
			if state != nil && state.Action != nil {
				detail = fmt.Sprintf("已记录行动 %d 分钟，上限 %d；可执行性需人工评阅", state.Action.Minutes, c.Limit)
			} else {
				detail = "没有可核验的结构化行动记录"
			}
		case "risk_is":
			found := false
			for _, v := range c.Terms {
				found = found || risk == v
			}
			ok = ok && found
			detail = "共同风险结果：" + risk
		case "state_is":
			found := false
			if state != nil {
				for _, v := range c.Terms {
					found = found || state.Status == v
				}
				detail = "实际状态：" + state.Status
			}
			ok = ok && found
		}
		r.Status = "fail"
		if ok {
			r.Status = "pass"
		}
		r.Detail = detail
		out = append(out, r)
	}
	return out
}

//go:embed scenarios.json
var scenarioData []byte

func Scenarios() []Scenario {
	var out []Scenario
	if err := json.Unmarshal(scenarioData, &out); err != nil {
		panic(err)
	}
	return out
}
