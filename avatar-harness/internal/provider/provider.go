package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"avatarharness/internal/config"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

type Request struct {
	Capability string         `json:"capability"`
	Input      map[string]any `json:"input"`
	Context    map[string]any `json:"context"`
}
type Result struct {
	Status    string         `json:"status"`
	Output    map[string]any `json:"output"`
	Error     string         `json:"error,omitempty"`
	ElapsedMS float64        `json:"elapsed_ms,omitempty"`
}
type Emit func(map[string]any) error
type Internal func(context.Context, Request, Emit) (Result, error)
type Bus struct {
	Config   *config.Config
	Client   *http.Client
	mu       sync.RWMutex
	internal map[string]Internal
	schemas  map[string]*jsonschema.Schema
}

func New(c *config.Config, client *http.Client) (*Bus, error) {
	if client == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.Proxy = nil
		transport.MaxIdleConns = 128
		transport.MaxIdleConnsPerHost = 32
		client = &http.Client{Transport: transport}
	}
	b := &Bus{Config: c, Client: client, internal: map[string]Internal{}, schemas: map[string]*jsonschema.Schema{}}
	b.Register("get_current_time", func(ctx context.Context, r Request, e Emit) (Result, error) {
		zone, _ := r.Input["timezone"].(string)
		if zone == "" {
			zone = "Asia/Shanghai"
		}
		loc, err := time.LoadLocation(zone)
		if err != nil {
			return Result{}, err
		}
		now := time.Now().In(loc)
		return OK(map[string]any{"datetime": now.Format(time.RFC3339Nano), "timezone": zone, "weekday": int(now.Weekday())}), nil
	})
	b.Register("list_characters", func(ctx context.Context, r Request, e Emit) (Result, error) {
		items := []map[string]any{}
		for _, ch := range c.Characters {
			items = append(items, map[string]any{"id": ch.ID, "name": ch.Name, "asset_configured": ch.AssetPath != "" || ch.Renderer.AssetURL != "" || ch.Renderer.URL != ""})
		}
		return OK(map[string]any{"characters": items}), nil
	})
	for _, t := range c.Tools {
		compiler := jsonschema.NewCompiler()
		name := "urn:avatar-harness:tool:" + t.Name
		if err := compiler.AddResource(name, t.Parameters); err != nil {
			return nil, err
		}
		schema, err := compiler.Compile(name)
		if err != nil {
			return nil, fmt.Errorf("工具 %s 的 schema: %w", t.Name, err)
		}
		b.schemas[t.Name] = schema
	}
	return b, nil
}
func OK(output map[string]any) Result {
	if output == nil {
		output = map[string]any{}
	}
	return Result{Status: "ok", Output: output}
}
func Fail(status string, err error) Result {
	return Result{Status: status, Output: map[string]any{}, Error: err.Error()}
}
func (b *Bus) Register(name string, fn Internal) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.internal[name] = fn
}
func (b *Bus) Spec() []map[string]any {
	out := []map[string]any{}
	for _, t := range b.Config.Tools {
		out = append(out, map[string]any{"type": "function", "function": map[string]any{"name": t.Name, "description": t.Description, "parameters": t.Parameters}})
	}
	return out
}
func (b *Bus) Invoke(ctx context.Context, name string, input, metadata map[string]any) (result Result) {
	started := time.Now()
	defer func() { result.ElapsedMS = float64(time.Since(started).Microseconds()) / 1000 }()
	p, ok := b.Config.Providers[name]
	if !ok {
		found := false
		for _, t := range b.Config.Tools {
			if t.Name == name {
				found = true
				p = config.Provider{Kind: t.Kind, BaseURL: t.BaseURL, APIKeyEnv: t.APIKeyEnv, Factory: t.Factory, TimeoutMS: t.TimeoutMS}
				if p.Factory == "" {
					p.Factory = name
				}
				break
			}
		}
		if !found {
			return Fail("error", fmt.Errorf("工具不存在：%s", name))
		}
		if err := b.schemas[name].Validate(input); err != nil {
			return Fail("error", fmt.Errorf("工具参数校验失败：%w", err))
		}
	}
	err := b.call(ctx, name, p, input, metadata, nil, &result)
	if err != nil {
		status := "error"
		if errors.Is(err, context.DeadlineExceeded) {
			status = "timeout"
		}
		if errors.Is(err, context.Canceled) {
			status = "cancelled"
		}
		return Fail(status, err)
	}
	return result
}
func (b *Bus) Stream(ctx context.Context, name string, input, metadata map[string]any, emit Emit) error {
	p, ok := b.Config.Providers[name]
	if !ok {
		return fmt.Errorf("流式 provider 不存在：%s", name)
	}
	if p.Kind == "openai" {
		return b.LLM(ctx, input, metadata, emit)
	}
	if !p.Streaming || p.Kind == "deferred" {
		r := b.Invoke(ctx, name, input, metadata)
		return emit(map[string]any{"type": "result", "status": r.Status, "output": r.Output, "error": r.Error})
	}
	var result Result
	return b.call(ctx, name, p, input, metadata, emit, &result)
}
func (b *Bus) call(ctx context.Context, name string, p config.Provider, input, metadata map[string]any, emit Emit, result *Result) error {
	if p.Kind == "deferred" {
		*result = Fail("deferred", fmt.Errorf("%s 尚未配置，请在终端运行 ./start configure %s", name, name))
		return nil
	}
	if p.TimeoutMS <= 0 {
		p.TimeoutMS = 15000
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(p.TimeoutMS)*time.Millisecond)
	defer cancel()
	values := map[string]any{}
	for k, v := range p.Parameters {
		values[k] = v
	}
	for k, v := range input {
		values[k] = v
	}
	if p.Model != "" {
		if _, ok := values["model"]; !ok {
			values["model"] = p.Model
		}
	}
	if p.Kind == "bailian" {
		var err error
		*result, err = b.bailian(ctx, name, p, values, emit)
		return err
	}
	if p.Kind == "openai" {
		text := ""
		calls := []any{}
		err := b.LLM(ctx, values, metadata, func(e map[string]any) error {
			if e["type"] == "text" {
				text += String(e["text"])
			}
			if e["type"] == "done" {
				calls, _ = e["tool_calls"].([]any)
			}
			return nil
		})
		*result = OK(map[string]any{"content": text, "tool_calls": calls})
		return err
	}
	r := Request{name, values, metadata}
	if p.Kind == "internal" {
		b.mu.RLock()
		fn := b.internal[p.Factory]
		b.mu.RUnlock()
		if fn == nil {
			return fmt.Errorf("未注册内部 Go 函数 %q；外部 Python 模型请选择 FastAPI", p.Factory)
		}
		type answer struct {
			r Result
			e error
		}
		done := make(chan answer, 1)
		var emitMu sync.Mutex
		finished := false
		defer func() { emitMu.Lock(); finished = true; emitMu.Unlock() }()
		go func() {
			v, err := fn(ctx, r, func(e map[string]any) error {
				emitMu.Lock()
				defer emitMu.Unlock()
				if finished {
					return context.Canceled
				}
				if emit == nil {
					return fmt.Errorf("同步调用不接受流式事件")
				}
				if err := ctx.Err(); err != nil {
					return err
				}
				return emit(e)
			})
			done <- answer{v, err}
		}()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case answer := <-done:
			*result = answer.r
			return answer.e
		}
	}
	path := "/invoke"
	if emit != nil {
		path = "/stream"
	}
	res, err := b.post(ctx, p, path, r)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if emit != nil {
		return readEvents(res.Body, emit)
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 32<<20))
	if err != nil {
		return err
	}
	return decodeResult(raw, result)
}
func (b *Bus) post(ctx context.Context, p config.Provider, path string, body any) (*http.Response, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(p.BaseURL, "/")+path, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream, application/x-ndjson")
	if p.APIKeyEnv != "" {
		key := os.Getenv(p.APIKeyEnv)
		if key == "" {
			return nil, fmt.Errorf("缺少密钥环境变量 %s", p.APIKeyEnv)
		}
		req.Header.Set("Authorization", "Bearer "+key)
	}
	res, err := b.Client.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		res.Body.Close()
		return nil, fmt.Errorf("外部 %s 返回 HTTP %d", p.Kind, res.StatusCode)
	}
	return res, nil
}
func decodeResult(raw []byte, r *Result) error {
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return err
	}
	if obj == nil {
		return fmt.Errorf("provider 必须返回 JSON 对象")
	}
	if _, ok := obj["status"]; !ok {
		if _, ok = obj["output"]; !ok {
			*r = OK(obj)
			return nil
		}
	}
	if err := json.Unmarshal(raw, r); err != nil {
		return err
	}
	if r.Status == "" {
		r.Status = "ok"
	}
	if r.Output == nil {
		r.Output = map[string]any{}
	}
	switch r.Status {
	case "ok", "deferred", "error", "timeout", "cancelled":
		return nil
	default:
		return fmt.Errorf("provider 返回未知 status %q", r.Status)
	}
}
func readEvents(reader io.Reader, emit Emit) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 8192), 32<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") || strings.HasPrefix(line, "event:") || strings.HasPrefix(line, "id:") {
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if line == "[DONE]" {
			break
		}
		var e map[string]any
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			return err
		}
		if e == nil {
			return fmt.Errorf("流式事件必须是 JSON 对象")
		}
		if err := emit(e); err != nil {
			return err
		}
	}
	return scanner.Err()
}
func (b *Bus) LLM(ctx context.Context, input, metadata map[string]any, emit Emit) error {
	p := b.Config.Providers["llm"]
	if config.State(p)["configured"] != true {
		return fmt.Errorf("LLM 尚未配置，请在终端运行 ./start configure llm")
	}
	if p.Kind != "openai" {
		if !p.Streaming {
			r := b.Invoke(ctx, "llm", input, metadata)
			if r.Status != "ok" {
				return errors.New(r.Error)
			}
			if err := emit(map[string]any{"type": "text", "text": String(r.Output["content"])}); err != nil {
				return err
			}
			return emit(map[string]any{"type": "done", "tool_calls": r.Output["tool_calls"]})
		}
		done := false
		err := b.Stream(ctx, "llm", input, metadata, func(e map[string]any) error {
			if e["type"] == "error" || e["status"] == "error" {
				return errors.New(String(e["error"]))
			}
			if e["type"] == "done" {
				done = true
			}
			return emit(e)
		})
		if err != nil {
			return err
		}
		if !done {
			return fmt.Errorf("LLM 流缺少完整 done 事件")
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(p.TimeoutMS)*time.Millisecond)
	defer cancel()
	body := map[string]any{}
	for k, v := range p.Parameters {
		body[k] = v
	}
	body["model"] = p.Model
	body["messages"] = input["messages"]
	body["stream"] = true
	if format, ok := input["response_format"]; ok {
		body["response_format"] = format
	}
	if tools, ok := input["tools"].([]map[string]any); ok && len(tools) > 0 {
		body["tools"] = tools
	} else if tools, ok := input["tools"].([]any); ok && len(tools) > 0 {
		body["tools"] = tools
	} else {
		delete(body, "tools")
	}
	res, err := b.post(ctx, p, "/chat/completions", body)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	calls := map[int]map[string]any{}
	err = readEvents(res.Body, func(e map[string]any) error {
		choices, _ := e["choices"].([]any)
		if len(choices) == 0 {
			if e["error"] != nil {
				return fmt.Errorf("模型流返回错误")
			}
			return nil
		}
		choice, _ := choices[0].(map[string]any)
		delta, _ := choice["delta"].(map[string]any)
		if text := String(delta["content"]); text != "" {
			if err := emit(map[string]any{"type": "text", "text": text}); err != nil {
				return err
			}
		}
		parts, _ := delta["tool_calls"].([]any)
		for _, part := range parts {
			call, _ := part.(map[string]any)
			index := int(Number(call["index"]))
			current := calls[index]
			if current == nil {
				current = map[string]any{"id": "", "type": "function", "function": map[string]any{"name": "", "arguments": ""}}
				calls[index] = current
			}
			if id := String(call["id"]); id != "" {
				current["id"] = id
			}
			fn, _ := call["function"].(map[string]any)
			dst := current["function"].(map[string]any)
			dst["name"] = String(dst["name"]) + String(fn["name"])
			dst["arguments"] = String(dst["arguments"]) + String(fn["arguments"])
			if err := emit(map[string]any{"type": "tool.delta", "name": dst["name"]}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	output := []any{}
	for index := 0; len(output) < len(calls); index++ {
		if call, ok := calls[index]; ok {
			output = append(output, call)
		}
		if index > 1024 {
			return fmt.Errorf("工具索引过大")
		}
	}
	return emit(map[string]any{"type": "done", "tool_calls": output})
}
func String(v any) string { s, _ := v.(string); return s }
func Number(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case json.Number:
		f, _ := x.Float64()
		return f
	}
	return 0
}

func Calls(value any) ([]any, error) {
	if value == nil {
		return nil, nil
	}
	switch calls := value.(type) {
	case []any:
		return calls, nil
	case []map[string]any:
		out := make([]any, len(calls))
		for i, call := range calls {
			out[i] = call
		}
		return out, nil
	}
	return nil, fmt.Errorf("tool_calls 必须是调用对象数组")
}
