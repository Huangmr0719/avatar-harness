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
