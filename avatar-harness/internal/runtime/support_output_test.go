package runtime

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"avatarharness/internal/intervention"
	"avatarharness/internal/provider"
)

func TestSupportExactSentenceCountUsesLocalValidationWithoutRewrite(t *testing.T) {
	for _, tc := range []struct {
		name, answer string
		valid        bool
	}{
		{"invalid", "你希望先被征询；再由自己决定。", false},
		{"valid", "你希望先被征询。然后由自己决定。", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests atomic.Int32
			r, sid := supportFixture(t, func(ctx context.Context, req provider.Request, emit provider.Emit) (provider.Result, error) {
				requests.Add(1)
				if req.Context["phase"] == "support.risk" {
					return content(`{"level":"ordinary","quote":""}`)
				}
				if req.Context["phase"] != "support.compose" {
					return provider.Result{}, fmt.Errorf("unexpected serial request")
				}
				return content(composeFixture(intervention.Plan{Intent: "answer"}, tc.answer))
			})
			turn, _ := r.Start(sid, "用两句话概括我的需要。", "text", false, nil)
			awaitTurn(t, turn)
			if requests.Load() != 2 {
				t.Fatal("extra request", requests.Load())
			}
			if tc.valid && finalSupportText(r, sid) != tc.answer {
				t.Fatal(r.Store.Events(sid, 0))
			}
			if !tc.valid {
				if !strings.Contains(finalSupportText(r, sid), "暂未完成") {
					t.Fatal(r.Store.Events(sid, 0))
				}
				for _, e := range r.Store.Events(sid, 0) {
					if e.Type == "assistant.delta" {
						t.Fatal("invalid whole-answer format leaked", e)
					}
				}
			}
		})
	}
}

func TestSupportSingleGenerationRecordsOnlySourceGroundedAction(t *testing.T) {
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
			var calls atomic.Int32
			r, sid := supportFixture(t, func(ctx context.Context, req provider.Request, emit provider.Emit) (provider.Result, error) {
				calls.Add(1)
				if req.Context["phase"] == "support.risk" {
					return content(`{"level":"ordinary","quote":""}`)
				}
				if req.Context["phase"] != "support.compose" {
					return provider.Result{}, fmt.Errorf("extra serial call")
				}
				return content(composeFixture(intervention.Plan{Intent: "suggest", Action: &intervention.Proposal{Text: tc.text, Minutes: tc.minutes, ConsentQuote: tc.quote}}, answer))
			})
			turn, _ := r.Start(sid, "我愿意试一个不超过一分钟的小行动。", "text", false, nil)
			awaitTurn(t, turn)
			state := r.Store.Intervention(sid)
			if finalSupportText(r, sid) != answer || calls.Load() != 2 || (state.Action != nil) != tc.want {
				t.Fatal(state.Action, calls.Load(), r.Store.Events(sid, 0))
			}
			if tc.want && (state.Action.Status != "proposed" || state.Action.Text != tc.text || state.DialogueTurns != 1) {
				t.Fatal("action completed or state advanced twice", state)
			}
		})
	}
}

func TestSupportComposeIncompleteJSONIsReportedAsFailure(t *testing.T) {
	for _, raw := range []string{
		`{"emotion":"neutral","intensity":0.1,"speech":"<speak/>未经提交正文。"`,
		`{"emotion":"neutral","intensity":0.1,"speech":"未经提交正文。"}`,
	} {
		r, sid := supportFixture(t, func(ctx context.Context, req provider.Request, emit provider.Emit) (provider.Result, error) {
			if req.Context["phase"] == "support.risk" {
				return content(`{"level":"ordinary","quote":""}`)
			}
			return content(raw)
		})
		turn, _ := r.Start(sid, "先听我说。", "text", false, nil)
		awaitTurn(t, turn)
		found := false
		for _, e := range r.Store.Events(sid, 0) {
			if e.Type == "intervention.check" && e.Data["phase"] == "support.compose" {
				found = true
				if e.Data["ok"] != false {
					t.Fatal("parse failure hidden", e)
				}
			}
			if e.Type == "assistant.delta" {
				t.Fatal("invalid non-streaming reply leaked", e)
			}
		}
		if !found {
			t.Fatal("compose diagnostic missing")
		}
	}
}

func TestSupportInvalidOptionalMetadataDoesNotBlockValidSpeech(t *testing.T) {
	for _, tail := range []string{`,"support":"bad"`, `,"support":{"intent":"invented","facts":[]}`} {
		answer := "好，先听你说。"
		raw := strings.TrimSuffix(composeFixture(intervention.Plan{}, answer), "}") + tail + "}"
		var requests atomic.Int32
		r, sid := supportFixture(t, func(ctx context.Context, req provider.Request, emit provider.Emit) (provider.Result, error) {
			requests.Add(1)
			if req.Context["phase"] == "support.risk" {
				return content(`{"level":"ordinary","quote":""}`)
			}
			return content(raw)
		})
		turn, _ := r.Start(sid, "先听我说。", "text", false, nil)
		awaitTurn(t, turn)
		if finalSupportText(r, sid) != answer || requests.Load() != 2 || len(r.Store.Intervention(sid).Facts) != 0 {
			t.Fatal("metadata turned into a gate", r.Store.Events(sid, 0))
		}
	}
}
