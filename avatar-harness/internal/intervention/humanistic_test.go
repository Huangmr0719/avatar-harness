package intervention

import "testing"

func TestHumanisticMovesFollowIntentWithoutCBTStageProgression(t *testing.T) {
	for _, tc := range []struct{ intent, card string }{
		{"listen", "reflect-need"}, {"answer", "reflect-need"},
		{"clarify", "verify-understanding"}, {"explore", "explore-meaning"},
		{"stop", "respect-direction"},
	} {
		t.Run(tc.intent, func(t *testing.T) {
			s := startMode(t, Humanistic)
			p := Plan{Risk: "ordinary", Intent: tc.intent, Facts: []Fact{{Key: "need", Quote: "想自己决定"}, {Key: "feeling", Quote: "不是害怕，是累"}, {Key: "preference", Quote: "暂时不要建议"}}}
			if err := p.Validate("我想自己决定，不是害怕，是累，暂时不要建议。", s); err != nil {
				t.Fatal(err)
			}
			next := Advance(s, p, "turn-1", "execution", epoch)
			if next.Execution.CardID != tc.card || next.Stage != "listening" || next.Facts["feeling"].Quote != "不是害怕，是累" {
				t.Fatal(next)
			}
			if next.Action != nil || (tc.intent == "stop" && next.Active()) {
				t.Fatal("humanistic dialogue forced an action or ignored stop", next)
			}
		})
	}
}

func TestHumanisticQuestionsRespectListeningAndBurden(t *testing.T) {
	s := startMode(t, Humanistic)
	s.DialogueTurns = 1
	for _, intent := range []string{"answer", "listen", "stop"} {
		if AllowQuestion(s, Plan{Intent: intent}) || len(CheckOutput(s, Plan{Intent: intent}, "这对你意味着什么？")) == 0 {
			t.Fatal("question permitted after a non-question intent", intent)
		}
	}
	if !AllowQuestion(s, Plan{Intent: "clarify"}) {
		t.Fatal("voluntary clarification was disabled")
	}
	s.Parameters.QuestionDepth = 0
	if AllowQuestion(s, Plan{Intent: "clarify"}) {
		t.Fatal("zero question depth ignored")
	}
	s.Parameters.QuestionDepth = 1
	s.DialogueTurns = 2
	if AllowQuestion(s, Plan{Intent: "explore"}) {
		t.Fatal("question frequency ignored")
	}
}

func TestHumanisticUnderstandingRequiresCurrentUserEvidence(t *testing.T) {
	s := startMode(t, Humanistic)
	for _, key := range []string{"need", "feeling", "preference"} {
		p := Plan{Risk: "ordinary", Intent: "listen", Facts: []Fact{{Key: key, Quote: "怕被抛弃"}}}
		if p.Validate("我希望先讲完经过。", s) == nil {
			t.Fatal("unsupported understanding became a fact", key)
		}
	}
}
