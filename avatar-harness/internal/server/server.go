package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"avatarharness/internal/config"
	"avatarharness/internal/provider"
	kernel "avatarharness/internal/runtime"
	"avatarharness/internal/store"
	"avatarharness/web"
)

const Version = "0.3.0"

type App struct {
	Config  *config.Config
	Runtime *kernel.Runtime
	Handler http.Handler
	done    context.Context
	cancel  context.CancelFunc
	voiceMu sync.Mutex
	voices  map[string]*voiceConnection
}

func New(c *config.Config, s *store.Store, b *provider.Bus) *App {
	ctx, cancel := context.WithCancel(context.Background())
	a := &App{Config: c, Runtime: kernel.New(c, s, b), done: ctx, cancel: cancel, voices: map[string]*voiceConnection{}}
	a.routes()
	return a
}
func (a *App) Close() { a.cancel(); a.Runtime.Shutdown() }
func write(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func problem(w http.ResponseWriter, status int, message string) {
	write(w, status, map[string]any{"detail": message})
}
func decode(w http.ResponseWriter, r *http.Request, out any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 32<<20)
	if err := json.NewDecoder(r.Body).Decode(out); err != nil {
		problem(w, 400, "无效的 JSON 请求")
		return false
	}
	return true
}
func (a *App) session(w http.ResponseWriter, r *http.Request) (store.Session, bool) {
	s, ok := a.Runtime.Store.Session(r.PathValue("id"))
	if !ok {
		problem(w, 404, "会话不存在")
	}
	return s, ok
}
func (a *App) routes() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		write(w, 200, map[string]any{"status": "ok", "version": Version, "runtime": "go", "go_version": runtime.Version(), "process_id": os.Getpid(), "gpu_required": false})
	})
	mux.HandleFunc("GET /api/state", func(w http.ResponseWriter, r *http.Request) {
		states := map[string]any{}
		for name, p := range a.Config.Providers {
			states[name] = config.State(p)
		}
		write(w, 200, map[string]any{"version": Version, "runtime": "go", "providers": states, "characters": a.Config.Characters, "tools": a.Config.Tools, "limits": a.Config.Limits, "configuration": "terminal", "oac_url": a.Config.OACURL})
	})
	mux.HandleFunc("GET /api/sessions", func(w http.ResponseWriter, r *http.Request) { write(w, 200, a.Runtime.Store.Sessions()) })
	mux.HandleFunc("POST /api/sessions", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			CharacterID string `json:"character_id"`
		}
		if !decode(w, r, &body) {
			return
		}
		if body.CharacterID == "" {
			body.CharacterID = a.Config.Characters[0].ID
		}
		if _, ok := a.Runtime.Character(body.CharacterID); !ok {
			problem(w, 422, "角色不存在")
			return
		}
		write(w, 201, a.Runtime.Store.Create(body.CharacterID))
	})
	mux.HandleFunc("GET /api/sessions/{id}/history", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := a.session(w, r); !ok {
			return
		}
		write(w, 200, a.Runtime.Store.Events(r.PathValue("id"), 0))
	})
	mux.HandleFunc("POST /api/sessions/{id}/turns", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := a.session(w, r); !ok {
			return
		}
		var body struct {
			Text   string `json:"text"`
			Source string `json:"source"`
		}
		if !decode(w, r, &body) {
			return
		}
		if len([]rune(body.Text)) > 16000 || body.Source != "" && body.Source != "text" && body.Source != "voice" {
			problem(w, 422, "输入长度或来源无效")
			return
		}
		a.interruptVoice(r.PathValue("id"))
		t, err := a.Runtime.Start(r.PathValue("id"), strings.TrimSpace(body.Text), body.Source, true, nil)
		if err != nil {
			problem(w, 422, err.Error())
			return
		}
		write(w, 202, map[string]any{"turn_id": t.ID})
	})
	mux.HandleFunc("POST /api/sessions/{id}/stop", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := a.session(w, r); !ok {
			return
		}
		stopped := a.interruptVoice(r.PathValue("id"))
		stopped = a.Runtime.Cancel(r.PathValue("id"), "用户停止") || stopped
		write(w, 200, map[string]any{"stopped": stopped})
	})
	mux.HandleFunc("DELETE /api/sessions/{id}/messages", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := a.session(w, r); !ok {
			return
		}
		sid := r.PathValue("id")
		a.Runtime.Cancel(sid, "清空日志")
		a.Runtime.Store.Clear(sid)
		a.Runtime.Store.Emit(sid, "", "session.cleared", map[string]any{})
		write(w, 200, map[string]any{"status": "ok"})
	})
	mux.HandleFunc("GET /api/sessions/{id}/events", a.events)
	mux.HandleFunc("POST /api/sessions/{id}/audio", a.audio)
	mux.HandleFunc("GET /api/sessions/{id}/voice", a.voice)
	mux.HandleFunc("POST /api/sessions/{id}/playback", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := a.session(w, r); !ok {
			return
		}
		var body map[string]any
		if !decode(w, r, &body) {
			return
		}
		elapsed, ok := body["elapsed_ms"].(float64)
		if !ok || elapsed < 0 || elapsed > 600000 {
			problem(w, 422, "起播时间无效")
			return
		}
		result, err := a.Runtime.Report(r.PathValue("id"), provider.String(body["turn_id"]), body)
		if err != nil {
			problem(w, 409, err.Error())
			return
		}
		write(w, 200, result)
	})
	mux.HandleFunc("GET /v1/capabilities", func(w http.ResponseWriter, r *http.Request) {
		states := map[string]any{}
		for name, p := range a.Config.Providers {
			states[name] = config.State(p)
		}
		write(w, 200, map[string]any{"providers": states, "tools": a.Runtime.Bus.Spec()})
	})
	mux.HandleFunc("POST /v1/capabilities/{name}/invoke", func(w http.ResponseWriter, r *http.Request) {
		var body provider.Request
		if !decode(w, r, &body) {
			return
		}
		if body.Input == nil {
			body.Input = map[string]any{}
		}
		write(w, 200, a.Runtime.Bus.Invoke(r.Context(), r.PathValue("name"), body.Input, body.Context))
	})
	mux.HandleFunc("POST /v1/capabilities/{name}/stream", func(w http.ResponseWriter, r *http.Request) {
		var body provider.Request
		if !decode(w, r, &body) {
			return
		}
		if _, ok := a.Config.Providers[r.PathValue("name")]; !ok {
			problem(w, 404, "流式能力不存在")
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		err := a.Runtime.Bus.Stream(r.Context(), r.PathValue("name"), body.Input, body.Context, func(e map[string]any) error {
			if err := json.NewEncoder(w).Encode(e); err != nil {
				return err
			}
			_ = http.NewResponseController(w).Flush()
			return nil
		})
		if err != nil {
			_ = json.NewEncoder(w).Encode(map[string]any{"type": "error", "error": err.Error()})
		}
	})
	mux.HandleFunc("GET /api/characters/{character}/asset", a.asset)
	mux.HandleFunc("GET /api/characters/{character}/asset/avatar.zip", a.asset)
	mux.HandleFunc("POST /api/sessions/{id}/avatar", a.avatar)
	mux.HandleFunc("POST /oac/v1/chat/completions", a.oac)
	mux.HandleFunc("GET /docs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><meta charset="utf-8"><title>Avatar Harness API</title><style>body{max-width:900px;margin:60px auto;background:#101317;color:#e8edf2;font:16px/1.8 system-ui}code{color:#7ce3b2}a{color:#7ce3b2}</style><h1>Avatar Harness · Go</h1><p>外部 FastAPI 是 provider 服务端的一种接入协议，Harness 本身由 Go 运行。</p><p><code>GET /api/state</code> 配置与角色<br><code>POST /api/sessions</code> 创建会话<br><code>POST /api/sessions/{id}/turns</code> 文本输入<br><code>POST /api/sessions/{id}/audio</code> ASR 输入<br><code>GET /api/sessions/{id}/events</code> SSE 事件<br><code>POST /api/sessions/{id}/avatar</code> 获取展示描述<br><code>POST /v1/capabilities/{name}/invoke</code> 能力调用<br><code>POST /v1/capabilities/{name}/stream</code> NDJSON 流<br><code>POST /oac/v1/chat/completions</code> OpenAvatarChat 桥接</p><p>请求：<code>{"input":{},"context":{}}</code><br>返回：<code>{"status":"ok","output":{}}</code></p><a href="/v1/capabilities">已注册能力</a> · <a href="/">返回工作台</a>`)
	})
	assets, _ := fs.Sub(web.Files, "dist")
	mux.Handle("/", http.FileServer(http.FS(assets)))
	a.Handler = mux
}
func send(w http.ResponseWriter, e store.Event) error {
	raw, _ := json.Marshal(e)
	controller := http.NewResponseController(w)
	_ = controller.SetWriteDeadline(time.Now().Add(5 * time.Second))
	defer controller.SetWriteDeadline(time.Time{})
	if _, err := fmt.Fprintf(w, "id: %d\ndata: %s\n\n", e.Seq, raw); err != nil {
		return err
	}
	return controller.Flush()
}
func (a *App) events(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.session(w, r); !ok {
		return
	}
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	last, _ := strconv.ParseInt(r.Header.Get("Last-Event-ID"), 10, 64)
	if last > after {
		after = last
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	history, queue, unsubscribe := a.Runtime.Store.Subscribe(r.PathValue("id"))
	defer unsubscribe()
	for _, e := range history {
		if e.Seq > after {
			if send(w, e) != nil {
				return
			}
			after = e.Seq
		}
	}
	_ = http.NewResponseController(w).Flush()
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-a.done.Done():
			return
		case <-r.Context().Done():
			return
		case e, ok := <-queue:
			if !ok {
				fmt.Fprint(w, "event: resync\ndata: {\"reason\":\"client_backpressure\"}\n\n")
				_ = http.NewResponseController(w).Flush()
				return
			}
			if e.Seq > after {
				if send(w, e) != nil {
					return
				}
				after = e.Seq
			}
		case <-ticker.C:
			if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
				return
			}
			if http.NewResponseController(w).Flush() != nil {
				return
			}
		}
	}
}
func (a *App) audio(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.session(w, r); !ok {
		return
	}
	var body provider.Request
	if !decode(w, r, &body) {
		return
	}
	sid := r.PathValue("id")
	a.Runtime.Cancel(sid, "新的语音输入")
	started := time.Now()
	a.Runtime.Store.Emit(sid, "", "module.status", map[string]any{"module": "asr", "state": "running"})
	result := a.Runtime.Bus.Invoke(r.Context(), "asr", body.Input, map[string]any{"session_id": sid, "request_id": store.ID()})
	if r.Context().Err() != nil {
		return
	}
	state := "configured"
	if result.Status != "ok" {
		state = result.Status
	}
	a.Runtime.Store.Emit(sid, "", "module.status", map[string]any{"module": "asr", "state": state, "elapsed_ms": float64(time.Since(started).Microseconds()) / 1000, "message": result.Error})
	if result.Status != "ok" {
		problem(w, 503, result.Error)
		return
	}
	text := strings.TrimSpace(provider.String(result.Output["text"]))
	if text == "" {
		problem(w, 422, "ASR 未返回有效文字")
		return
	}
	turn, err := a.Runtime.Start(sid, text, "voice", true, nil)
	if err != nil {
		problem(w, 422, err.Error())
		return
	}
	write(w, 202, map[string]any{"turn_id": turn.ID, "text": text})
}
func (a *App) asset(w http.ResponseWriter, r *http.Request) {
	ch, ok := a.Runtime.Character(r.PathValue("character"))
	if !ok || ch.AssetPath == "" {
		problem(w, 404, "角色未配置本地资产")
		return
	}
	ext := strings.ToLower(filepath.Ext(ch.AssetPath))
	if ext != ".glb" && ext != ".vrm" && !(ext == ".zip" && ch.Renderer.Kind == "lam") {
		problem(w, 422, "需要 GLB/VRM 或已配置 LAM 展示的 ZIP")
		return
	}
	w.Header().Set("Content-Type", "model/gltf-binary")
	if ext == ".zip" {
		w.Header().Set("Content-Type", "application/zip")
	}
	http.ServeFile(w, r, ch.AssetPath)
}
func (a *App) avatar(w http.ResponseWriter, r *http.Request) {
	session, ok := a.session(w, r)
	if !ok {
		return
	}
	character, _ := a.Runtime.Character(session.CharacterID)
	renderer := character.Renderer
	if renderer.Kind == "none" && a.Config.OACURL != "" {
		renderer = config.Renderer{Kind: "iframe", URL: a.Config.OACURL}
	}
	if a.Config.Providers["avatar"].Kind != "deferred" {
		a.Runtime.Store.Emit(session.ID, "", "module.status", map[string]any{"module": "avatar", "state": "running"})
		result := a.Runtime.Bus.Invoke(r.Context(), "avatar", map[string]any{"operation": "describe", "character_id": character.ID}, map[string]any{"session_id": session.ID})
		if result.Status != "ok" {
			a.Runtime.Store.Emit(session.ID, "", "module.status", map[string]any{"module": "avatar", "state": result.Status, "message": result.Error})
			problem(w, 502, result.Error)
			return
		}
		value := result.Output
		if nested, ok := value["renderer"].(map[string]any); ok {
			value = nested
		}
		raw, _ := json.Marshal(value)
		if err := json.Unmarshal(raw, &renderer); err != nil {
			problem(w, 502, "无效的形象展示描述")
			return
		}
		a.Runtime.Store.Emit(session.ID, "", "module.status", map[string]any{"module": "avatar", "state": "configured"})
	}
	switch renderer.Kind {
	case "none", "scene", "iframe", "video":
	default:
		problem(w, 502, "形象展示类型只支持 scene、iframe、video 或 none")
		return
	}
	write(w, 200, map[string]any{"renderer": renderer, "character_id": character.ID})
}
func (a *App) oac(w http.ResponseWriter, r *http.Request) {
	if config.State(a.Config.Providers["llm"])["configured"] != true {
		problem(w, 503, "先在终端配置 LLM provider")
		return
	}
	var body struct {
		Model    string           `json:"model"`
		Stream   bool             `json:"stream"`
		Messages []map[string]any `json:"messages"`
	}
	if !decode(w, r, &body) {
		return
	}
	text := ""
	for i := len(body.Messages) - 1; i >= 0; i-- {
		if body.Messages[i]["role"] == "user" {
			text = provider.String(body.Messages[i]["content"])
			break
		}
	}
	if strings.TrimSpace(text) == "" {
		problem(w, 422, "OAC 桥接接受 ASR 后的文字输入")
		return
	}
	_, characterID, found := strings.Cut(body.Model, "/")
	if !found {
		characterID = a.Config.Characters[0].ID
	}
	if _, ok := a.Runtime.Character(characterID); !ok {
		problem(w, 422, "角色不存在")
		return
	}
	session := a.Runtime.Store.Create(characterID)
	_, queue, unsubscribe := a.Runtime.Store.Subscribe(session.ID)
	defer unsubscribe()
	turn, err := a.Runtime.Start(session.ID, text, "text", false, body.Messages)
	if err != nil {
		problem(w, 422, err.Error())
		return
	}
	defer func() { a.Runtime.Cancel(session.ID, "OAC 消费端已关闭") }()
	if !body.Stream {
		select {
		case <-turn.Done:
		case <-r.Context().Done():
			return
		}
		answer := ""
		for _, e := range a.Runtime.Store.Events(session.ID, 0) {
			if e.Type == "assistant.message" {
				answer = provider.String(e.Data["text"])
			}
		}
		if answer == "" {
			problem(w, 502, "Harness 未返回有效回答")
			return
		}
		write(w, 200, map[string]any{"id": turn.ID, "object": "chat.completion", "model": body.Model, "choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": answer}, "finish_reason": "stop"}}})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	delivered := false
	chunk := func(text string, finished bool) error {
		delta := map[string]any{}
		var finish any
		if text != "" {
			delta["content"] = text
		}
		if finished {
			finish = "stop"
		}
		raw, _ := json.Marshal(map[string]any{"id": turn.ID, "object": "chat.completion.chunk", "created": time.Now().Unix(), "model": body.Model, "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}}})
		if _, err := fmt.Fprintf(w, "data: %s\n\n", raw); err != nil {
			return err
		}
		return http.NewResponseController(w).Flush()
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case <-a.done.Done():
			return
		case e, ok := <-queue:
			if !ok {
				return
			}
			switch e.Type {
			case "assistant.delta":
				if e.Data["draft"] == false {
					delivered = true
					if chunk(provider.String(e.Data["text"]), false) != nil {
						return
					}
				}
			case "assistant.message":
				if !delivered {
					delivered = true
					if chunk(provider.String(e.Data["text"]), false) != nil {
						return
					}
				}
			case "turn.error":
				raw, _ := json.Marshal(map[string]any{"error": map[string]any{"message": e.Data["message"]}})
				fmt.Fprintf(w, "data: %s\n\n", raw)
			case "turn.finished", "turn.cancelled":
				_ = chunk("", true)
				fmt.Fprint(w, "data: [DONE]\n\n")
				_ = http.NewResponseController(w).Flush()
				return
			}
		}
	}
}
