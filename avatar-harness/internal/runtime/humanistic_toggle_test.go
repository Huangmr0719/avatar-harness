package runtime

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"avatarharness/internal/intervention"
	"avatarharness/internal/provider"
)

func TestHumanisticToggleControlsPromptAndPreventsAutomaticModeChanges(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprint(enabled), func(t *testing.T) {
			var r *Runtime
			var mu sync.Mutex
			phases := map[string]int{}
			r, sid := supportFixture(t, func(ctx context.Context, req provider.Request, emit provider.Emit) (provider.Result, error) {
				phase := provider.String(req.Context["phase"])
				mu.Lock()
				phases[phase]++
				mu.Unlock()
				if phase == "support.risk" {
					return content(`{"level":"ordinary","quote":""}`)
				}
				system := provider.String(req.Input["messages"].([]map[string]any)[0]["content"])
				if strings.Contains(system, humanisticDialoguePrompt) != enabled {
					t.Error("humanistic instructions do not follow the switch")
				}
				foundTime := false
				for _, tool := range req.Input["tools"].([]map[string]any) {
					fn := tool["function"].(map[string]any)
					name := provider.String(fn["name"])
					if name == "set_support_mode" || name == "start_mindfulness" {
						t.Error("automatic mode tool advertised after manual choice")
					}
					foundTime = foundTime || name == "get_current_time"
				}
				if !foundTime {
					t.Error("unrelated tools disabled")
				}
				if _, err := r.scheduleSessionControl(ctx, req, sessionControl{Mode: intervention.CBT}); err == nil {
					t.Error("unadvertised tool overrode the manual choice")
				}
				return content(composeFixture(intervention.Plan{Intent: "listen"}, "我在听。"))
			}, true)
			before := r.Store.Intervention(sid)
			next, err := r.SupportCommand(sid, intervention.Command{Operation: "humanistic", Version: before.Version, Enabled: &enabled})
			if err != nil || next.Version != before.Version+1 || next.Active() != enabled {
				t.Fatal(next, err)
			}
			turn, err := r.Start(sid, "先听我说。", "text", false, nil)
			if err != nil {
				t.Fatal(err)
			}
			awaitTurn(t, turn)
			mu.Lock()
			defer mu.Unlock()
			wantCalls := 1
			if enabled {
				wantCalls = 2
				if phases["support.risk"] != 1 || phases["support.compose"] != 1 {
					t.Fatal("risk or compose missing", phases)
				}
			}
			calls := 0
			for _, n := range phases {
				calls += n
			}
			state := r.Store.Intervention(sid)
			if calls != wantCalls || state.HumanisticOverride == nil || *state.HumanisticOverride != enabled || state.Active() != enabled || finalSupportText(r, sid) != "我在听。" {
				t.Fatal("switch changed or extra model calls introduced", phases, state)
			}
		})
	}
}

func TestHumanisticToggleCancelsLateAnswerAndRejectsStaleCommand(t *testing.T) {
	entered, release, late := make(chan struct{}), make(chan struct{}), make(chan struct{})
	r, sid := supportFixture(t, func(ctx context.Context, req provider.Request, emit provider.Emit) (provider.Result, error) {
		if req.Context["phase"] == "support.risk" {
			close(entered)
			<-release
			defer close(late)
			return content(`{"level":"ordinary","quote":""}`)
		}
		return content(composeFixture(intervention.Plan{Intent: "listen"}, "旧模式回答。"))
	})
	turn, _ := r.Start(sid, "先听我说。", "text", false, nil)
	waitSignal(t, entered, turn, "risk")
	before := r.Store.Intervention(sid)
	enabled := false
	state, err := r.SupportCommand(sid, intervention.Command{Operation: "humanistic", Version: before.Version, Enabled: &enabled})
	close(release)
	awaitTurn(t, turn)
	<-late
	if err != nil || state.Active() || len(r.Store.History(sid, 20)) != 0 || r.Store.Intervention(sid).Version != state.Version {
		t.Fatal("old answer survived switching", state, err)
	}
	enabled = true
	if _, err := r.SupportCommand(sid, intervention.Command{Operation: "humanistic", Version: before.Version, Enabled: &enabled}); err == nil || r.Store.Intervention(sid).Active() {
		t.Fatal("stale switch changed the session")
	}
	for _, e := range r.Store.Events(sid, 0) {
		if e.Type == "assistant.message" || e.Type == "assistant.delta" {
			t.Fatal("cancelled answer leaked", e)
		}
	}
}
