package provider

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"avatarharness/internal/config"
	"github.com/gorilla/websocket"
)

func audioTTSFixture(t *testing.T, serve func(*websocket.Conn)) *Bus {
	t.Helper()
	t.Setenv(config.BailianKeyEnv, "audio-protocol-fixture")
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer audio-protocol-fixture" || r.URL.Path != "/api-ws/v1/inference" || r.URL.Query().Has("model") {
			t.Error("wrong Qwen-Audio endpoint, query or authentication")
			http.Error(w, "invalid", 400)
			return
		}
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		serve(conn)
	}))
	t.Cleanup(remote.Close)
	c := config.Default()
	_ = c.UseBailian("")
	p := c.Providers["tts"]
	p.BaseURL = "ws" + strings.TrimPrefix(remote.URL, "http") + "/api-ws/v1/inference?model=remove-me"
	p.TimeoutMS = 2000
	c.Providers["tts"] = p
	b, err := New(&c, nil)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func acceptAudioTask(t *testing.T, conn *websocket.Conn) (string, map[string]any) {
	t.Helper()
	var event map[string]any
	if err := conn.ReadJSON(&event); err != nil {
		t.Error(err)
		return "", nil
	}
	header, _ := event["header"].(map[string]any)
	id := String(header["task_id"])
	payload, _ := event["payload"].(map[string]any)
	if header["action"] != "run-task" || header["streaming"] != "duplex" || len(id) != 36 || id[14] != '4' || payload["model"] != config.BailianTTSModel {
		t.Error("invalid run-task", event)
	}
	_ = conn.WriteJSON(map[string]any{"header": map[string]any{"event": "task-started", "task_id": id}})
	for _, action := range []string{"continue-task", "finish-task"} {
		var next map[string]any
		if err := conn.ReadJSON(&next); err != nil {
			t.Error(err)
			return "", nil
		}
		h, _ := next["header"].(map[string]any)
		if h["action"] != action || h["task_id"] != id {
			t.Error("invalid task continuation", next)
		}
	}
	params, _ := payload["parameters"].(map[string]any)
	return id, params
}

func finishAudioTask(conn *websocket.Conn, id string) {
	_ = conn.WriteJSON(map[string]any{"header": map[string]any{"event": "task-finished", "task_id": id}})
}

func TestQwenAudioTTSStreamsPCMBeforeFinishAndPreservesOddFrames(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		t.Run(map[bool]string{true: "stream", false: "invoke"}[streaming], func(t *testing.T) {
			release := make(chan struct{})
			b := audioTTSFixture(t, func(conn *websocket.Conn) {
				id, params := acceptAudioTask(t, conn)
				if params["format"] != "pcm" || !strings.Contains(String(params["instruction"]), "惊讶") {
					t.Error(params)
				}
				_ = conn.WriteMessage(websocket.BinaryMessage, []byte{1, 0, 2})
				if streaming {
					select {
					case <-release:
					case <-time.After(time.Second):
						t.Error("audio buffered until task-finished")
					}
				}
				_ = conn.WriteMessage(websocket.BinaryMessage, []byte{0, 3, 0})
				finishAudioTask(conn, id)
			})
			input := map[string]any{"text": "竟然是这样。", "expression_intent": map[string]any{"style": "surprised", "intensity": .7}}
			var pcm []byte
			done := false
			if streaming {
				err := b.Stream(context.Background(), "tts", input, nil, func(e map[string]any) error {
					if e["type"] == "audio" {
						output := e["output"].(map[string]any)
						raw, _ := base64.StdEncoding.DecodeString(String(output["audio_base64"]))
						if len(pcm) == 0 {
							close(release)
						}
						pcm = append(pcm, raw...)
					}
					done = done || e["type"] == "done"
					return nil
				})
				if err != nil || !done {
					t.Fatal(err, done)
				}
			} else {
				result := b.Invoke(context.Background(), "tts", input, nil)
				if result.Status != "ok" {
					t.Fatal(result)
				}
				pcm, _ = base64.StdEncoding.DecodeString(String(result.Output["audio_base64"]))
			}
			if !bytes.Equal(pcm, []byte{1, 0, 2, 0, 3, 0}) {
				t.Fatal("PCM corruption", pcm)
			}
		})
	}
}

func TestQwenAudioTTSPreconnectDefersEmotionAndReusesConnection(t *testing.T) {
	var connections atomic.Int32
	ids := make(chan string, 2)
	b := audioTTSFixture(t, func(conn *websocket.Conn) {
		connections.Add(1)
		for _, keyword := range []string{"欣喜", "低落"} {
			id, params := acceptAudioTask(t, conn)
			ids <- id
			if !strings.Contains(String(params["instruction"]), keyword) {
				t.Error("emotion lost after preconnect", params)
			}
			_ = conn.WriteMessage(websocket.BinaryMessage, []byte{1, 0})
			finishAudioTask(conn, id)
		}
	})
	s := b.NewTTSStream(context.Background(), nil, nil)
	defer s.Close()
	<-s.ready
	select {
	case <-ids:
		t.Fatal("task started before text and emotion were known")
	default:
	}
	for _, style := range []string{"happy", "sad"} {
		err := s.StreamWithInput("一句话。", map[string]any{"expression_intent": map[string]any{"style": style, "intensity": .8}}, func(map[string]any) error { return nil })
		if err != nil {
			t.Fatal(err)
		}
	}
	if connections.Load() != 1 || <-ids == <-ids {
		t.Fatal("tasks did not reuse connection with fresh IDs")
	}
}

func TestQwenAudioTTSRejectsEmptyTruncatedFailedAndStaleResults(t *testing.T) {
	for _, mode := range []string{"empty", "odd", "failed", "stale", "truncated"} {
		t.Run(mode, func(t *testing.T) {
			b := audioTTSFixture(t, func(conn *websocket.Conn) {
				id, _ := acceptAudioTask(t, conn)
				switch mode {
				case "odd":
					_ = conn.WriteMessage(websocket.BinaryMessage, []byte{1})
				case "truncated":
					_ = conn.WriteMessage(websocket.BinaryMessage, []byte{1, 0})
					return
				case "failed":
					_ = conn.WriteJSON(map[string]any{"header": map[string]any{"event": "task-failed", "task_id": id, "error_code": "InvalidParameter", "error_message": "fixture"}})
					return
				case "stale":
					id = "previous-task"
				}
				finishAudioTask(conn, id)
			})
			result := b.Invoke(context.Background(), "tts", map[string]any{"text": "你好。"}, nil)
			if result.Status == "ok" {
				t.Fatal("invalid stream accepted", mode)
			}
		})
	}
}

func TestQwenAudioTTSCancelClosesActiveTask(t *testing.T) {
	started, closed := make(chan struct{}), make(chan struct{})
	b := audioTTSFixture(t, func(conn *websocket.Conn) {
		_, _ = acceptAudioTask(t, conn)
		close(started)
		_, _, _ = conn.ReadMessage()
		close(closed)
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := b.NewTTSStream(ctx, nil, nil)
	defer s.Close()
	done := make(chan error, 1)
	go func() { done <- s.Stream("你好。", func(map[string]any) error { return nil }) }()
	<-started
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancel blocked")
	}
	<-closed
	if err := s.Stream("不可重放。", func(map[string]any) error { t.Error("replayed cancelled task"); return nil }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestSpeechInstructionCoversSevenEmotionsAndIntensity(t *testing.T) {
	seen := map[string]bool{}
	neutral := speechInstruction(map[string]any{"expression_intent": map[string]any{"style": "neutral", "intensity": 1}})
	if !strings.Contains(neutral, "中性平稳") || strings.Contains(neutral, "表现强烈") {
		t.Fatal("neutral comparison was made emotional by maximum intensity", neutral)
	}
	for _, style := range []string{"neutral", "happy", "sad", "angry", "fearful", "surprised", "disgusted"} {
		instruction := speechInstruction(map[string]any{"expression_intent": map[string]any{"style": style, "intensity": .7}})
		if seen[instruction] {
			t.Fatal("distinct emotion shares instruction", style)
		}
		seen[instruction] = true
	}
	for _, intensity := range []float64{0, .2, 1} {
		p, _, err := qwenAudioParameters(config.Provider{}, map[string]any{"expression_intent": map[string]any{"style": "angry", "intensity": intensity}})
		instruction := String(p["instruction"])
		if err != nil || intensity == 0 && strings.Contains(instruction, "愤怒") || intensity == .2 && !strings.Contains(instruction, "轻微") || intensity == 1 && !strings.Contains(instruction, "明显") {
			t.Fatal(intensity, instruction, err)
		}
	}
	for _, sample := range []struct {
		intensity float64
		degree    string
	}{{.7, "表现明显"}, {.9, "表现强烈"}} {
		instruction := speechInstruction(map[string]any{"expression_intent": map[string]any{"style": "happy", "intensity": sample.intensity}})
		if !strings.Contains(instruction, sample.degree) || strings.Contains(instruction, "克制") {
			t.Fatal("expressive affect was damped in TTS", instruction)
		}
	}
}

func TestTheatricalSpeechIsExplicitAndKeepsNeutralBaseline(t *testing.T) {
	for _, style := range []string{"happy", "sad", "angry", "fearful", "surprised", "disgusted"} {
		normal := speechInstruction(map[string]any{"expression_intent": map[string]any{"style": style, "intensity": 1}})
		acting := speechInstruction(map[string]any{"expression_intent": map[string]any{"style": style, "intensity": 1, "performance": "theatrical"}})
		if strings.Contains(normal, "戏剧化") || !strings.Contains(acting, "戏剧化、夸张") || !strings.Contains(acting, "严格照原文") {
			t.Fatal(style, normal, acting)
		}
	}
	neutral := speechInstruction(map[string]any{"expression_intent": map[string]any{"style": "neutral", "intensity": 1, "performance": "theatrical"}})
	if strings.Contains(neutral, "戏剧化") || !strings.Contains(neutral, "中性平稳") {
		t.Fatal("neutral baseline was exaggerated", neutral)
	}
}
