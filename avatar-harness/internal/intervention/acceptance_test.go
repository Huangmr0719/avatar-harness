package intervention

import (
	"math"
	"strings"
	"testing"
	"time"
)

var epoch = time.Unix(100, 0)

func startMode(t *testing.T, mode Mode) State {
	t.Helper()
	s, err := Apply(State{}, Command{Operation: "start", Mode: mode, Goal: "梳理工作压力"}, epoch, "video-1")
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func apply(t *testing.T, s State, c Command) State {
	t.Helper()
	c.Version = s.Version
	n, err := Apply(s, c, epoch, "new-execution")
	if err != nil {
		t.Fatal(err)
	}
	return n
}
func presented(s State, id string) State {
	s.Execution = &Execution{ID: id, Mode: s.Mode, Kind: "dialogue", Status: "presented", Presented: true, Parameters: s.Parameters}
	return s
}

func TestL01ThreeDistinctPathsAndReviewedSources(t *testing.T) {
	ids := map[string]bool{}
	for _, mode := range []Mode{Mindfulness, Humanistic, CBT} {
		s := startMode(t, mode)
		card := Select(s, "clarify")
		if card.Source == "" || card.Version == "" || card.Provenance != "original_paraphrase" {
			t.Fatal(card)
		}
		ids[card.ID] = true
		if mode == Mindfulness && (s.Execution == nil || s.Execution.Kind != "video" || s.Execution.Presented) {
			t.Fatal(s)
		}
	}
	if len(ids) != 3 {
		t.Fatal("methods collapsed", ids)
	}
	for _, card := range Cards {
		if !strings.HasPrefix(card.Source, "https://") || card.Rule == "" {
			t.Fatal(card)
		}
	}
}

func TestL05L06IndependentBoundedParameters(t *testing.T) {
	for _, mode := range []Mode{Mindfulness, Humanistic, CBT} {
		s := presented(startMode(t, mode), "e")
		before := s.Parameters
		s = apply(t, s, Command{Operation: "feedback", ExecutionID: "e", Feedback: "too_much"})
		if s.Parameters == before || s.Mode != mode || s.Parameters.Validate() != nil {
			t.Fatal(s)
		}
		changes := 0
		if before.DurationSeconds != s.Parameters.DurationSeconds {
			changes++
		}
		if before.QuestionDepth != s.Parameters.QuestionDepth {
			changes++
		}
		if before.MaxCharacters != s.Parameters.MaxCharacters {
			changes++
		}
		if changes != 1 {
			t.Fatal("not a single-parameter change", before, s.Parameters)
		}
		// No avatar expression, AU or sensor score enters Apply or Parameters.
	}
	p := Defaults(CBT)
	p.PlaybackRate = math.NaN()
	if p.Validate() == nil {
		t.Fatal("non-finite rate accepted")
	}
	p = Defaults(CBT)
	p.QuestionEvery = 0
	if p.Validate() == nil {
		t.Fatal("invalid cadence accepted")
	}
	for i := 0; i < 30; i++ {
		s := presented(startMode(t, Mindfulness), "e")
		s.Parameters.DurationSeconds = 15
		s = apply(t, s, Command{Operation: "feedback", ExecutionID: "e", Feedback: "too_much"})
		if s.Parameters.DurationSeconds != 15 {
			t.Fatal(s)
		}
	}
}

func TestL04L08FeedbackHysteresisAndUserChoice(t *testing.T) {
	s := startMode(t, CBT)
	for i := 0; i < 2; i++ {
		s = presented(s, "e")
		s = apply(t, s, Command{Operation: "feedback", ExecutionID: "e", Feedback: "no_help"})
		if s.Mode != CBT || (i == 0 && s.SuggestedMode != "") {
			t.Fatal("premature switch", s)
		}
	}
	if s.SuggestedMode != Humanistic {
		t.Fatal(s)
	}
	if _, err := Apply(s, Command{Operation: "feedback", Version: s.Version, ExecutionID: "e", Feedback: "no_help"}, epoch, ""); err == nil {
		t.Fatal("duplicate feedback accepted")
	}
	s = presented(s, "e2")
	before := s.Parameters
	s = apply(t, s, Command{Operation: "feedback", ExecutionID: "e2", Feedback: "helpful"})
	if s.Parameters != before || s.NoHelpCount != 0 {
		t.Fatal(s)
	}
	s = presented(s, "e3")
	s = apply(t, s, Command{Operation: "feedback", ExecutionID: "e3", Feedback: "worse"})
	if s.Status != "paused" {
		t.Fatal(s)
	}
	if _, err := Apply(s, Command{Operation: "start", Version: 0, Mode: Humanistic}, epoch, ""); err == nil {
		t.Fatal("stale command accepted")
	}
}

func TestL04NoHelpMustBeConsecutive(t *testing.T) {
	s := presented(startMode(t, CBT), "e1")
	s = apply(t, s, Command{Operation: "feedback", ExecutionID: "e1", Feedback: "no_help"})
	s = presented(s, "e2")
	s = apply(t, s, Command{Operation: "feedback", ExecutionID: "e2", Feedback: "too_much"})
	s = presented(s, "e3")
	s = apply(t, s, Command{Operation: "feedback", ExecutionID: "e3", Feedback: "no_help"})
	if s.SuggestedMode != "" || s.NoHelpCount != 1 {
		t.Fatal("nonconsecutive reports caused a switch suggestion", s)
	}
}

func TestL01L07PlaybackRequiresObservedTimeAndCurrentExecution(t *testing.T) {
	s := startMode(t, Mindfulness)
	s = apply(t, s, Command{Operation: "playback", ExecutionID: "video-1", Event: "playing"})
	complete := Command{Operation: "playback", Version: s.Version, ExecutionID: "video-1", Event: "completed", PlayedMS: 60000}
	if _, err := Apply(s, complete, epoch, ""); err == nil {
		t.Fatal("instant fake completion accepted")
	}
	n, err := Apply(s, complete, epoch.Add(61*time.Second), "")
	if err != nil || n.Execution.Status != "completed" || n.Execution.PlayedMS != 60000 {
		t.Fatal(n, err)
	}
	if _, err := Apply(n, complete, epoch.Add(62*time.Second), ""); err == nil {
		t.Fatal("replayed completion accepted")
	}
	s = apply(t, s, Command{Operation: "stop"})
	complete.Version = s.Version
	if _, err := Apply(s, complete, epoch.Add(time.Minute), ""); err == nil {
		t.Fatal("stopped video completed")
	}
}

func TestE01S01QuotesCorrectionsAndGoalReset(t *testing.T) {
	s := startMode(t, CBT)
	p := Plan{Risk: "ordinary", Intent: "explore", Facts: []Fact{{"situation", "昨天开会卡住"}, {"constraints", "最多三分钟"}}}
	if p.Validate("昨天开会卡住，最多三分钟", s) != nil {
		t.Fatal("valid quotes rejected")
	}
	n := Advance(s, p, "turn1", "e1", epoch)
	p.Facts = []Fact{{"situation", "其实是今天，不是昨天"}}
	n = Advance(n, p, "turn2", "e2", epoch)
	if n.Facts["situation"].TurnID != "turn2" || n.Facts["constraints"].Quote != "最多三分钟" {
		t.Fatal(n)
	}
	p.Facts = []Fact{{"thought", "因为准备方向错误"}}
	if p.Validate("我不知道为什么", s) == nil {
		t.Fatal("invented explanation accepted")
	}
	p.Facts = nil
	p.GoalQuote = "现在想谈睡眠"
	n = Advance(n, p, "turn3", "e3", epoch)
	if len(n.Facts) != 0 || n.Goal != "现在想谈睡眠" {
		t.Fatal(n)
	}
}

func TestQ01Q02L02OneUnansweredOperationAndDirectAnswer(t *testing.T) {
	s := startMode(t, CBT)
	s.Facts["thought"] = Evidence{Quote: "一次失误就代表失败"}
	if Select(s, "explore").ID != "check-evidence" {
		t.Fatal(s)
	}
	s.Facts["evidence"] = Evidence{Quote: "上次出错也完成了"}
	if Select(s, "explore").ID != "other-explanation" {
		t.Fatal("answered evidence was asked again")
	}
	s.DialogueTurns = 1
	if len(CheckOutput(s, Plan{Intent: "answer"}, "结果是十二。")) != 0 || len(CheckOutput(s, Plan{Intent: "answer"}, "结果是十二。你感觉如何？")) == 0 {
		t.Fatal("direct answer question gate failed")
	}
	if len(CheckOutput(s, Plan{Intent: "explore"}, "什么时候？为什么？")) == 0 {
		t.Fatal("multiple questions accepted")
	}
	s.Parameters.QuestionEvery = 3
	s.DialogueTurns = 2
	if AllowQuestion(s, Plan{Intent: "explore"}) {
		t.Fatal("cadence ignored")
	}
}

func TestA01A02ActionConsentCompletionAndReview(t *testing.T) {
	s := startMode(t, CBT)
	p := Plan{Risk: "ordinary", Intent: "suggest", Action: &Proposal{Text: "写下一件可控的小事", Minutes: 2, ConsentQuote: "我愿意试试"}}
	if p.Validate("我愿意试试", s) != nil {
		t.Fatal(p)
	}
	s = Advance(s, p, "turn", "e", epoch)
	if s.Action.Status != "proposed" {
		t.Fatal(s)
	}
	if _, err := Apply(s, Command{Operation: "action_done", Version: s.Version, ExecutionID: s.Action.ID}, epoch, ""); err == nil {
		t.Fatal("completion before agreement")
	}
	s = apply(t, s, Command{Operation: "action_agree", ExecutionID: s.Action.ID})
	s = apply(t, s, Command{Operation: "action_not_done", ExecutionID: s.Action.ID})
	if s.Action.Status != "reported_not_done" || s.Stage != "review" || s.LastFeedback == "helpful" {
		t.Fatal(s)
	}
	p.Action.Minutes = 5
	if p.Validate("我愿意试试", s) == nil {
		t.Fatal("over-budget action accepted")
	}
}

func TestD01D02DatasetRegistryExcludesMissingProvenanceAndLeakage(t *testing.T) {
	base := Sample{ID: "s1", CaseID: "case", Kind: "synthetic", Split: "development", Source: "authored fixture", Permission: "project evaluation"}
	if ValidateSamples([]Sample{base}) != nil {
		t.Fatal(base)
	}
	bad := base
	bad.ID = "s2"
	bad.Split = "heldout"
	if ValidateSamples([]Sample{base, bad}) == nil {
		t.Fatal("case leakage")
	}
	bad = base
	bad.Permission = ""
	if ValidateSamples([]Sample{bad}) == nil {
		t.Fatal("unregistered permission")
	}
	bad = base
	bad.Kind = "real"
	if ValidateSamples([]Sample{bad}) == nil {
		t.Fatal("unverified real-data label")
	}
}

func TestR01RiskRequiresCurrentQuote(t *testing.T) {
	s := startMode(t, CBT)
	for _, risk := range []string{"research", "ordinary"} {
		if (Plan{Risk: risk, Intent: "answer"}).Validate("请解释危机识别的研究", s) != nil {
			t.Fatal(risk)
		}
	}
	if (Plan{Risk: "urgent", Intent: "listen", RiskQuote: "我现在有危险"}).Validate("请解释危机识别的研究", s) == nil {
		t.Fatal("risk without user quote")
	}
}
