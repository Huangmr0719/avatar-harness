package provider

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func acceptReusableTTS(t *testing.T, conn *websocket.Conn) bool {
	t.Helper()
	event, err := receiveFixture(conn, "session.update")
	if err != nil {
		t.Error(err)
		return false
	}
	session := event["session"].(map[string]any)
	if session["mode"] != "commit" || session["voice"] != "Ethan" || session["response_format"] != "pcm" {
		t.Error(session)
	}
	_ = conn.WriteJSON(map[string]any{"type": "session.updated"})
	return true
}

func acceptTTSCommit(t *testing.T, conn *websocket.Conn) bool {
	t.Helper()
	for _, kind := range []string{"input_text_buffer.append", "input_text_buffer.commit"} {
		if _, err := receiveFixture(conn, kind); err != nil {
			t.Error(err)
			return false
		}
	}
	return true
}

func TestTTSStreamEmitsAudioBeforeResponseDoneAndReusesSession(t *testing.T) {
	release := make(chan struct{})
	b := speechFixture(t, "tts", func(conn *websocket.Conn) {
		if !acceptReusableTTS(t, conn) {
			return
		}
		for i := range 2 {
			if !acceptTTSCommit(t, conn) {
				return
			}
			_ = conn.WriteJSON(map[string]any{"type": "response.audio.delta", "delta": "AQACAA=="})
			if i == 0 {
				select {
				case <-release:
				case <-time.After(time.Second):
					t.Error("audio was held until response.done")
					return
				}
			}
			_ = conn.WriteJSON(map[string]any{"type": "response.audio.delta", "delta": "AwAEAA=="})
			_ = conn.WriteJSON(map[string]any{"type": "response.done", "response": map[string]any{"status": "completed"}})
		}
		_, _, _ = conn.ReadMessage()
	})
	stream := b.NewTTSStream(context.Background(), map[string]any{"voice": "Ethan"}, nil)
	defer stream.Close()
	for i := range 2 {
		packets, done := 0, false
		if err := stream.Stream("测试连续分段。", func(event map[string]any) error {
			switch event["type"] {
			case "progress":
				if event["reused"] != (i > 0) {
					t.Error("connection reuse was not reported", event)
				}
			case "audio":
				packets++
				if i == 0 && packets == 1 {
					close(release)
				}
			case "done":
				done = true
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if packets != 2 || !done {
			t.Fatal("response boundaries were not preserved", i, packets, done)
		}
	}
}

func TestTTSStreamCancellationClosesPreparingIdleAndActiveConnections(t *testing.T) {
	for _, phase := range []string{"prepare", "idle", "active"} {
		t.Run(phase, func(t *testing.T) {
			started, closed := make(chan struct{}), make(chan struct{})
			b := speechFixture(t, "tts", func(conn *websocket.Conn) {
				if phase == "prepare" {
					if _, err := receiveFixture(conn, "session.update"); err != nil {
						t.Error(err)
						return
					}
				} else if !acceptReusableTTS(t, conn) {
					return
				}
				if phase == "active" {
					if !acceptTTSCommit(t, conn) {
						return
					}
					_ = conn.WriteJSON(map[string]any{"type": "response.audio.delta", "delta": "AQACAA=="})
				}
				close(started)
				_, _, _ = conn.ReadMessage()
				close(closed)
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			stream := b.NewTTSStream(ctx, map[string]any{"voice": "Ethan"}, nil)
			defer stream.Close()
			result := make(chan error, 1)
			if phase == "active" {
				go func() { result <- stream.Stream("正在生成的分段。", func(map[string]any) error { return nil }) }()
			}
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("fixture did not reach", phase)
			}
			cancel()
			stream.Close()
			select {
			case <-closed:
			case <-time.After(time.Second):
				t.Fatal("connection survived cancellation during", phase)
			}
			if phase == "active" {
				select {
				case err := <-result:
					if !errors.Is(err, context.Canceled) {
						t.Fatal(err)
					}
				case <-time.After(time.Second):
					t.Fatal("active segment did not stop")
				}
			}
		})
	}
}

func TestTTSStreamRejectsIncompleteResponsesWithoutReplayingText(t *testing.T) {
	for _, mode := range []string{"no-audio", "truncated", "failed", "session-finished"} {
		t.Run(mode, func(t *testing.T) {
			b := speechFixture(t, "tts", func(conn *websocket.Conn) {
				if !acceptReusableTTS(t, conn) || !acceptTTSCommit(t, conn) {
					return
				}
				switch mode {
				case "no-audio":
					_ = conn.WriteJSON(map[string]any{"type": "response.done", "response": map[string]any{"status": "completed"}})
				case "truncated":
					_ = conn.WriteJSON(map[string]any{"type": "response.audio.delta", "delta": "AQACAA=="})
				case "failed":
					_ = conn.WriteJSON(map[string]any{"type": "response.done", "response": map[string]any{"status": "failed"}})
				case "session-finished":
					_ = conn.WriteJSON(map[string]any{"type": "session.finished"})
				}
			})
			stream := b.NewTTSStream(context.Background(), map[string]any{"voice": "Ethan"}, nil)
			defer stream.Close()
			done := false
			emit := func(event map[string]any) error { done = done || event["type"] == "done"; return nil }
			first := stream.Stream("不能重复播放的文本。", emit)
			second := stream.Stream("后续文本。", emit)
			if first == nil || second == nil || first.Error() != second.Error() || done {
				t.Fatal("failed session was accepted or replayed", first, second, done)
			}
		})
	}
}

func TestTTSStreamPreparationValidatesSettings(t *testing.T) {
	b := speechFixture(t, "tts", func(conn *websocket.Conn) { t.Error("invalid settings opened a connection") })
	p := b.Config.Providers["tts"]
	p.Parameters = map[string]any{"sample_rate": 123}
	b.Config.Providers["tts"] = p
	stream := b.NewTTSStream(context.Background(), nil, nil)
	defer stream.Close()
	if err := stream.Stream("协议测试。", func(map[string]any) error { return nil }); err == nil || !strings.Contains(err.Error(), "采样率") {
		t.Fatal(err)
	}
}
