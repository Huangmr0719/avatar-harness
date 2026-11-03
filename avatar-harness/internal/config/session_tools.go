package config

// Upgrade existing tool-enabled configurations; an explicitly empty tool list
// still disables tools. Keep user-supplied providers with the same names.
func (c *Config) addSessionTools() {
	if len(c.Tools) == 0 {
		return
	}
	for _, tool := range []Tool{
		{Name: "set_support_mode", Description: "根据当前交流需要选择支持方式：humanistic 倾听与共情；cbt 梳理具体想法与小行动；off 回到普通交谈。由助手选择，不要求用户理解或配置方法。不作诊断，不仅凭表情标签启动。", Kind: "internal", TimeoutMS: 500, Parameters: map[string]any{"type": "object", "properties": map[string]any{"mode": map[string]any{"type": "string", "enum": []any{"humanistic", "cbt", "off"}}}, "required": []any{"mode"}, "additionalProperties": false}},
		{Name: "start_mindfulness", Description: "需要简短呼吸或正念练习时，安排播放当前正念视频。用户明确要求、接受建议，或当前交流适合时可调用；明确拒绝时不调用。先用一句话说明，语音结束后自动播放，用户可随时停止。工具成功只代表已安排，不代表已播放或已产生效果。", Kind: "internal", TimeoutMS: 500, Parameters: map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false}},
	} {
		found := false
		for _, existing := range c.Tools {
			found = found || existing.Name == tool.Name
		}
		if !found {
			c.Tools = append(c.Tools, tool)
		}
	}
}
