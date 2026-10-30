package config

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type Provider struct {
	Kind       string         `json:"kind"`
	BaseURL    string         `json:"base_url"`
	Model      string         `json:"model"`
	APIKeyEnv  string         `json:"api_key_env"`
	Factory    string         `json:"factory,omitempty"`
	Streaming  bool           `json:"streaming"`
	TimeoutMS  int            `json:"timeout_ms"`
	Parameters map[string]any `json:"parameters,omitempty"`
}
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Kind        string         `json:"kind"`
	BaseURL     string         `json:"base_url,omitempty"`
	APIKeyEnv   string         `json:"api_key_env,omitempty"`
	Factory     string         `json:"factory,omitempty"`
	TimeoutMS   int            `json:"timeout_ms"`
	ReadOnly    bool           `json:"read_only"`
	Parameters  map[string]any `json:"parameters"`
}
type Renderer struct {
	Kind          string `json:"kind"`
	AssetURL      string `json:"asset_url,omitempty"`
	URL           string `json:"url,omitempty"`
	IncludesAudio bool   `json:"includes_audio,omitempty"`
}
type Character struct {
	ID        string             `json:"id"`
	Name      string             `json:"name"`
	AssetPath string             `json:"asset_path"`
	Voice     string             `json:"voice"`
	Persona   string             `json:"persona"`
	PAD       map[string]float64 `json:"pad"`
	Renderer  Renderer           `json:"renderer"`
}
type Limits struct {
	FirstResponseMS    int `json:"first_response_ms"`
	ToolBatchMS        int `json:"tool_batch_ms"`
	MaxToolBatches     int `json:"max_tool_batches"`
	MaxTurnMS          int `json:"max_turn_ms"`
	TTSConcurrency     int `json:"tts_concurrency"`
	TTSFirstChunkMS    int `json:"tts_first_chunk_ms"`
	TTSChunkMS         int `json:"tts_chunk_ms"`
	MaxHistoryMessages int `json:"max_history_messages"`
	VoiceSilenceMS     int `json:"voice_silence_ms"`
	VoiceOnsetMS       int `json:"voice_onset_ms"`
}
type Config struct {
	Version    int                 `json:"version"`
	Host       string              `json:"host"`
	Port       int                 `json:"port"`
	Providers  map[string]Provider `json:"providers"`
	Tools      []Tool              `json:"tools"`
	Characters []Character         `json:"characters"`
	Limits     Limits              `json:"limits"`
	OACURL     string              `json:"oac_url,omitempty"`
}

var Roles = []string{"llm", "asr", "tts", "avatar"}
var namePattern = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_-]{0,63}$`)

func Default() Config {
	c := Config{Version: 2, Host: "127.0.0.1", Port: 8765, Providers: map[string]Provider{},
		Limits: Limits{FirstResponseMS: 3000, ToolBatchMS: 500, MaxToolBatches: 1, MaxTurnMS: 30000, TTSConcurrency: 2, TTSFirstChunkMS: 180, TTSChunkMS: 400, MaxHistoryMessages: 20, VoiceSilenceMS: 400, VoiceOnsetMS: 80}}
	for _, role := range Roles {
		c.Providers[role] = Provider{Kind: "deferred", TimeoutMS: 15000}
	}
	c.Tools = []Tool{
		{Name: "get_current_time", Description: "读取真实当前日期和时间。", Kind: "internal", TimeoutMS: 500, ReadOnly: true, Parameters: map[string]any{"type": "object", "properties": map[string]any{"timezone": map[string]any{"type": "string"}}, "additionalProperties": false}},
		{Name: "list_characters", Description: "列出已配置角色及资产状态。", Kind: "internal", TimeoutMS: 500, ReadOnly: true, Parameters: map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false}},
	}
	c.Characters = []Character{{ID: "character-01", Name: "角色 01", Persona: "自然、简洁地使用中文回答；需要外部事实时使用工具。", PAD: map[string]float64{"pleasure": .2, "arousal": 0, "dominance": 0}, Renderer: Renderer{Kind: "none"}}}
	return c
}
func DefaultPath() string {
	p := os.Getenv("AVATAR_HARNESS_CONFIG")
	if p == "" {
		p = ".avatar-harness/config.json"
	}
	p, _ = filepath.Abs(p)
	return p
}
func (c *Config) Validate() error {
	if c.Host == "" {
		c.Host = "127.0.0.1"
	}
	if c.Port < 1024 || c.Port > 65535 {
		return fmt.Errorf("端口必须在 1024–65535 之间")
	}
	if len(c.Characters) == 0 {
		return fmt.Errorf("至少配置一个角色")
	}
	seen := map[string]bool{}
	for i := range c.Characters {
		ch := &c.Characters[i]
		if ch.ID == "" || seen[ch.ID] {
			return fmt.Errorf("角色 ID 为空或重复")
		}
		seen[ch.ID] = true
		if ch.Renderer.Kind == "" {
			ch.Renderer.Kind = "none"
		}
		switch ch.Renderer.Kind {
		case "none", "scene", "lam", "iframe", "video":
		default:
			return fmt.Errorf("不支持的形象展示类型 %s", ch.Renderer.Kind)
		}
		if (ch.Renderer.Kind == "scene" || ch.Renderer.Kind == "lam") && ch.Renderer.AssetURL == "" && ch.AssetPath != "" {
			ch.Renderer.AssetURL = "/api/characters/" + ch.ID + "/asset"
			if ch.Renderer.Kind == "lam" {
				ch.Renderer.AssetURL += "/avatar.zip"
			}
		}
	}
	if len(c.Providers) != 4 {
		return fmt.Errorf("providers 必须包含 llm/asr/tts/avatar")
	}
	for _, role := range Roles {
		p, ok := c.Providers[role]
		if !ok {
			return fmt.Errorf("缺少 %s provider", role)
		}
		if p.Kind == "" {
			p.Kind = "deferred"
		}
		if p.TimeoutMS == 0 {
			p.TimeoutMS = 15000
		}
		switch p.Kind {
		case "deferred":
		case "fastapi", "openai":
			if !strings.HasPrefix(p.BaseURL, "http://") && !strings.HasPrefix(p.BaseURL, "https://") {
				return fmt.Errorf("%s 需要 HTTP(S) 地址", role)
			}
			if p.Kind == "openai" && role != "llm" {
				return fmt.Errorf("OpenAI 兼容传输只适用于 LLM，其他服务选择 FastAPI")
			}
		case "bailian":
			if role != "asr" && role != "tts" {
				return fmt.Errorf("百炼实时语音传输只用于 ASR/TTS；LLM 使用 OpenAI 兼容接口")
			}
			u, err := url.Parse(p.BaseURL)
			if err != nil || u.Host == "" || (u.Scheme != "wss" && u.Scheme != "ws") || p.Model == "" {
				return fmt.Errorf("%s 需要 WebSocket 地址和百炼模型 ID", role)
			}
			if p.APIKeyEnv == "" {
				return fmt.Errorf("%s 需要密钥环境变量名", role)
			}
		case "internal":
			if p.Factory == "" || strings.Contains(p.Factory, ":") {
				return fmt.Errorf("%s 的内部 provider 需要已注册 Go 函数名；Python 函数请包装为 FastAPI", role)
			}
		default:
			return fmt.Errorf("未知 provider 类型 %s", p.Kind)
		}
		if p.TimeoutMS < 10 || p.TimeoutMS > 120000 {
			return fmt.Errorf("%s 超时配置无效", role)
		}
		c.Providers[role] = p
	}
	seen = map[string]bool{}
	for i := range c.Tools {
		t := &c.Tools[i]
		if !namePattern.MatchString(t.Name) || seen[t.Name] {
			return fmt.Errorf("工具名称不合法或重复：%s", t.Name)
		}
		seen[t.Name] = true
		if t.Kind == "" {
			t.Kind = "internal"
		}
		if t.TimeoutMS == 0 {
			t.TimeoutMS = 500
		}
		if t.Kind != "internal" && t.Kind != "fastapi" {
			return fmt.Errorf("工具只支持内部 Go 或 FastAPI")
		}
		if t.Kind == "fastapi" && !strings.HasPrefix(t.BaseURL, "http://") && !strings.HasPrefix(t.BaseURL, "https://") {
			return fmt.Errorf("工具 %s 需要 HTTP(S) 地址", t.Name)
		}
		if t.TimeoutMS < 10 || t.TimeoutMS > 120000 {
			return fmt.Errorf("工具超时配置无效")
		}
		if t.Parameters == nil {
			t.Parameters = map[string]any{"type": "object"}
		}
	}
	if c.Limits.FirstResponseMS < 200 || c.Limits.MaxTurnMS < 1000 || c.Limits.ToolBatchMS < 10 || c.Limits.MaxToolBatches < 0 || c.Limits.MaxToolBatches > 3 || c.Limits.TTSConcurrency < 1 || c.Limits.TTSConcurrency > 4 || c.Limits.MaxHistoryMessages < 2 {
		return fmt.Errorf("时间或并发预算配置无效")
	}
	if c.Limits.TTSFirstChunkMS == 0 {
		c.Limits.TTSFirstChunkMS = 180
	}
	if c.Limits.TTSChunkMS == 0 {
		c.Limits.TTSChunkMS = 400
	}
	if c.Limits.TTSFirstChunkMS < 50 || c.Limits.TTSFirstChunkMS > 1000 || c.Limits.TTSChunkMS < 100 || c.Limits.TTSChunkMS > 2000 {
		return fmt.Errorf("TTS 分段等待时间配置无效")
	}
	if c.Limits.VoiceSilenceMS == 0 {
		c.Limits.VoiceSilenceMS = 400
	}
	if c.Limits.VoiceOnsetMS == 0 {
		c.Limits.VoiceOnsetMS = 80
	}
	if c.Limits.VoiceSilenceMS < 200 || c.Limits.VoiceSilenceMS > 2000 || c.Limits.VoiceOnsetMS < 40 || c.Limits.VoiceOnsetMS > 400 {
		return fmt.Errorf("语音起止检测时间配置无效")
	}
	return nil
}
func Load(path string) (Config, error) {
	c := Default()
	raw, err := os.ReadFile(path)
	if err != nil {
		return c, err
	}
	if err = json.Unmarshal(raw, &c); err != nil {
		return c, err
	}
	LoadSecrets(filepath.Join(filepath.Dir(path), "secrets.env"))
	err = c.Validate()
	return c, err
}
func Save(c Config, path string) error {
	if err := c.Validate(); err != nil {
		return err
	}
	c.Version = 2
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	if err = os.WriteFile(path+".tmp", append(raw, '\n'), 0600); err != nil {
		return err
	}
	return os.Rename(path+".tmp", path)
}
func LoadSecrets(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || os.Getenv(key) != "" {
			continue
		}
		if v, err := strconv.Unquote(value); err == nil {
			value = v
		}
		_ = os.Setenv(key, value)
	}
}
func SaveSecret(path, key, value string) error {
	if !regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`).MatchString(key) || strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("密钥环境变量名或格式无效")
	}
	raw, _ := os.ReadFile(path)
	lines := []string{}
	for _, line := range strings.Split(string(raw), "\n") {
		if line != "" && !strings.HasPrefix(line, key+"=") {
			lines = append(lines, line)
		}
	}
	lines = append(lines, key+"="+strconv.Quote(value))
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0600); err != nil {
		return err
	}
	_ = os.Chmod(path, 0600)
	return os.Setenv(key, value)
}
func State(p Provider) map[string]any {
	status := "configured"
	ready := true
	if p.Kind == "deferred" {
		status = "deferred"
		ready = false
	} else if p.APIKeyEnv != "" && os.Getenv(p.APIKeyEnv) == "" {
		status = "missing_key"
		ready = false
	}
	return map[string]any{"kind": p.Kind, "model": p.Model, "configured": ready, "status": status, "api_key_env": p.APIKeyEnv, "base_url": p.BaseURL, "live_input": p.Kind == "bailian" && strings.Contains(p.Model, "asr")}
}
