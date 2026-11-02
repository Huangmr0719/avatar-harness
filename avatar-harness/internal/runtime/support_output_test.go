package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"avatarharness/internal/intervention"
	"avatarharness/internal/provider"
)

func TestSupportExplicitSentenceLimitRejectsApprovalBeforeRelease(t *testing.T) {
	bad, good := "你希望先被征询；再由自己决定。", "你希望先被征询。然后由自己决定。"
	var repairs atomic.Int32
	r, sid := supportFixture(t, func(ctx context.Context, req provider.Request, emit provider.Emit) (provider.Result, error) {
		switch req.Context["phase"] {
		case "support.risk":
			return content(`{"level":"ordinary","quote":""}`)
		case "support.compose":
			return content(composeFixture(intervention.Plan{}, bad))
		case "support.guard":
			return content(guardFixture("approve", bad))
		case "support.repair":
			repairs.Add(1)
			var input struct {
				HardLimits struct {
					Sentences int `json:"sentences"`
				} `json:"hard_limits"`
			}
			messages := req.Input["messages"].([]map[string]any)
			_ = json.Unmarshal([]byte(provider.String(messages[1]["content"])), &input)
			if input.HardLimits.Sentences != 2 {
				t.Error("explicit sentence constraint lost")
			}
			return content(guardFixture("revise", good))
		}
		return provider.Result{}, fmt.Errorf("unexpected phase")
	})
	turn, _ := r.Start(sid, "用两句话概括我的需要。", "text", false, nil)
	awaitTurn(t, turn)
	if repairs.Load() != 1 || finalSupportText(r, sid) != good {
		t.Fatal("invalid approval escaped", finalSupportText(r, sid), repairs.Load())
	}
	for _, e := range r.Store.Events(sid, 0) {
		if e.Type == "assistant.delta" && strings.Contains(provider.String(e.Data["text"]), "；") {
			t.Fatal("unvalidated prefix leaked")
		}
	}
}

func TestSupportRevisionRecordsOnlyVerifiedFinalAction(t *testing.T) {
	for _, tc := range []struct {
		name, quote, text string
		minutes           int
		want              bool
	}{
		{"valid", "我愿意试", "写下一个事实", 1, true},
		{"invented_consent", "我已经完成", "写下一个事实", 1, false},
		{"absent_from_speech", "我愿意试", "给财务打电话", 1, false},
		{"exceeds_current_limit", "我愿意试", "写下一个事实", 2, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			answer := "用一分钟写下一个事实。下次回顾是否有新信息。"
			var requests atomic.Int32
			r, sid := supportFixture(t, func(ctx context.Context, req provider.Request, emit provider.Emit) (provider.Result, error) {
				requests.Add(1)
				switch req.Context["phase"] {
				case "support.risk":
					return content(`{"level":"ordinary","quote":""}`)
				case "support.compose":
					return content(composeFixture(intervention.Plan{}, "拿出一张纸分成两列。"))
				case "support.guard":
					prefix := guardPlanFixture(intervention.Plan{Intent: "suggest", Action: &intervention.Proposal{Text: "拿出一张纸", Minutes: 1, ConsentQuote: "我愿意试"}}, "revise", answer)
					tail, _ := json.Marshal(intervention.Proposal{Text: tc.text, Minutes: tc.minutes, ConsentQuote: tc.quote})
					return content(strings.TrimSuffix(prefix, "}") + `,"final_action":` + string(tail) + `}`)
				}
				return provider.Result{}, fmt.Errorf("extra serial call")
			})
			turn, _ := r.Start(sid, "我愿意试一个不超过一分钟的小行动。", "text", false, nil)
			awaitTurn(t, turn)
			s := r.Store.Intervention(sid)
			if finalSupportText(r, sid) != answer || requests.Load() != 3 || (s.Action != nil) != tc.want {
				t.Fatal("final action validation", s.Action, requests.Load(), finalSupportText(r, sid))
			}
			if tc.want && (s.Action.Text != tc.text || s.Action.Status != "proposed" || s.DialogueTurns != 1) {
				t.Fatal("action marked completed or advanced twice", s)
			}
		})
	}
}

func TestSupportComposeIncompleteJSONIsReportedAsFailure(t *testing.T) {
	for _, tc := range []struct{ name, raw string }{
		{"incomplete_json", `{"emotion":"neutral","intensity":0.1,"speech":"<speak/>未经提交正文。"`},
		{"missing_commit", `{"emotion":"neutral","intensity":0.1,"speech":"未经提交正文。"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, sid := supportFixture(t, func(ctx context.Context, req provider.Request, emit provider.Emit) (provider.Result, error) {
				switch req.Context["phase"] {
				case "support.risk":
					return content(`{"level":"ordinary","quote":""}`)
				case "support.compose":
					return content(tc.raw)
				}
				return provider.Result{}, fmt.Errorf("invalid compose reached reviewer")
			})
			turn, _ := r.Start(sid, "先听我说。", "text", false, nil)
			awaitTurn(t, turn)
			found := false
			for _, e := range r.Store.Events(sid, 0) {
				if e.Type == "intervention.check" && e.Data["phase"] == "support.compose" {
					found = true
					if e.Data["ok"] != false {
						t.Fatal("transport success hid final parse failure", e)
					}
				}
				if e.Type == "assistant.delta" && strings.Contains(provider.String(e.Data["text"]), "未经提交正文") {
					t.Fatal("uncommitted draft leaked")
				}
			}
			if !found {
				t.Fatal("compose check missing")
			}
		})
	}
}
