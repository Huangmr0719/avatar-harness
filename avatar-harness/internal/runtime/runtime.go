package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"avatarharness/internal/config"
	"avatarharness/internal/provider"
	"avatarharness/internal/store"
)

type Turn struct {
	ID            string
	SessionID     string
	Source        string
	InputID       string
	Origin        time.Time
	Started       time.Time
	Context       context.Context
	Cancel        context.CancelFunc
	Done          chan struct{}
	Cancelled     bool
	Finished      bool
	Playback      bool
	AudioPlayback bool
	Metadata      map[string]any
}
type Runtime struct {
	Config  *config.Config
	Store   *store.Store
	Bus     *provider.Bus
	mu      sync.Mutex
	current map[string]*Turn
	turns   map[string]*Turn
	closed  bool
}

func New(c *config.Config, s *store.Store, b *provider.Bus) *Runtime {
	return &Runtime{Config: c, Store: s, Bus: b, current: map[string]*Turn{}, turns: map[string]*Turn{}}
}
func (r *Runtime) Character(id string) (config.Character, bool) {
	for _, c := range r.Config.Characters {
		if c.ID == id {
			return c, true
		}
	}
	return config.Character{}, false
}
func (r *Runtime) Start(sid, text, source string, media bool, inherited []map[string]any) (*Turn, error) {
	return r.start(sid, text, source, media, inherited, "", time.Time{})
}
func (r *Runtime) StartVoice(sid, text, inputID string, endedAt time.Time) (*Turn, error) {
	return r.start(sid, text, "voice", true, nil, inputID, endedAt)
}
func (r *Runtime) start(sid, text, source string, media bool, inherited []map[string]any, inputID string, origin time.Time) (*Turn, error) {
	if _, ok := r.Store.Session(sid); !ok {
		return nil, fmt.Errorf("会话不存在")
	}
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("输入不能为空")
	}
	if source == "" {
		source = "text"
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil, fmt.Errorf("运行时已停止")
	}
	r.cancelLocked(sid, "被新输入替换")
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(r.Config.Limits.MaxTurnMS)*time.Millisecond)
	started := time.Now()
	if origin.IsZero() {
		origin = started
	}
	t := &Turn{ID: store.ID(), SessionID: sid, Source: source, InputID: inputID, Origin: origin, Started: started, Context: ctx, Cancel: cancel, Done: make(chan struct{})}
	t.Metadata = map[string]any{"session_id": sid, "turn_id": t.ID, "request_id": t.ID, "input_id": inputID}
	r.current[sid] = t
	r.turns[t.ID] = t
	if len(r.turns) > 1024 {
		for key, old := range r.turns {
			if old.Finished && r.current[old.SessionID] != old {
				delete(r.turns, key)
				if len(r.turns) <= 768 {
					break
				}
			}
		}
	}
	r.mu.Unlock()
	r.Emit(t, "turn.started", map[string]any{"source": source, "input_id": inputID, "budget_ms": r.Config.Limits.FirstResponseMS})
	r.Emit(t, "user.message", map[string]any{"text": text})
	go r.run(t, text, media, inherited)
	return t, nil
}
func (r *Runtime) cancelLocked(sid, reason string) bool {
	t := r.current[sid]
	if t == nil || t.Cancelled {
		return false
	}
	t.Cancelled = true
	t.Cancel()
	delete(r.current, sid)
	r.Store.Emit(sid, t.ID, "turn.cancelled", map[string]any{"reason": reason})
	return true
}
func (r *Runtime) Cancel(sid, reason string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cancelLocked(sid, reason)
}
func (r *Runtime) Emit(t *Turn, kind string, data map[string]any) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if t.Cancelled || r.current[t.SessionID] != t {
		return false
	}
	r.Store.Emit(t.SessionID, t.ID, kind, data)
	return true
}
func (r *Runtime) module(t *Turn, name, state string, elapsed float64, message string) {
	r.Emit(t, "module.status", map[string]any{"module": name, "state": state, "elapsed_ms": elapsed, "message": message})
}
func (r *Runtime) invoke(t *Turn, name string, input map[string]any) provider.Result {
	start := time.Now()
	r.module(t, name, "running", 0, "")
	result := r.Bus.Invoke(t.Context, name, input, t.Metadata)
	state := "configured"
	if result.Status != "ok" {
		state = result.Status
	}
	r.module(t, name, state, float64(time.Since(start).Microseconds())/1000, result.Error)
	return result
}
func (r *Runtime) run(t *Turn, text string, media bool, inherited []map[string]any) {
	var speech *Speech
	var runErr error
	defer func() {
		if speech != nil {
			speech.Stop()
		}
		if runErr != nil && !errors.Is(runErr, context.Canceled) {
			r.Emit(t, "turn.error", map[string]any{"message": runErr.Error()})
		}
		r.mu.Lock()
		t.Finished = true
		r.mu.Unlock()
		r.Emit(t, "turn.finished", map[string]any{"duration_ms": float64(time.Since(t.Started).Microseconds()) / 1000, "note": "生成耗时不是音画反馈时间"})
		t.Cancel()
		close(t.Done)
	}()
	if strings.HasPrefix(text, "/") {
		runErr = r.command(t, text)
		return
	}
	session, _ := r.Store.Session(t.SessionID)
	character, _ := r.Character(session.CharacterID)
	messages := []map[string]any{{"role": "system", "content": character.Persona + "\n使用简洁自然的中文回答，不编造工具结果。"}}
	if media {
		messages[0]["content"] = provider.String(messages[0]["content"]) + "\n当前是面对面实时语音交谈。每次像正常人接话，默认只说 1–2 句、约 20–60 个汉字，可播报正文最多 80 字。先直接回应当前问题，只讲一个最重要的点；复杂话题分轮展开，等用户继续问。不要演讲、长篇科普、条目罗列、连续举例或复述问题，不要加总结式结尾或空泛的等待用语。缺少关键信息时只问一个简短问题。只有用户明确要求详细说明时才展开，但每次仍最多 3 句、120 字。长度限制只计算可播报正文，不包含结构化字段或提交标记。若需要工具，直接调用工具，不输出工具前言。若确定可以回答，正文必须以 " + answerMarker + " 开头，此标记表示你已提交最终回答、此轮不再调用工具。标记后直接输出可播报正文，不使用其他 XML 标签。"
	}
	structured := character.Renderer.Kind == "lam"
	if structured {
		messages[0]["content"] = provider.String(messages[0]["content"]) + expressionPrompt
	}
	if inherited != nil {
		messages = append(messages, inherited...)
	} else {
		messages = append(messages, r.Store.History(t.SessionID, r.Config.Limits.MaxHistoryMessages)...)
		messages = append(messages, map[string]any{"role": "user", "content": text})
	}
	commit := []map[string]any{{"role": "user", "content": text}}
	if media {
		speech = NewSpeech(t.Context, r.Config, character, r.Bus, t.Metadata, func(kind string, data map[string]any) { r.Emit(t, kind, data) })
		go r.deadline(t)
	}
	var firstLLMStarted time.Time
	firstSpeechTimed := false
	timeFirstSpeech := func() {
		if !firstSpeechTimed {
			firstSpeechTimed = true
			r.Emit(t, "pipeline.timing", map[string]any{"stage": "llm.first_speech", "elapsed_ms": float64(time.Since(firstLLMStarted).Microseconds()) / 1000})
		}
	}
	for batch := 0; batch <= r.Config.Limits.MaxToolBatches; batch++ {
		tools := []map[string]any{}
		if batch < r.Config.Limits.MaxToolBatches {
			tools = r.Bus.Spec()
		}
		draft := len(tools) > 0
		textOut := ""
		calls := []any{}
		var gate AnswerGate
		response := StructuredAnswer{Enabled: structured}
		var pendingExpression *Expression
		toolSeen := false
		acceptText := func(value string) error {
			if value == "" {
				return nil
			}
			textOut += value
			r.Emit(t, "assistant.delta", map[string]any{"text": value, "draft": draft && !gate.Committed})
			if !draft || gate.Committed {
				timeFirstSpeech()
			}
			if (!draft || gate.Committed) && speech != nil {
				return speech.Feed(value, false)
			}
			return nil
		}
		processText := func(chunk string, final bool) error {
			text, expression, err := response.Feed(chunk, final)
			if err != nil {
				return err
			}
			if expression != nil {
				pendingExpression = expression
			}
			value := gate.Feed(text, final)
			if gate.Committed && toolSeen {
				return fmt.Errorf("模型在工具决策中提交了冲突的语音回答")
			}
			if gate.Committed && pendingExpression != nil {
				if speech != nil {
					speech.SetExpression(*pendingExpression)
				}
				r.Emit(t, "pipeline.expression", map[string]any{"style": pendingExpression.Style, "intensity": pendingExpression.Intensity, "duration_ms": pendingExpression.DurationMS, "source": "llm_structured"})
				pendingExpression = nil
			}
			return acceptText(value)
		}
		start := time.Now()
		if firstLLMStarted.IsZero() {
			firstLLMStarted = start
		}
		r.module(t, "llm", "running", 0, "")
		input := map[string]any{"messages": messages, "tools": tools}
		if structured {
			input["response_format"] = map[string]any{"type": "json_object"}
		}
		runErr = r.Bus.LLM(t.Context, input, t.Metadata, func(e map[string]any) error {
			if err := t.Context.Err(); err != nil {
				return err
			}
			switch e["type"] {
			case "text":
				return processText(provider.String(e["text"]), false)
			case "tool.delta":
				toolSeen = true
				if gate.Committed {
					return fmt.Errorf("模型在提交语音回答后又请求工具，已停止本轮")
				}
			case "done":
				var err error
				calls, err = provider.Calls(e["tool_calls"])
				if err != nil {
					return err
				}
			}
			return nil
		})
		if runErr != nil {
			r.module(t, "llm", "error", float64(time.Since(start).Microseconds())/1000, runErr.Error())
			return
		}
		if len(calls) == 0 {
			runErr = processText("", true)
		}
		if runErr != nil {
			return
		}
		if gate.Committed && len(calls) > 0 {
			runErr = fmt.Errorf("模型在提交语音回答后又请求工具，未执行额外调用")
			return
		}
		r.module(t, "llm", "configured", float64(time.Since(start).Microseconds())/1000, "")
		if len(calls) == 0 {
			if strings.TrimSpace(textOut) == "" {
				runErr = fmt.Errorf("模型返回空回答")
				return
			}
			if draft && !gate.Committed {
				// An unmarked answer becomes speakable only after the provider
				// confirms there are no tool calls. Time this fallback too.
				timeFirstSpeech()
				if speech != nil {
					if runErr = speech.Feed(textOut, false); runErr != nil {
						return
					}
				}
			}
			answer := map[string]any{"role": "assistant", "content": response.HistoryContent(textOut)}
			commit = append(commit, answer)
			r.mu.Lock()
			if !t.Cancelled && r.current[t.SessionID] == t {
				r.Store.AddHistory(t.SessionID, commit)
			}
			r.mu.Unlock()
			r.Emit(t, "assistant.message", map[string]any{"text": textOut, "source": "llm"})
			if speech != nil {
				runErr = speech.Finish()
			}
			return
		}
		if batch >= r.Config.Limits.MaxToolBatches {
			runErr = fmt.Errorf("模型超过工具回合预算，额外调用未执行")
			return
		}
		for _, v := range calls {
			call, ok := v.(map[string]any)
			if !ok {
				runErr = fmt.Errorf("模型工具调用格式错误")
				return
			}
			if provider.String(call["id"]) == "" {
				call["id"] = store.ID()
			}
		}
		assistant := map[string]any{"role": "assistant", "content": textOut, "tool_calls": calls}
		messages = append(messages, assistant)
		commit = append(commit, assistant)
		r.Emit(t, "assistant.draft_reset", map[string]any{})
		deadline := time.Now().Add(time.Duration(r.Config.Limits.ToolBatchMS) * time.Millisecond)
		for _, v := range calls {
			call := v.(map[string]any)
			fn, _ := call["function"].(map[string]any)
			name := provider.String(fn["name"])
			callID := provider.String(call["id"])
			args, err := Arguments(fn["arguments"])
			var result provider.Result
			if err != nil {
				result = provider.Fail("error", fmt.Errorf("参数解析失败，未执行：%w", err))
				r.Emit(t, "tool.finished", merge(map[string]any{"call_id": callID, "name": name}, result))
			} else if !r.registeredTool(name) {
				result = provider.Fail("error", fmt.Errorf("工具未注册：%s；模型模块不能绕过工具注册调用", name))
				r.Emit(t, "tool.finished", merge(map[string]any{"call_id": callID, "name": name}, result))
			} else {
				ctx, cancel := context.WithDeadline(t.Context, deadline)
				metadata := copyMap(t.Metadata)
				metadata["remaining_budget_ms"] = max(0, time.Until(deadline).Milliseconds())
				r.Emit(t, "tool.started", map[string]any{"call_id": callID, "name": name, "arguments": args})
				result = r.Bus.Invoke(ctx, name, args, metadata)
				cancel()
				r.Emit(t, "tool.finished", merge(map[string]any{"call_id": callID, "name": name}, result))
			}
			if err := t.Context.Err(); err != nil {
				runErr = err
				return
			}
			raw, _ := json.Marshal(result)
			message := map[string]any{"role": "tool", "tool_call_id": callID, "content": string(raw)}
			messages = append(messages, message)
			commit = append(commit, message)
		}
	}
}

func (r *Runtime) registeredTool(name string) bool {
	for _, tool := range r.Config.Tools {
		if tool.Name == name {
			return true
		}
	}
	return false
}
func merge(data map[string]any, result provider.Result) map[string]any {
	data["status"] = result.Status
	data["output"] = result.Output
	data["error"] = result.Error
	data["elapsed_ms"] = result.ElapsedMS
	return data
}
func copyMap(input map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range input {
		out[k] = v
	}
	return out
}
func Arguments(v any) (map[string]any, error) {
	if v == nil {
		return map[string]any{}, nil
	}
	if obj, ok := v.(map[string]any); ok {
		return obj, nil
	}
	raw, ok := v.(string)
	if !ok {
		return nil, fmt.Errorf("参数需要 JSON 对象或字符串")
	}
	if raw == "" {
		raw = "{}"
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return nil, err
	}
	if result == nil {
		return nil, fmt.Errorf("参数必须是 JSON 对象")
	}
	return result, nil
}
func (r *Runtime) command(t *Turn, text string) error {
	command, rest, _ := strings.Cut(text, " ")
	answer := ""
	switch command {
	case "/help":
		answer = "/help 命令列表\n/status 模块配置\n/tools 工具列表\n/tool get_current_time {\"timezone\":\"Asia/Shanghai\"} 调用真实工具\n/clear 清空日志\nProvider 通过终端 ./start configure all 配置。"
	case "/status":
		states := map[string]any{}
		for role, p := range r.Config.Providers {
			states[role] = config.State(p)
		}
		raw, _ := json.MarshalIndent(states, "", "  ")
		answer = string(raw)
	case "/tools":
		for _, tool := range r.Config.Tools {
			answer += tool.Name + " · " + tool.Kind + " · " + tool.Description + "\n"
		}
	case "/clear":
		r.Store.Clear(t.SessionID)
		r.Emit(t, "session.cleared", map[string]any{})
		answer = "日志已清空。"
	case "/tool":
		name, raw, _ := strings.Cut(strings.TrimSpace(rest), " ")
		args, err := Arguments(raw)
		if err != nil {
			return err
		}
		id := store.ID()
		r.Emit(t, "tool.started", map[string]any{"call_id": id, "name": name, "arguments": args})
		result := r.Bus.Invoke(t.Context, name, args, t.Metadata)
		r.Emit(t, "tool.finished", merge(map[string]any{"call_id": id, "name": name}, result))
		data, _ := json.MarshalIndent(result, "", "  ")
		answer = string(data)
	default:
		answer = "未知命令。输入 /help 查看可用命令。"
	}
	r.Emit(t, "assistant.message", map[string]any{"text": answer, "source": "local_command"})
	return nil
}
func (r *Runtime) deadline(t *Turn) {
	timer := time.NewTimer(max(0, time.Until(t.Origin.Add(time.Duration(r.Config.Limits.FirstResponseMS)*time.Millisecond))))
	defer timer.Stop()
	<-timer.C
	r.mu.Lock()
	qualifies := t.AudioPlayback || t.Playback || t.Cancelled
	r.mu.Unlock()
	if !qualifies {
		r.Emit(t, "deadline.missed", map[string]any{"message": "尚未收到有效语音起播记录", "metric": "meaningful_audio_response"})
	}
}
func (r *Runtime) Report(sid, tid string, report map[string]any) (map[string]any, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t := r.turns[tid]
	if t == nil || t.SessionID != sid || t.Cancelled {
		return nil, fmt.Errorf("轮次不存在或已取消")
	}
	elapsed := provider.Number(report["elapsed_ms"])
	skew, hasSkew := report["av_skew_ms"]
	qualifies := report["modality"] == "audio_video" && report["meaningful"] == true && hasSkew && skew != nil && provider.Number(skew) >= -100 && provider.Number(skew) <= 100
	if qualifies {
		t.Playback = true
	}
	audioObserved := (report["modality"] == "audio" || report["modality"] == "audio_video") && report["meaningful"] == true && report["playback_observed"] == true
	if audioObserved {
		t.AudioPlayback = true
	}
	result := copyMap(report)
	result["qualifies"] = qualifies
	result["within_budget"] = qualifies && t.Source == "voice" && elapsed <= float64(r.Config.Limits.FirstResponseMS)
	result["diagnostic_within_budget"] = qualifies && elapsed <= float64(r.Config.Limits.FirstResponseMS)
	result["audio_within_budget"] = audioObserved && t.Source == "voice" && elapsed <= float64(r.Config.Limits.FirstResponseMS)
	result["origin"] = t.Source
	r.Store.Emit(sid, tid, "playback.report", result)
	return result, nil
}
func (r *Runtime) Shutdown() {
	r.mu.Lock()
	r.closed = true
	turns := []*Turn{}
	for sid, t := range r.current {
		turns = append(turns, t)
		r.cancelLocked(sid, "服务停止")
	}
	r.mu.Unlock()
	for _, t := range turns {
		select {
		case <-t.Done:
		case <-time.After(time.Second):
		}
	}
}
