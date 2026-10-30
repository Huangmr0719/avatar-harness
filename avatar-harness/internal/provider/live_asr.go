package provider

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/gorilla/websocket"
)

// LiveASR reads and writes concurrently. Closing audio commits one utterance;
// cancelling ctx closes the socket and discards pending recognition results.
func (b *Bus) LiveASR(parent context.Context, audio <-chan []byte, emit Emit) (err error) {
	p := b.Config.Providers["asr"]
	if p.Kind != "bailian" {
		return fmt.Errorf("当前 ASR 不支持持续音频输入")
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	conn, err := dialBailian(ctx, p, "asr")
	if err != nil {
		return err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	defer func() { err = realtimeError(parent, err) }()
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	language := String(p.Parameters["language"])
	if language == "" {
		language = "zh"
	}
	if err = realtimeConfigure(conn, map[string]any{"input_audio_format": "pcm", "sample_rate": 16000, "input_audio_transcription": map[string]any{"language": language}, "turn_detection": nil}); err != nil {
		return err
	}
	_ = conn.SetReadDeadline(time.Time{})
	if err = emit(map[string]any{"type": "ready"}); err != nil {
		return err
	}
	writerDone := make(chan error, 1)
	go func() {
		heartbeat := time.NewTicker(10 * time.Second)
		defer heartbeat.Stop()
		var writeErr error
		defer func() {
			writerDone <- writeErr
			if writeErr != nil {
				_ = conn.Close()
			}
		}()
		count := 0
		for {
			select {
			case <-ctx.Done():
				writeErr = ctx.Err()
				return
			case <-heartbeat.C:
				if writeErr = conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(3*time.Second)); writeErr != nil {
					return
				}
			case raw, ok := <-audio:
				_ = conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
				if !ok {
					if count < 3200 {
						writeErr = fmt.Errorf("语音不足 100ms，未提交识别")
						return
					}
					if writeErr = realtimeSend(conn, "input_audio_buffer.commit", nil); writeErr == nil {
						writeErr = realtimeSend(conn, "session.finish", nil)
					}
					return
				}
				if len(raw) == 0 || len(raw)%2 != 0 || len(raw) > 6400 || count+len(raw) > 16000*2*60 {
					writeErr = fmt.Errorf("实时 PCM 音频帧或录音时长无效")
					return
				}
				count += len(raw)
				if writeErr = realtimeSend(conn, "input_audio_buffer.append", map[string]any{"audio": base64.StdEncoding.EncodeToString(raw)}); writeErr != nil {
					return
				}
			}
		}
	}()
	defer func() {
		cancel()
		_ = conn.Close()
		if writeErr := <-writerDone; writeErr != nil && !errors.Is(writeErr, context.Canceled) && err != nil && parent.Err() == nil {
			err = writeErr
		}
	}()
	completed := false
	for {
		event, readErr := realtimeRead(conn)
		if readErr != nil {
			return readErr
		}
		switch event["type"] {
		case "conversation.item.input_audio_transcription.text":
			if err = emit(map[string]any{"type": "transcript", "text": String(event["text"]) + String(event["stash"]), "final": false}); err != nil {
				return err
			}
		case "conversation.item.input_audio_transcription.completed":
			completed = true
			if err = emit(map[string]any{"type": "transcript", "text": String(event["transcript"]), "final": true, "language": event["language"], "emotion": event["emotion"]}); err != nil {
				return err
			}
		case "session.finished":
			if !completed {
				return fmt.Errorf("实时 ASR 缺少最终识别结果")
			}
			return nil
		}
	}
}
