package provider

import (
	"context"
	"encoding/base64"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestLiveASRDeliversPartialBeforeCommit(t *testing.T) {
	bus := speechFixture(t, "asr", func(conn *websocket.Conn) {
		if _, err := receiveFixture(conn, "session.update"); err != nil {
			t.Error(err)
			return
		}
		_ = conn.WriteJSON(map[string]any{"type": "session.updated"})
		event, err := receiveFixture(conn, "input_audio_buffer.append")
		if err != nil {
			t.Error(err)
			return
		}
		raw, _ := base64.StdEncoding.DecodeString(String(event["audio"]))
		if len(raw) != 3200 {
			t.Error("audio was not forwarded while speaking")
		}
		_ = conn.WriteJSON(map[string]any{"type": "conversation.item.input_audio_transcription.text", "text": "边说", "stash": "边识别"})
		for _, kind := range []string{"input_audio_buffer.commit", "session.finish"} {
			if _, err = receiveFixture(conn, kind); err != nil {
				t.Error(err)
				return
			}
		}
		_ = conn.WriteJSON(map[string]any{"type": "conversation.item.input_audio_transcription.completed", "transcript": "边说边识别。"})
		_ = conn.WriteJSON(map[string]any{"type": "session.finished"})
	})
	frames := make(chan []byte, 2)
	ready, partial := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	final := ""
	go func() {
		done <- bus.LiveASR(context.Background(), frames, func(e map[string]any) error {
			if e["type"] == "ready" {
				close(ready)
			}
			if e["type"] == "transcript" {
				if e["final"] == true {
					final = String(e["text"])
				} else if e["text"] == "边说边识别" {
					close(partial)
				}
			}
			return nil
		})
	}()
	select {
	case <-ready:
	case <-time.After(time.Second):
		t.Fatal("ASR did not become ready")
	}
	frames <- make([]byte, 3200)
	select {
	case <-partial:
	case <-time.After(time.Second):
		t.Fatal("no partial recognition before end of speech")
	}
	close(frames)
	if err := <-done; err != nil || final != "边说边识别。" {
		t.Fatal(err, final)
	}
}

func TestLiveASRCancelsBothDirections(t *testing.T) {
	closed := make(chan struct{})
	bus := speechFixture(t, "asr", func(conn *websocket.Conn) {
		_, _ = receiveFixture(conn, "session.update")
		_ = conn.WriteJSON(map[string]any{"type": "session.updated"})
		_, _, _ = conn.ReadMessage()
		close(closed)
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- bus.LiveASR(ctx, make(chan []byte), func(e map[string]any) error {
			if e["type"] == "ready" {
				close(ready)
			}
			return nil
		})
	}()
	<-ready
	cancel()
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("live recognizer did not cancel")
	}
	<-closed
}
