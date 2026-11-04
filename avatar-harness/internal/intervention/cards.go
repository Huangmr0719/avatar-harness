package intervention

import "fmt"

// Cards contain original engineering paraphrases, not copied therapy scripts.
// They are reference material, never instructions supplied by a user document.
type Card struct {
	ID         string   `json:"id"`
	Mode       Mode     `json:"mode"`
	Operation  string   `json:"operation"`
	Requires   []string `json:"requires"`
	Rule       string   `json:"rule"`
	Source     string   `json:"source"`
	Version    string   `json:"version"`
	Provenance string   `json:"provenance"`
}

var Cards = []Card{
	{"attention-observation", Mindfulness, "attention", nil, "经用户选择才播放注意练习。自然观察画面或身边物体，不要求屏息、闭眼或获得特定感受；允许随时停止。只询问实际体验，不推断已放松。", "https://www.nhs.uk/mental-health/self-help/tips-and-support/mindfulness/", "2026-10-31.1", "original_paraphrase"},
	{"reflect-need", Humanistic, "listen", nil, "反映用户已表达的处境和需要；只倾诉时不建议或追问，含义清楚时不必再确认。", "https://www.bacp.co.uk/about-therapy/types-of-therapy/person-centred-counselling", "2026-11-04.1", "original_paraphrase"},
	{"verify-understanding", Humanistic, "clarify", nil, "愿意探索且含义未知时，用一个沿用原话的开放问题核对理解；不塞入原因选项，遵守提问负担。", "https://psychclassics.yorku.ca/Rogers/therapy.htm", "2026-11-04.1", "original_paraphrase"},
	{"explore-meaning", Humanistic, "explore", nil, "按用户方向探索一点价值或并存感受，由用户决定含义与节奏，不强行反驳认知或推进行动。", "https://www.bacp.co.uk/about-therapy/types-of-therapy/person-centred-counselling", "2026-11-04.1", "original_paraphrase"},
	{"respect-direction", Humanistic, "stop", nil, "遵守只听、拒绝、停止或直接作答；采用最新纠正，不换措辞推动；坦诚理解限度，不虚构经历或疗效。", "https://www.bacp.co.uk/about-therapy/types-of-therapy/person-centred-counselling", "2026-11-04.1", "original_paraphrase"},
	{"clarify-situation", CBT, "clarify", nil, "只澄清尚缺的具体情境，每轮至多一个问题。已明确的时间、预算、目标和限制直接复用。", "https://beckinstitute.org/about/understanding-cbt/", "2026-10-31.1", "original_paraphrase"},
	{"fact-interpretation", CBT, "explore", []string{"situation"}, "区分可观察事实与用户赋予的解释。不要替用户宣判想法错误，不给认知歪曲贴标签。", "https://beckinstitute.org/about/understanding-cbt/", "2026-10-31.1", "original_paraphrase"},
	{"check-evidence", CBT, "explore", []string{"thought"}, "合作检查支持和不支持当前想法的具体证据；只问尚未回答的一项，不预设结论或盘问。", "https://beckinstitute.org/blog/selecting-techniques-in-cbt/", "2026-10-31.1", "original_paraphrase"},
	{"other-explanation", CBT, "explore", []string{"evidence"}, "基于已有证据邀请一个其他解释，保留不确定性；用户已有解释时先用其解释，不重复要求。", "https://beckinstitute.org/blog/selecting-techniques-in-cbt/", "2026-10-31.1", "original_paraphrase"},
	{"small-action", CBT, "suggest", nil, "仅在用户要求或愿意尝试时共同拟定一个可选小行动，符合时间与环境约束，说明下次回顾什么。提出不等于认可或完成。", "https://beckinstitute.org/blog/selecting-techniques-in-cbt/", "2026-10-31.1", "original_paraphrase"},
	{"review-action", CBT, "review", nil, "依据用户实际尝试记录回顾困难与帮助程度。未完成先理解障碍，协商缩小行动，不自动认定改善。", "https://beckinstitute.org/about/understanding-cbt/", "2026-10-31.1", "original_paraphrase"},
}

func Select(s State, intent string) Card {
	id := "reflect-need"
	if s.Mode == Mindfulness {
		id = "attention-observation"
	}
	if s.Mode == Humanistic {
		switch intent {
		case "clarify":
			id = "verify-understanding"
		case "explore":
			id = "explore-meaning"
		case "stop":
			id = "respect-direction"
		}
	}
	if s.Mode == CBT {
		switch {
		case intent == "listen":
			id = "reflect-need"
		case intent == "suggest":
			id = "small-action"
		case intent == "review":
			id = "review-action"
		case s.Facts["evidence"].Quote != "" && s.Facts["alternative"].Quote == "":
			id = "other-explanation"
		case s.Facts["thought"].Quote != "" && s.Facts["evidence"].Quote == "":
			id = "check-evidence"
		case s.Facts["situation"].Quote != "" && s.Facts["thought"].Quote == "":
			id = "fact-interpretation"
		default:
			id = "clarify-situation"
		}
	}
	for _, card := range Cards {
		if card.ID == id {
			return card
		}
	}
	panic("missing built-in method card")
}

// Dataset registration is separate from method cards and personal memory.
type Sample struct {
	ID         string `json:"id"`
	CaseID     string `json:"case_id"`
	Kind       string `json:"kind"`
	Split      string `json:"split"`
	Source     string `json:"source"`
	Permission string `json:"permission"`
}

func ValidateSamples(samples []Sample) error {
	groups := map[string]string{}
	ids := map[string]bool{}
	for _, s := range samples {
		if s.ID == "" || ids[s.ID] || s.CaseID == "" || s.Source == "" || s.Permission == "" || (s.Split != "development" && s.Split != "heldout") || (s.Kind != "synthetic" && s.Kind != "roleplay" && s.Kind != "real_consented") {
			return fmt.Errorf("语料 %q 缺少来源、用途、类型或有效切分", s.ID)
		}
		if split, ok := groups[s.CaseID]; ok && split != s.Split {
			return fmt.Errorf("个案 %q 跨开发与留出集合", s.CaseID)
		}
		groups[s.CaseID], ids[s.ID] = s.Split, true
	}
	return nil
}
