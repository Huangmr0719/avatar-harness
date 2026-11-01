package runtime

import (
	"avatarharness/internal/promptcompare"
)

// Historical behavior and voice instructions from commit
// 42a366e64800a11272e805e76cdee3b4ce29d66b, runtime.go:163–165.
// Current format, perception, memory interpretation and safety protocols are
// deliberately common to both variants; this is not a software rollback.
const legacyConversationPrompt = "\n使用简洁自然的中文回答，不编造工具结果。"
const legacyVoicePrompt = `
当前是面对面实时语音交谈。每次像正常人接话，默认只说 1–2 句、约 20–60 个汉字，可播报正文最多 80 字。先直接回应当前问题，只讲一个最重要的点；复杂话题分轮展开，等用户继续问。不要演讲、长篇科普、条目罗列、连续举例或复述问题，不要加总结式结尾或空泛的等待用语。缺少关键信息时只问一个简短问题。只有用户明确要求详细说明时才展开，但每次仍最多 3 句、120 字。长度限制只计算可播报正文，不包含结构化字段或提交标记。若需要工具，直接调用工具，不输出工具前言。若确定可以回答，正文必须以 <speak/> 开头，此标记表示你已提交最终回答、此轮不再调用工具。标记后直接输出可播报正文，不使用其他 XML 标签。
`

// Immutable and only attached to the private runtime of one comparison leg.
type comparisonSettings struct {
	Variant string
	Signals map[string]any
	Risk    *riskTask
}

func PromptVariants() []promptcompare.Variant {
	out := []promptcompare.Variant{{ID: "legacy", Label: "旧版 System Prompt", Behavior: legacyConversationPrompt, Voice: legacyVoicePrompt}, {ID: "current", Label: "新版 System Prompt", Behavior: conversationPrompt, Voice: voicePrompt}}
	for i := range out {
		out[i].SHA256 = promptcompare.Digest([]string{out[i].Behavior, out[i].Voice})
	}
	return out
}

func (r *Runtime) behaviorPrompts() (string, string) {
	if r.comparison != nil && r.comparison.Variant == "legacy" {
		return legacyConversationPrompt, legacyVoicePrompt
	}
	return conversationPrompt, voicePrompt
}

func (r *Runtime) captureComparisonInput(t *Turn, messages []map[string]any) {
	if r.comparison == nil {
		return
	}
	// Only the behavior/voice text differs. All other system instructions are
	// separately fingerprinted in the manifest. Full provider parameters are
	// hashed here, never exposed (they may contain private provider extensions).
	session, _ := r.Store.Session(t.SessionID)
	character, _ := r.Character(session.CharacterID)
	hash := promptcompare.Digest(map[string]any{"messages": messages[1:], "persona": character.Persona, "provider": r.Config.Providers["llm"], "policy": compactSupport(*t.Support), "tools": []any{}, "disable_thinking": true, "max_tokens": 1024, "protocol": comparisonProtocolHash()})
	r.Emit(t, "comparison.input", map[string]any{"conditions_hash": hash})
}
func comparisonProtocolHash() string {
	return promptcompare.Digest([]string{commitPrompt, expressionPrompt, perceptionPrompt, memoryContextPrompt, supportComposePrompt, supportGuardPrompt, supportRiskPrompt, supportConstraintRepairPrompt})
}
