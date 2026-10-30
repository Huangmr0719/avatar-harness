package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"avatarharness/internal/config"
	"golang.org/x/term"
)

type terminal struct {
	reader      *bufio.Reader
	out         io.Writer
	fd          int
	interactive bool
}

func (t *terminal) prompt(label, defaultValue string) (string, error) {
	fmt.Fprintf(t.out, "  %s", label)
	if defaultValue != "" {
		fmt.Fprintf(t.out, " [%s]", defaultValue)
	}
	fmt.Fprint(t.out, ": ")
	raw, err := t.reader.ReadString('\n')
	if err != nil && len(raw) == 0 {
		return "", err
	}
	value := strings.TrimSpace(raw)
	if value == "" {
		value = defaultValue
	}
	return value, nil
}
func (t *terminal) choose(label string, options []string, defaultValue int) (int, error) {
	fmt.Fprintf(t.out, "\n%s\n", label)
	for i, item := range options {
		fmt.Fprintf(t.out, "  %d. %s\n", i+1, item)
	}
	for {
		raw, err := t.prompt("选择", strconv.Itoa(defaultValue))
		if err != nil {
			return 0, err
		}
		n, err := strconv.Atoi(raw)
		if err == nil && n >= 1 && n <= len(options) {
			return n, nil
		}
		fmt.Fprintln(t.out, "  请输入列表中的编号。")
	}
}
func (t *terminal) secret(label string) (string, error) {
	fmt.Fprint(t.out, "  "+label+": ")
	if t.interactive {
		raw, err := term.ReadPassword(t.fd)
		fmt.Fprintln(t.out)
		return strings.TrimSpace(string(raw)), err
	}
	raw, err := t.reader.ReadString('\n')
	if err != nil && len(raw) == 0 {
		return "", err
	}
	return strings.TrimSpace(raw), nil
}
func (t *terminal) wizard(c *config.Config, path string) error {
	fmt.Fprintln(t.out, "\nAvatar Harness · Go\n先配置服务，再展示 Web。模型和密钥均可稍后接入。")
	if err := t.configure(c, path, "all"); err != nil {
		return err
	}
	name, err := t.prompt("首个角色名", c.Characters[0].Name)
	if err != nil {
		return err
	}
	c.Characters[0].Name = name
	port, err := t.prompt("Web 端口", "8765")
	if err != nil {
		return err
	}
	c.Port, err = strconv.Atoi(port)
	return err
}
func (t *terminal) configure(c *config.Config, path, role string) error {
	roles := config.Roles
	if role != "all" {
		valid := false
		for _, name := range roles {
			if name == role {
				valid = true
			}
		}
		if !valid {
			return fmt.Errorf("未知 provider %s", role)
		}
		roles = []string{role}
	}
	for _, name := range roles {
		p := c.Providers[name]
		options := []string{"FastAPI / 自有 HTTP 服务", "内部 Go 函数（编译注册）", "稍后配置"}
		defaultChoice := len(options)
		if name == "llm" {
			options = append([]string{"百炼模型 API（DeepSeek / 千问等）", "其他 OpenAI 兼容 API"}, options...)
			defaultChoice = len(options)
		} else if name == "asr" || name == "tts" {
			options = append(options, "百炼 Qwen 实时语音（原生 WebSocket）")
		}
		selected, err := t.choose(strings.ToUpper(name)+" provider", options, defaultChoice)
		if err != nil {
			return err
		}
		answer := options[selected-1]
		next := config.Provider{TimeoutMS: 15000}
		switch answer {
		case "稍后配置":
			next.Kind = "deferred"
		case "内部 Go 函数（编译注册）":
			next.Kind = "internal"
			next.Factory, err = t.prompt("已注册 Go 函数名", p.Factory)
		case "FastAPI / 自有 HTTP 服务":
			next.Kind = "fastapi"
			next.BaseURL, err = t.prompt("服务地址（追加 /invoke 或 /stream）", p.BaseURL)
			if err == nil {
				value, e := t.prompt("支持 NDJSON /stream，y/n", "n")
				err = e
				next.Streaming = value == "y"
			}
		case "百炼 Qwen 实时语音（原生 WebSocket）":
			presets, _ := config.BailianProviders("")
			next = presets[name]
			next.BaseURL, err = t.prompt("WebSocket 地址", next.BaseURL)
			if err == nil {
				next.Model, err = t.prompt("模型 ID", next.Model)
			}
		default:
			next.Kind = "openai"
			base := p.BaseURL
			model := p.Model
			if selected == 1 {
				presets, _ := config.BailianProviders("")
				next = presets["llm"]
				base, model = next.BaseURL, next.Model
			}
			next.BaseURL, err = t.prompt("接口基础地址", base)
			if err == nil {
				next.Model, err = t.prompt("模型 ID", model)
			}
			if next.Parameters == nil {
				next.Parameters = map[string]any{"max_tokens": 512}
			}
		}
		if err != nil {
			return err
		}
		if next.Kind == "fastapi" || next.Kind == "openai" || next.Kind == "bailian" {
			env := p.APIKeyEnv
			if name == "llm" && selected == 1 || next.Kind == "bailian" {
				env = config.BailianKeyEnv
			}
			next.APIKeyEnv, err = t.prompt("密钥环境变量名，无鉴权留空", env)
			if err != nil {
				return err
			}
			if next.APIKeyEnv != "" && os.Getenv(next.APIKeyEnv) == "" {
				key, e := t.secret("密钥（隐藏输入，回车表示稍后配置）")
				if e != nil {
					return e
				}
				if key != "" {
					if err = config.SaveSecret(filepath.Join(filepath.Dir(path), "secrets.env"), next.APIKeyEnv, key); err != nil {
						return err
					}
				}
			}
		}
		if next.Kind != "deferred" {
			value, e := t.prompt("超时毫秒", strconv.Itoa(next.TimeoutMS))
			if e != nil {
				return e
			}
			next.TimeoutMS, err = strconv.Atoi(value)
			if err != nil {
				return err
			}
		}
		c.Providers[name] = next
	}
	if err := config.Save(*c, path); err != nil {
		return err
	}
	fmt.Fprintln(t.out, "配置已保存；正在运行的服务需停止后重启生效。")
	return nil
}
func (t *terminal) addTool(c *config.Config) error {
	var err error
	tool := config.Tool{TimeoutMS: 500, ReadOnly: true}
	tool.Name, err = t.prompt("工具名称", "")
	if err != nil {
		return err
	}
	tool.Description, err = t.prompt("用途", "")
	if err != nil {
		return err
	}
	kind, err := t.choose("接入形式", []string{"FastAPI / 自有 HTTP", "内部 Go 函数"}, 1)
	if err != nil {
		return err
	}
	if kind == 1 {
		tool.Kind = "fastapi"
		tool.BaseURL, err = t.prompt("服务基础地址", "http://127.0.0.1:9901")
		if err == nil {
			tool.APIKeyEnv, err = t.prompt("密钥环境变量名", "")
		}
	} else {
		tool.Kind = "internal"
		tool.Factory, err = t.prompt("已注册 Go 函数名", tool.Name)
	}
	if err != nil {
		return err
	}
	raw, err := t.prompt("参数 JSON Schema", `{"type":"object","properties":{},"additionalProperties":false}`)
	if err != nil {
		return err
	}
	if err = json.Unmarshal([]byte(raw), &tool.Parameters); err != nil {
		return err
	}
	for i, old := range c.Tools {
		if old.Name == tool.Name {
			c.Tools[i] = tool
			return nil
		}
	}
	c.Tools = append(c.Tools, tool)
	return nil
}
func (t *terminal) addCharacter(c *config.Config) error {
	var err error
	ch := config.Character{Renderer: config.Renderer{Kind: "none"}, PAD: map[string]float64{"pleasure": 0, "arousal": 0, "dominance": 0}}
	ch.ID, err = t.prompt("角色 ID", "")
	if err != nil {
		return err
	}
	ch.Name, err = t.prompt("显示名称", "")
	if err != nil {
		return err
	}
	ch.Persona, err = t.prompt("人设", "自然、简洁地使用中文回答。")
	if err != nil {
		return err
	}
	ch.Voice, err = t.prompt("音色 ID，可稍后填写", "")
	if err != nil {
		return err
	}
	for i, old := range c.Characters {
		if old.ID == ch.ID {
			c.Characters[i] = ch
			return nil
		}
	}
	c.Characters = append(c.Characters, ch)
	fmt.Fprintln(t.out, "角色已加入，可用 configure-avatar 设置展示。")
	return nil
}
func (t *terminal) avatar(c *config.Config) error {
	options := []string{}
	for _, ch := range c.Characters {
		options = append(options, ch.Name+" · "+ch.ID)
	}
	index, err := t.choose("选择角色", options, 1)
	if err != nil {
		return err
	}
	ch := &c.Characters[index-1]
	kind, err := t.choose("形象展示", []string{"本地 GLB / VRM / LAM ZIP，浏览器渲染", "远程 GLB / VRM / LAM ZIP 资产 URL", "其他数字人渲染页面", "服务端视频 URL", "稍后接入"}, 5)
	if err != nil {
		return err
	}
	switch kind {
	case 1:
		ch.AssetPath, err = t.prompt("资产文件路径", ch.AssetPath)
		if err == nil {
			ch.AssetPath, _ = filepath.Abs(ch.AssetPath)
			_, err = os.Stat(ch.AssetPath)
		}
		ext := strings.ToLower(filepath.Ext(ch.AssetPath))
		if err == nil && ext != ".glb" && ext != ".vrm" && ext != ".zip" {
			return fmt.Errorf("需要 GLB、VRM 或 LAM ZIP")
		}
		ch.Renderer = config.Renderer{Kind: "scene", AssetURL: "/api/characters/" + ch.ID + "/asset"}
		if ext == ".zip" {
			ch.Renderer = config.Renderer{Kind: "lam", AssetURL: "/api/characters/" + ch.ID + "/asset/avatar.zip"}
		}
	case 2:
		ch.Renderer.Kind = "scene"
		ch.Renderer.AssetURL, err = t.prompt("公开资产 URL", ch.Renderer.AssetURL)
		if strings.HasSuffix(strings.ToLower(strings.Split(ch.Renderer.AssetURL, "?")[0]), ".zip") {
			ch.Renderer.Kind = "lam"
		}
	case 3:
		ch.Renderer.Kind = "iframe"
		ch.Renderer.URL, err = t.prompt("渲染页面 URL", ch.Renderer.URL)
	case 4:
		ch.Renderer.Kind = "video"
		ch.Renderer.URL, err = t.prompt("浏览器可播放的视频 URL", ch.Renderer.URL)
	case 5:
		ch.Renderer = config.Renderer{Kind: "none"}
	}
	return err
}
