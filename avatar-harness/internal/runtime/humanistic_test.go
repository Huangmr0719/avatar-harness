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

// This is a protocol/source test, not a semantic empathy evaluation.
func TestHumanisticPrinciplesAndStateUseOneGenerationWithoutReview(t *testing.T) {
	answer := "你说现在只是累，不希望被解释成害怕。"
	var requests atomic.Int32
	r, sid := supportFixture(t, func(ctx context.Context, req provider.Request, emit provider.Emit) (provider.Result, error) {
		requests.Add(1)
		system := provider.String(req.Input["messages"].([]map[string]any)[0]["content"])
		switch req.Context["phase"] {
		case "support.risk":
			if strings.Contains(system, humanisticDialoguePrompt) {
				t.Error("behavior rules contaminated risk check")
			}
			return content(`{"level":"ordinary","quote":""}`)
		case "support.compose":
			if !strings.Contains(system, humanisticDialoguePrompt) || !strings.Contains(system, "verify-understanding") || strings.Contains(system, `"id":"check-evidence"`) {
				t.Error("wrong method instructions")
			}
			return content(composeFixture(intervention.Plan{Intent: "listen", Facts: []intervention.Fact{{Key: "feeling", Quote: "不是害怕，是累"}, {Key: "need", Quote: "先听我说"}, {Key: "preference", Quote: "编造的偏好"}}}, answer))
		default:
			return provider.Result{}, fmt.Errorf("review or rewrite invoked: %v", req.Context["phase"])
		}
	})
	state := r.Store.Intervention(sid)
	if _, err := r.SupportCommand(sid, intervention.Command{Operation: "start", Version: state.Version, Mode: intervention.Humanistic}); err != nil {
		t.Fatal(err)
	}
	turn, _ := r.Start(sid, "不是害怕，是累。先听我说。", "text", false, nil)
	awaitTurn(t, turn)
	got := r.Store.Intervention(sid)
	if requests.Load() != 2 || finalSupportText(r, sid) != answer || got.Facts["feeling"].Quote != "不是害怕，是累" || len(got.Facts) != 2 {
		t.Fatal(requests.Load(), got)
	}
}
