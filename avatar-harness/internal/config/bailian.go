package config

import (
	"fmt"
	"regexp"
)

const (
	BailianKeyEnv   = "DASHSCOPE_API_KEY"
	BailianLLMModel = "deepseek-v4.1-flash-1017"
	BailianASRModel = "qwen3-asr-flash-realtime"
	BailianTTSModel = "qwen-audio-3.1-tts-flash"
	BailianTTSVoice = "longanhuan_v3.1"
)

// An empty workspace uses the public Beijing endpoint, which remains supported.
func BailianProviders(workspace string) (map[string]Provider, error) {
	host := "dashscope.aliyuncs.com"
	if workspace != "" {
		if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,62}$`).MatchString(workspace) {
			return nil, fmt.Errorf("百炼业务空间 ID 格式无效")
		}
		host = workspace + ".cn-beijing.maas.aliyuncs.com"
	}
	return map[string]Provider{
		"llm": {Kind: "openai", BaseURL: "https://" + host + "/compatible-mode/v1", Model: BailianLLMModel, APIKeyEnv: BailianKeyEnv, Streaming: true, TimeoutMS: 15000, Parameters: map[string]any{"enable_thinking": false, "max_tokens": 512}},
		"asr": {Kind: "bailian", BaseURL: "wss://" + host + "/api-ws/v1/realtime", Model: BailianASRModel, APIKeyEnv: BailianKeyEnv, Streaming: true, TimeoutMS: 15000, Parameters: map[string]any{"language": "zh"}},
		"tts": {Kind: "bailian", BaseURL: "wss://" + host + "/api-ws/v1/inference", Model: BailianTTSModel, APIKeyEnv: BailianKeyEnv, Streaming: true, TimeoutMS: 15000, Parameters: map[string]any{"voice": BailianTTSVoice, "language_hints": []string{"zh"}, "sample_rate": 24000}},
	}, nil
}

func (c *Config) UseBailian(workspace string) error {
	providers, err := BailianProviders(workspace)
	if err != nil {
		return err
	}
	for name, p := range providers {
		c.Providers[name] = p
	}
	return c.Validate()
}
