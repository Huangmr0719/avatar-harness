package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"avatarharness/internal/config"
)

func (t *terminal) bailian(c *config.Config, path string, defaults bool) error {
	workspace := ""
	previous := map[string]config.Provider{}
	for role, p := range c.Providers {
		previous[role] = p
	}
	if !defaults {
		for _, role := range []string{"llm", "asr", "tts"} {
			address := c.Providers[role].BaseURL
			if host, _, found := strings.Cut(strings.TrimPrefix(strings.TrimPrefix(address, "https://"), "wss://"), ".cn-beijing.maas.aliyuncs.com"); found {
				workspace = host
				break
			}
		}
		fmt.Fprintln(t.out, "\n百炼 · 北京地域 · LLM + ASR + TTS\n共用 DASHSCOPE_API_KEY；形象继续使用现有渲染方式。")
		var err error
		workspace, err = t.prompt("业务空间 ID（留空使用公共域名）", workspace)
		if err != nil {
			return err
		}
	}
	if err := c.UseBailian(workspace); err != nil {
		return err
	}
	if !defaults {
		for _, role := range []string{"llm", "asr", "tts"} {
			p := c.Providers[role]
			if old := previous[role]; old.Kind == p.Kind && old.APIKeyEnv == config.BailianKeyEnv {
				p.Model = old.Model
				if old.Parameters != nil {
					p.Parameters = old.Parameters
				}
			}
			var err error
			p.Model, err = t.prompt(strings.ToUpper(role)+" 模型 ID", p.Model)
			if err != nil {
				return err
			}
			if role == "tts" && strings.HasPrefix(p.Model, "qwen3-") {
				p.BaseURL = strings.TrimSuffix(p.BaseURL, "/inference") + "/realtime"
			}
			c.Providers[role] = p
		}
		p := c.Providers["tts"]
		defaultVoice, _ := p.Parameters["voice"].(string)
		if defaultVoice == "" {
			defaultVoice = config.BailianTTSVoice
		}
		if strings.HasPrefix(p.Model, "qwen3-") && defaultVoice == config.BailianTTSVoice {
			defaultVoice = "Cherry"
		}
		if strings.HasPrefix(p.Model, "qwen-audio-") && defaultVoice == "Cherry" {
			defaultVoice = config.BailianTTSVoice
		}
		voice, err := t.prompt("默认音色", defaultVoice)
		if err != nil {
			return err
		}
		p.Parameters["voice"] = voice
		c.Providers["tts"] = p
		if os.Getenv(config.BailianKeyEnv) == "" {
			key, err := t.secret("百炼 API Key（隐藏输入，回车稍后填写）")
			if err != nil {
				return err
			}
			if key != "" {
				if err = config.SaveSecret(filepath.Join(filepath.Dir(path), "secrets.env"), config.BailianKeyEnv, key); err != nil {
					return err
				}
			}
		}
	}
	if err := config.Save(*c, path); err != nil {
		return err
	}
	fmt.Fprintln(t.out, "百炼三项服务已保存。API Key 可稍后通过 ./start configure bailian 填写；重启服务后生效。")
	show(t.out, c, path)
	return nil
}
