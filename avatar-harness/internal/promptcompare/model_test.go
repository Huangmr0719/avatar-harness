package promptcompare

import (
	"avatarharness/internal/intervention"
	"testing"
)

func TestChecksReportViolationsAndKeepSemanticItemsManual(t *testing.T) {
	checks := []Check{{ID: "q", Label: "不追问", Kind: "max_questions", Limit: 0}, {ID: "n", Label: "只报数字", Kind: "one_of", Terms: []string{"56", "五十六"}}, {ID: "m", Label: "不推断", Kind: "manual"}}
	got := Evaluate(checks, "五十六。", nil, "ordinary", true)
	if got[0].Status != "pass" || got[1].Status != "pass" || got[2].Status != "manual" {
		t.Fatal(got)
	}
	bad := Evaluate(checks, "56。你还有其他问题吗？", nil, "ordinary", true)
	if bad[0].Status != "fail" || bad[1].Status != "fail" {
		t.Fatal(bad)
	}
	for _, v := range Evaluate(checks, "", nil, "", true) {
		if v.Status == "pass" {
			t.Fatal("missing response passed", v)
		}
	}
	for _, v := range Evaluate(checks, "已经有一段文字", nil, "", false) {
		if v.Status != "not_run" {
			t.Fatal("partial result certified", v)
		}
	}
}
func TestActionsAndSentenceChecksUseActualEvidence(t *testing.T) {
	checks := []Check{{ID: "s", Label: "两句", Kind: "sentences", Limit: 2}, {ID: "a", Label: "一分钟行动", Kind: "action_budget", Limit: 1}}
	got := Evaluate(checks, "先写下一个事实。下次核对实际结果。", nil, "ordinary", true)
	if got[0].Status != "pass" || got[1].Status != "fail" {
		t.Fatal(got)
	}
	s := &intervention.State{Action: &intervention.Action{Minutes: 2}}
	if Evaluate(checks, "先写下一个事实。下次核对实际结果。", s, "ordinary", true)[1].Status != "fail" {
		t.Fatal("over-budget action passed")
	}
}
func TestDevelopmentScenariosAreValid(t *testing.T) {
	for _, s := range Scenarios() {
		if err := s.Validate(); err != nil {
			t.Fatal(s.ID, err)
		}
	}
}

func TestCalibrationSeparatesMeaningWordsRecordsAndSafety(t *testing.T) {
	checks := []Check{
		{ID: "meaning", Label: "不实际祝贺", Kind: "manual"},
		{ID: "word", Label: "词面禁用", Kind: "excludes", Terms: []string{"祝贺"}},
		{ID: "action", Label: "已记录行动", Kind: "action_budget", Limit: 1},
		{ID: "risk", Label: "共同安全检查", Kind: "risk_is", Terms: []string{"ordinary"}},
	}
	got := Evaluate(checks, "好，那就不说祝贺。可以用一分钟写下一个事实。", nil, "ordinary", true)
	if got[0].Status != "manual" || got[0].Scope != "semantic" || got[1].Status != "fail" || got[1].Scope != "literal" || got[2].Scope != "record" || got[3].Scope != "safety" {
		t.Fatal("meaning conflated with words, records or common safety", got)
	}
	state := &intervention.State{Action: &intervention.Action{Text: "未出现在正文的动作", Minutes: 1}}
	if Evaluate(checks, "用一分钟写下一个事实。", state, "ordinary", true)[2].Status != "fail" {
		t.Fatal("unrelated action record passed")
	}
	state.Action.Text = "写下一个事实"
	if Evaluate(checks, "用一分钟写下一个事实。", state, "ordinary", true)[2].Status != "pass" {
		t.Fatal("matching record failed")
	}
	for _, scenario := range Scenarios() {
		if scenario.ID == "D05" && scenario.Turns[0].Checks[0].Kind != "manual" {
			t.Fatal("congratulation requires semantic review")
		}
		if scenario.ID == "D03" && scenario.Turns[1].Checks[0].Kind != "manual" {
			t.Fatal("no question is not no repeated evidence")
		}
	}
}
