package runtime

import (
	"context"
	"fmt"
	"os"
	"time"

	"avatarharness/internal/intervention"
	"avatarharness/internal/provider"
	"avatarharness/internal/store"
)

const sessionToolPrompt = `
交流支持和正念视频都是你可主动使用的工具。根据用户本次表达选择，不让用户自行配置心理方法。需要倾听共情时用 set_support_mode(humanistic)，需要梳理具体想法时用 cbt，回到普通交谈或拒绝支持时用 off；普通问答不启动支持。仅凭感知标签不启动，也不诊断。适合呼吸/正念练习时用 start_mindfulness；用户拒绝练习时不要播放。可以在同一工具批次完成所需调用，不增加解释性往返。工具安排成功后用一句自然的话引导，说明可以随时停止；不要让用户找界面按钮。scheduled 仅表示已安排，只有实际播放记录才表示播放，不能声称练习已完成或情绪已改善。保留 <speak/> 提交规则，工具决策不朗读。`

type sessionControl struct {
	Mode            intervention.Mode `json:"mode"`
	PlayVideo       bool              `json:"play_video"`
	DurationSeconds int               `json:"duration_seconds,omitempty"`
}

func (r *Runtime) sessionToolInstructions(t *Turn) string {
	state := r.Store.Intervention(t.SessionID)
	if state.HumanisticOverride != nil {
		return "\n用户已在界面手动选择交流方式。保持当前方式，不自动调用交流方式或正念视频工具；事实问题直接回答。保留 <speak/> 提交规则，其他工具照常使用。"
	}
	return sessionToolPrompt
}

func (r *Runtime) sessionToolSpecs(t *Turn) []map[string]any {
	tools := r.Bus.Spec()
	if r.Store.Intervention(t.SessionID).HumanisticOverride == nil {
		return tools
	}
	filtered := []map[string]any{}
	for _, tool := range tools {
		function, _ := tool["function"].(map[string]any)
		name := provider.String(function["name"])
		if name != "set_support_mode" && name != "start_mindfulness" {
			filtered = append(filtered, tool)
		}
	}
	return filtered
}

func (r *Runtime) registerSessionTools() {
	r.Bus.Register("set_support_mode", func(ctx context.Context, req provider.Request, _ provider.Emit) (provider.Result, error) {
		mode := provider.String(req.Input["mode"])
		if mode != "off" && mode != "humanistic" && mode != "cbt" {
			return provider.Result{}, fmt.Errorf("无效支持方式")
		}
		return r.scheduleSessionControl(ctx, req, sessionControl{Mode: intervention.Mode(mode)})
	})
	r.Bus.Register("start_mindfulness", func(ctx context.Context, req provider.Request, _ provider.Emit) (provider.Result, error) {
		return r.scheduleSessionControl(ctx, req, sessionControl{Mode: intervention.Mindfulness, PlayVideo: true})
	})
}

func (r *Runtime) scheduleSessionControl(ctx context.Context, req provider.Request, control sessionControl) (provider.Result, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t := r.current[provider.String(req.Context["session_id"])]
	if err := ctx.Err(); err != nil {
		return provider.Result{}, err
	}
	if t == nil || t.ID != provider.String(req.Context["turn_id"]) || t.Cancelled || t.Context.Err() != nil {
		return provider.Result{}, context.Canceled
	}
	if r.Store.Intervention(t.SessionID).HumanisticOverride != nil {
		return provider.Result{}, fmt.Errorf("交流方式已由界面开关固定，请保持当前方式")
	}
	output := map[string]any{"scheduled": true, "mode": control.Mode, "applies_after_answer": true}
	if control.PlayVideo {
		if v := r.Config.PracticeVideo; v != nil {
			if info, err := os.Stat(v.Path); err != nil || info.IsDir() || info.Size() == 0 {
				return provider.Result{}, fmt.Errorf("正念视频暂不可用")
			}
			output["title"], output["duration_seconds"] = v.Title, v.DurationSeconds
		} else {
			output["title"], output["duration_seconds"] = "注意力观察练习", 120
		}
		output["playback_started"] = false
		output["can_stop"] = true
		control.DurationSeconds = output["duration_seconds"].(int)
	}
	t.pendingControl = &control
	return provider.OK(output), nil
}

// Apply only after the answer has been admitted and speech generation succeeds.
// Scheduling a tool never cancels its own turn or changes support state versions.
func (r *Runtime) commitSessionControl(t *Turn) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if t.pendingControl == nil {
		return nil
	}
	if t.Cancelled || t.Context.Err() != nil || r.current[t.SessionID] != t {
		return context.Canceled
	}
	control := *t.pendingControl
	s := r.Store.Intervention(t.SessionID)
	command := intervention.Command{Version: s.Version, Operation: "start", Mode: control.Mode, Source: "llm_tool"}
	if control.Mode == "off" {
		command.Operation = "stop"
	}
	if control.PlayVideo {
		p := intervention.Defaults(intervention.Mindfulness)
		p.DurationSeconds = 120
		if v := r.Config.PracticeVideo; v != nil {
			p.DurationSeconds = v.DurationSeconds
		}
		command.Parameters = &p
	}
	next, err := intervention.Apply(s, command, time.Now(), store.ID())
	if err != nil {
		return err
	}
	next.LastDecision.Reason = "根据当前对话由助手工具选择"
	if next.Execution != nil {
		next.Execution.TurnID = t.ID
	}
	if !r.Store.CommitIntervention(t.SessionID, s.Version, next, nil) {
		return fmt.Errorf("支持状态已更新")
	}
	t.pendingControl = nil
	r.Store.Emit(t.SessionID, t.ID, "intervention.state", map[string]any{"state": next})
	if control.PlayVideo {
		r.Store.Emit(t.SessionID, t.ID, "intervention.request", map[string]any{"action": "play_after_speech", "execution_id": next.Execution.ID})
	}
	return nil
}

func (r *Runtime) scheduledControl(t *Turn) *sessionControl {
	r.mu.Lock()
	defer r.mu.Unlock()
	if t.pendingControl == nil {
		return nil
	}
	copy := *t.pendingControl
	return &copy
}
