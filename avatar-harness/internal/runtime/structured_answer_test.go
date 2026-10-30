package runtime

import (
	"context"
	"encoding/base64"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"avatarharness/internal/config"
	"avatarharness/internal/provider"
	"avatarharness/internal/store"
)

func TestStructuredAnswerAllChunkBoundaries(t *testing.T) {
	raw := `{"emotion":"happy","intensity":0.8,"speech":"<speak/>你好，\"欢迎\"！\n路径 C:\\demo \u4f60\u597d \uD83D\uDE00"}`
	want := "<speak/>你好，\"欢迎\"！\n路径 C:\\demo 你好 😀"
	for split := 0; split <= len(raw); split++ {
		parser := StructuredAnswer{Enabled: true}
		text, controls := "", 0
		for _, chunk := range []string{raw[:split], raw[split:]} {
			part, expression, err := parser.Feed(chunk, false)
			if err != nil {
				t.Fatalf("split %d: %v", split, err)
			}
			text += part
			if expression != nil {
				controls++
				if expression.Style != "happy" || expression.Intensity != .8 {
					t.Fatal(expression)
				}
			}
		}
		part, _, err := parser.Feed("", true)
		if err != nil || text+part != want || controls != 1 {
			t.Fatalf("split %d: %q, %d, %v", split, text+part, controls, err)
		}
	}
}

func TestStructuredAnswerSevenBasicEmotions(t *testing.T) {
	for _, style := range []string{"neutral", "happy", "sad", "angry", "fearful", "surprised", "disgusted"} {
		p := StructuredAnswer{Enabled: true}
		text, expression, err := p.Feed(`{"emotion":"`+style+`","intensity":0.7,"speech":"<speak/>你好。"}`, true)
		if err != nil || expression == nil || expression.Style != style || expression.Intensity != .7 || text != "<speak/>你好。" {
			t.Fatal(style, text, expression, err)
		}
		if !strings.Contains(expressionPrompt, style) {
			t.Fatal("emotion missing from LLM contract", style)
		}
	}
}

func TestStructuredAnswerByteStreamAndBadMetadata(t *testing.T) {
	for _, emotion := range []string{`"concerned"`, `"unknown"`} {
		p := StructuredAnswer{Enabled: true}
		text, controls := "", 0
		raw := `{"emotion":` + emotion + `,"intensity":0.6,"speech":"<speak/>我理解你的担心。"}`
		for i := range len(raw) {
			part, control, err := p.Feed(raw[i:i+1], false)
			if err != nil {
				t.Fatalf("byte %d: %v", i, err)
			}
			text += part
			if control != nil {
				controls++
			}
		}
		_, _, err := p.Feed("", true)
		if err != nil || text != "<speak/>我理解你的担心。" {
			t.Fatal(text, err)
		}
		if (emotion == `"concerned"`) != (controls == 1) {
			t.Fatal("invalid style reached renderer")
		}
	}
}

func TestStructuredAnswerRejectsIncompleteJSONAndPreservesLegacyText(t *testing.T) {
	p := StructuredAnswer{Enabled: true}
	_, _, _ = p.Feed(`{"emotion":"happy","speech":"<speak/>你好`, false)
	if _, _, err := p.Feed("", true); err == nil {
		t.Fatal("incomplete JSON accepted")
	}
	p = StructuredAnswer{Enabled: true}
	text, control, err := p.Feed("<speak/>旧 provider 的正文", true)
	if err != nil || control != nil || text != "<speak/>旧 provider 的正文" {
		t.Fatal(text, control, err)
	}
}

func TestStructuredReplyStartsTTSBeforeJSONEndsWithoutExtraLLM(t *testing.T) {
	c := config.Default()
	c.Characters[0].Renderer = config.Renderer{Kind: "lam", AssetURL: "/models/lam/avatar.zip"}
	c.Providers["llm"] = config.Provider{Kind: "internal", Factory: "structured", Streaming: true, TimeoutMS: 2000}
	c.Providers["tts"] = config.Provider{Kind: "internal", Factory: "speech-spy", TimeoutMS: 1000}
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	b, err := provider.New(&c, nil)
	if err != nil {
		t.Fatal(err)
	}
	var llmCalls atomic.Int32
	ttsStarted := make(chan string, 1)
	release := make(chan struct{})
	b.Register("structured", func(ctx context.Context, req provider.Request, emit provider.Emit) (provider.Result, error) {
		llmCalls.Add(1)
		if err := emit(map[string]any{"type": "text", "text": `{"emotion":"happy","intensity":0.8,"speech":"<speak/>太棒了，恭喜你顺利通过。`}); err != nil {
			return provider.OK(nil), err
		}
		select {
		case <-release:
		case <-ctx.Done():
			return provider.OK(nil), ctx.Err()
		}
		if err := emit(map[string]any{"type": "text", "text": `"}`}); err != nil {
			return provider.OK(nil), err
		}
		return provider.OK(nil), emit(map[string]any{"type": "done", "tool_calls": []any{}})
	})
	b.Register("speech-spy", func(ctx context.Context, req provider.Request, emit provider.Emit) (provider.Result, error) {
		intent, _ := req.Input["expression_intent"].(map[string]any)
		if intent["style"] != "happy" || intent["intensity"] != .8 {
			t.Error("TTS did not receive committed face emotion", intent)
		}
		select {
		case ttsStarted <- provider.String(req.Input["text"]):
		default:
		}
		return provider.OK(map[string]any{"audio_base64": base64.StdEncoding.EncodeToString(make([]byte, 4800)), "format": "pcm_s16le", "sample_rate": 24000, "channels": 1}), nil
	})
	r := New(&c, s, b)
	defer r.Shutdown()
	sid := s.Create(c.Characters[0].ID).ID
	turn, err := r.StartVoice(sid, "我考试通过了", "structured-input", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	select {
	case spoken := <-ttsStarted:
		if spoken != "太棒了，恭喜你顺利通过。" {
			t.Fatal("metadata or marker leaked to TTS", spoken)
		}
	case <-time.After(time.Second):
		close(release)
		t.Fatal("TTS waited for complete JSON")
	}
	found := false
	timing := 0
	for _, event := range s.Events(sid, 0) {
		if event.Type == "pipeline.timing" && event.Data["stage"] == "llm.first_speech" {
			timing++
			if provider.Number(event.Data["elapsed_ms"]) < 0 {
				t.Fatal("invalid first-speech timing", event.Data)
			}
		}
		if event.Type == "pipeline.expression" && event.Data["style"] == "happy" {
			found = true
		}
	}
	if !found || timing != 1 {
		close(release)
		t.Fatal("expression or first-speech timing did not arrive with streamed reply", found, timing)
	}
	close(release)
	<-turn.Done
	if llmCalls.Load() != 1 {
		t.Fatal("expression added an LLM round trip", llmCalls.Load())
	}
	for _, event := range s.Events(sid, 0) {
		if event.Type == "turn.error" {
			t.Fatal(event.Data)
		}
		if event.Type == "assistant.message" && strings.Contains(provider.String(event.Data["text"]), `"emotion"`) {
			t.Fatal("JSON leaked to log", event.Data)
		}
	}
}

func TestStructuredReplyPreservesNativeToolRoundAndDiscardsPrelude(t *testing.T) {
	c := config.Default()
	c.Characters[0].Renderer = config.Renderer{Kind: "lam", AssetURL: "/models/lam/avatar.zip"}
	c.Providers["llm"] = config.Provider{Kind: "internal", Factory: "structured-tools", TimeoutMS: 1000}
	s, _ := store.Open(t.TempDir())
	defer s.Close()
	b, _ := provider.New(&c, nil)
	var count atomic.Int32
	b.Register("structured-tools", func(ctx context.Context, req provider.Request, emit provider.Emit) (provider.Result, error) {
		count.Add(1)
		if tools, _ := req.Input["tools"].([]map[string]any); len(tools) > 0 {
			return provider.OK(map[string]any{"content": `{"emotion":"happy","speech":"尚未提交的工具前言`, "tool_calls": []any{map[string]any{"id": "clock", "type": "function", "function": map[string]any{"name": "get_current_time", "arguments": "{}"}}}}), nil
		}
		if req.Input["response_format"] == nil {
			t.Error("final JSON constraint missing")
		}
		return provider.OK(map[string]any{"content": `{"emotion":"focused","intensity":0.5,"speech":"<speak/>已查询实际时间。"}`}), nil
	})
	r := New(&c, s, b)
	defer r.Shutdown()
	sid := s.Create(c.Characters[0].ID).ID
	turn, _ := r.Start(sid, "查询时间", "text", false, nil)
	<-turn.Done
	if count.Load() != 2 {
		t.Fatal("unexpected tool or expression round", count.Load())
	}
	controls, toolFinished := 0, false
	for _, event := range s.Events(sid, 0) {
		switch event.Type {
		case "turn.error":
			t.Fatal(event.Data)
		case "tool.finished":
			toolFinished = event.Data["status"] == "ok"
		case "pipeline.expression":
			controls++
			if !toolFinished || event.Data["style"] != "focused" {
				t.Fatal("uncommitted expression escaped", event.Data)
			}
		case "assistant.message":
			if event.Data["text"] != "已查询实际时间。" {
				t.Fatal(event.Data)
			}
		}
	}
	if controls != 1 {
		t.Fatal("missing final expression", controls)
	}
}
