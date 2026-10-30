package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"avatarharness/integrations"
	"avatarharness/internal/config"
	"avatarharness/internal/provider"
	"avatarharness/internal/server"
	"avatarharness/internal/store"
	"golang.org/x/term"
	"gopkg.in/yaml.v3"
)

func Execute(args []string, input io.Reader, output io.Writer) error {
	path := config.DefaultPath()
	clean := []string{}
	for i := 0; i < len(args); i++ {
		if args[i] == "--config" {
			if i+1 >= len(args) {
				return fmt.Errorf("--config 缺少路径")
			}
			i++
			path = args[i]
		} else if strings.HasPrefix(args[i], "--config=") {
			path = strings.TrimPrefix(args[i], "--config=")
		} else {
			clean = append(clean, args[i])
		}
	}
	path, _ = filepath.Abs(path)
	t := &terminal{reader: bufio.NewReader(input), out: output, fd: -1}
	if file, ok := input.(*os.File); ok {
		t.fd = int(file.Fd())
		t.interactive = term.IsTerminal(t.fd)
	}
	command := ""
	rest := []string{}
	if len(clean) > 0 {
		command = clean[0]
		rest = clean[1:]
	}
	if command == "help" || command == "--help" || command == "-h" {
		fmt.Fprintln(output, "Avatar Harness · Go\n./start [--config 文件] [命令]\ninit [--defaults]     首次配置\nconfigure all|llm|asr|tts|avatar\nconfigure bailian [--defaults]  百炼 LLM/ASR/TTS 一次配置\nconfigure-avatar    形象资产与展示方式\nproviders / doctor [--probe]\ntools / tool 名称 --args JSON\nadd-tool / add-character\nserve [--no-open] [--port 8765] [--host 127.0.0.1]\nstop\nexport-oac --asset 角色.zip --output 配置.yaml")
		return nil
	}
	if command == "init" {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("配置已经存在，使用 configure 修改")
		}
		c := config.Default()
		defaults := len(rest) > 0 && rest[0] == "--defaults"
		if !defaults {
			if err := t.wizard(&c, path); err != nil {
				return err
			}
		}
		if err := config.Save(c, path); err != nil {
			return err
		}
		show(output, &c, path)
		return nil
	}
	c, err := config.Load(path)
	if os.IsNotExist(err) && command == "" && t.interactive {
		c = config.Default()
		if err = t.wizard(&c, path); err != nil {
			return err
		}
		if err = config.Save(c, path); err != nil {
			return err
		}
	} else if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("先运行 ./start init；无密钥可使用 ./start init --defaults")
		}
		return err
	}
	if command == "" && t.interactive {
		for {
			show(output, &c, path)
			choice, err := t.choose("启动前配置", []string{"启动 Web 工作台", "配置模型与服务 provider", "配置数字人展示方式", "退出"}, 1)
			if err != nil {
				return err
			}
			switch choice {
			case 1:
				command = "serve"
			case 2:
				if err = t.configure(&c, path, "all"); err != nil {
					return err
				}
				continue
			case 3:
				if err = t.avatar(&c); err != nil {
					return err
				}
				if err = config.Save(c, path); err != nil {
					return err
				}
				continue
			case 4:
				return nil
			}
			break
		}
	}
	switch command {
	case "", "serve":
		flags := flag.NewFlagSet("serve", flag.ContinueOnError)
		flags.SetOutput(output)
		noOpen := flags.Bool("no-open", false, "")
		port := flags.Int("port", c.Port, "")
		host := flags.String("host", c.Host, "")
		if err = flags.Parse(rest); err != nil {
			return err
		}
		c.Port = *port
		c.Host = *host
		if err = c.Validate(); err != nil {
			return err
		}
		return serve(&c, path, !*noOpen, output)
	case "stop":
		return stop(path, output)
	case "configure":
		if len(rest) >= 1 && rest[0] == "bailian" {
			if len(rest) > 2 || len(rest) == 2 && rest[1] != "--defaults" {
				return fmt.Errorf("使用 configure bailian [--defaults]")
			}
			return t.bailian(&c, path, len(rest) == 2)
		}
		if len(rest) != 1 {
			return fmt.Errorf("指定 bailian、all、llm、asr、tts 或 avatar")
		}
		return t.configure(&c, path, rest[0])
	case "configure-avatar":
		if err = t.avatar(&c); err != nil {
			return err
		}
		return config.Save(c, path)
	case "providers", "doctor":
		show(output, &c, path)
		if command == "doctor" && len(rest) > 0 && rest[0] == "--probe" {
			transport := http.DefaultTransport.(*http.Transport).Clone()
			transport.Proxy = nil
			client := &http.Client{Transport: transport, Timeout: 2 * time.Second}
			for _, role := range config.Roles {
				p := c.Providers[role]
				if p.Kind == "fastapi" {
					req, _ := http.NewRequest("GET", strings.TrimRight(p.BaseURL, "/")+"/health", nil)
					if p.APIKeyEnv != "" {
						req.Header.Set("Authorization", "Bearer "+os.Getenv(p.APIKeyEnv))
					}
					res, err := client.Do(req)
					if err != nil {
						fmt.Fprintf(output, "  %s 连接失败：%v\n", role, err)
					} else {
						fmt.Fprintf(output, "  %s /health → HTTP %d（不代表推理已验证）\n", role, res.StatusCode)
						res.Body.Close()
					}
				}
			}
		}
		return nil
	case "tools":
		for _, tool := range c.Tools {
			fmt.Fprintf(output, "%-24s %-8s %d ms · %s\n", tool.Name, tool.Kind, tool.TimeoutMS, tool.Description)
		}
		return nil
	case "tool":
		if len(rest) == 0 {
			return fmt.Errorf("缺少工具名称")
		}
		flags := flag.NewFlagSet("tool", flag.ContinueOnError)
		raw := flags.String("args", "{}", "")
		if err = flags.Parse(rest[1:]); err != nil {
			return err
		}
		var args map[string]any
		if err = json.Unmarshal([]byte(*raw), &args); err != nil || args == nil {
			return fmt.Errorf("参数必须是 JSON 对象")
		}
		bus, err := provider.New(&c, nil)
		if err != nil {
			return err
		}
		result := bus.Invoke(context.Background(), rest[0], args, nil)
		return json.NewEncoder(output).Encode(result)
	case "add-tool":
		if err = t.addTool(&c); err != nil {
			return err
		}
		return config.Save(c, path)
	case "add-character":
		if err = t.addCharacter(&c); err != nil {
			return err
		}
		return config.Save(c, path)
	case "export-oac":
		return exportOAC(&c, rest, output)
	default:
		return fmt.Errorf("未知命令 %q；使用 ./start --help", command)
	}
}
func show(out io.Writer, c *config.Config, path string) {
	fmt.Fprintf(out, "\nAvatar Harness · Go %s\n配置 %s\n", server.Version, path)
	for _, role := range config.Roles {
		p := c.Providers[role]
		state := config.State(p)
		label := map[any]string{"configured": "已配置（未验证推理）", "deferred": "稍后接入", "missing_key": "缺少密钥"}[state["status"]]
		fmt.Fprintf(out, "  %-8s %-9s %s · %s\n", strings.ToUpper(role), p.Kind, label, p.Model)
	}
	fmt.Fprintf(out, "  %d 个角色 · %d 个工具\n", len(c.Characters), len(c.Tools))
}

type record struct {
	PID        int    `json:"pid"`
	Port       int    `json:"port"`
	ConfigPath string `json:"config_path"`
	Runtime    string `json:"runtime"`
}

func live(path string) (record, bool) {
	var rec record
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(path), "server.json"))
	if err != nil || json.Unmarshal(raw, &rec) != nil || rec.ConfigPath != path || rec.Runtime != "go" {
		return rec, false
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	client := &http.Client{Transport: transport, Timeout: 700 * time.Millisecond}
	res, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/api/health", rec.Port))
	if err != nil {
		return rec, false
	}
	defer res.Body.Close()
	var health struct {
		PID     int    `json:"process_id"`
		Runtime string `json:"runtime"`
	}
	err = json.NewDecoder(res.Body).Decode(&health)
	return rec, err == nil && health.PID == rec.PID && health.Runtime == "go"
}
func stop(path string, out io.Writer) error {
	rec, ok := live(path)
	if !ok {
		fmt.Fprintln(out, "本配置没有运行中的 Go 服务。")
		return nil
	}
	process, err := os.FindProcess(rec.PID)
	if err != nil {
		return err
	}
	if err = process.Signal(os.Interrupt); err != nil {
		return err
	}
	for i := 0; i < 40; i++ {
		if _, ok = live(path); !ok {
			fmt.Fprintln(out, "Go 服务已停止。")
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	fmt.Fprintln(out, "已请求停止，正在释放连接。")
	return nil
}
func openBrowser(url string) {
	switch runtime.GOOS {
	case "darwin":
		_ = exec.Command("open", url).Start()
	case "linux":
		_ = exec.Command("xdg-open", url).Start()
	}
}
func serve(c *config.Config, path string, open bool, out io.Writer) error {
	if rec, ok := live(path); ok {
		if configurationChanged(path) {
			fmt.Fprintln(out, "检测到配置、密钥或版本更新，正在重启工作台以加载。")
			if err := stop(path, out); err != nil {
				return err
			}
		} else {
			url := fmt.Sprintf("http://127.0.0.1:%d", rec.Port)
			fmt.Fprintln(out, "已在运行：", url)
			if open {
				openBrowser(url)
			}
			return nil
		}
	}
	bus, err := provider.New(c, nil)
	if err != nil {
		return err
	}
	state, err := store.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer state.Close()
	app := server.New(c, state, bus)
	listener, err := net.Listen("tcp", fmt.Sprintf("%s:%d", c.Host, c.Port))
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: app.Handler, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	recordPath := filepath.Join(filepath.Dir(path), "server.json")
	raw, _ := json.Marshal(record{os.Getpid(), c.Port, path, "go"})
	if err = os.WriteFile(recordPath, raw, 0600); err != nil {
		listener.Close()
		return err
	}
	defer func() {
		raw, _ := os.ReadFile(recordPath)
		var current record
		if json.Unmarshal(raw, &current) == nil && current.PID == os.Getpid() {
			_ = os.Remove(recordPath)
		}
	}()
	show(out, c, path)
	url := fmt.Sprintf("http://127.0.0.1:%d", c.Port)
	fmt.Fprintf(out, "\nWeb %s\n接口 %s/docs\nCtrl+C 停止。\n", url, url)
	if open {
		openBrowser(url)
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	done := make(chan error, 1)
	go func() { done <- srv.Serve(listener) }()
	select {
	case <-signals:
		app.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		err = srv.Shutdown(ctx)
	case err = <-done:
		app.Close()
	}
	if errors.Is(err, http.ErrServerClosed) {
		err = nil
	}
	return err
}

func configurationChanged(path string) bool {
	started, err := os.Stat(filepath.Join(filepath.Dir(path), "server.json"))
	if err != nil {
		return false
	}
	executable, _ := os.Executable()
	for _, file := range []string{path, filepath.Join(filepath.Dir(path), "secrets.env"), executable} {
		if info, err := os.Stat(file); err == nil && info.ModTime().After(started.ModTime()) {
			return true
		}
	}
	return false
}
func exportOAC(c *config.Config, args []string, out io.Writer) error {
	flags := flag.NewFlagSet("export-oac", flag.ContinueOnError)
	output := flags.String("output", "integrations/oac/chat_with_harness_lam.yaml", "")
	asset := flags.String("asset", "", "")
	characterID := flags.String("character", c.Characters[0].ID, "")
	harnessURL := flags.String("harness-url", fmt.Sprintf("http://127.0.0.1:%d", c.Port), "")
	if err := flags.Parse(args); err != nil {
		return err
	}
	raw, _ := integrations.Files.ReadFile("oac/lam-base.yaml")
	var root map[string]any
	if err := yaml.Unmarshal(raw, &root); err != nil {
		return err
	}
	def := root["default"].(map[string]any)
	engine := def["chat_engine"].(map[string]any)
	handlers := engine["handler_configs"].(map[string]any)
	delete(handlers, "Dify")
	found := false
	for _, ch := range c.Characters {
		if ch.ID == *characterID {
			found = true
			if *asset == "" {
				*asset = ch.AssetPath
			}
		}
	}
	if !found {
		return fmt.Errorf("角色不存在")
	}
	if *asset == "" {
		*asset = "REPLACE_WITH_PREGENERATED_LAM.zip"
	}
	handlers["LamClient"].(map[string]any)["asset_path"] = *asset
	handlers["LLMOpenAICompatible"] = map[string]any{"enabled": true, "module": "llm/openai_compatible/llm_handler_openai_compatible", "model_name": "avatar-harness/" + *characterID, "api_url": strings.TrimRight(*harnessURL, "/") + "/oac/v1", "api_key": "oac-local", "history_length": c.Limits.MaxHistoryMessages, "enable_video_input": false, "system_prompt": "使用自然口语回答，不输出 Markdown。"}
	data, err := yaml.Marshal(root)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(*output), 0755); err != nil {
		return err
	}
	if err = os.WriteFile(*output, append([]byte("# Go Harness → OpenAvatarChat。GPU、LAM 资产与语音凭据需要单独就绪。\n"), data...), 0644); err != nil {
		return err
	}
	fmt.Fprintln(out, "已导出", *output)
	return nil
}
