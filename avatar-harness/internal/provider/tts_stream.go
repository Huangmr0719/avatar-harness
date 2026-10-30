package provider

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"avatarharness/internal/config"
	"github.com/gorilla/websocket"
)

// TTSStream is one synthesis lane for a turn. Construction prepares a Bailian
// connection asynchronously; sequential Stream calls reuse its commit session.
// Each concurrent lane owns a separate connection. Other providers keep their
// existing Stream/Invoke contract. Close or parent cancellation releases it.
type TTSStream struct {
	ctx       context.Context
	cancel    context.CancelFunc
	bus       *Bus
	provider  config.Provider
	input     map[string]any
	metadata  map[string]any
	ready     chan struct{}
	closeOnce sync.Once
	conn      *websocket.Conn
	stop      func() bool
	rate      int
	err       error
	dialMS    float64
	configMS  float64
	used      bool
}

func (b *Bus) NewTTSStream(parent context.Context, input, metadata map[string]any) *TTSStream {
	ctx, cancel := context.WithCancel(parent)
	p := b.Config.Providers["tts"]
	if p.TimeoutMS <= 0 {
		p.TimeoutMS = 15000
	}
	s := &TTSStream{ctx: ctx, cancel: cancel, bus: b, provider: p, input: input, metadata: metadata, ready: make(chan struct{})}
	if p.Kind == "bailian" && p.Streaming {
		go func() {
			defer close(s.ready)
			s.prepare()
		}()
	} else {
		close(s.ready)
	}
	return s
}

func (s *TTSStream) prepare() {
	session, rate, err := bailianTTSSession(s.provider, s.input)
	if isQwenAudioTTS(s.provider) {
		session, rate, err = qwenAudioParameters(s.provider, s.input)
	}
	if err != nil {
		s.err = err
		return
	}
	ctx, cancel := context.WithTimeout(s.ctx, time.Duration(s.provider.TimeoutMS)*time.Millisecond)
	defer cancel()
	start := time.Now()
	conn, err := dialBailian(ctx, s.provider, "tts")
	s.dialMS = float64(time.Since(start).Microseconds()) / 1000
	if err != nil {
		s.err = err
		return
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	deadline, _ := ctx.Deadline()
	_ = conn.SetReadDeadline(deadline)
	_ = conn.SetWriteDeadline(deadline)
	start = time.Now()
	if !isQwenAudioTTS(s.provider) {
		err = realtimeConfigure(conn, session)
	}
	s.configMS = float64(time.Since(start).Microseconds()) / 1000
	if err = realtimeError(ctx, err); err != nil {
		_ = conn.Close()
		s.err = err
		return
	}
	_ = conn.SetReadDeadline(time.Time{})
	_ = conn.SetWriteDeadline(time.Time{})
	s.conn, s.rate = conn, rate
	s.stop = context.AfterFunc(s.ctx, func() { _ = conn.Close() })
}

// Stream must not run concurrently with another Stream on this lane. Failure
// closes the connection; partially emitted text is never automatically replayed.
func (s *TTSStream) Stream(text string, emit Emit) (err error) {
	return s.StreamWithInput(text, nil, emit)
}

// One worker owns this lane; options are snapshots, not mutated shared state.
func (s *TTSStream) StreamWithInput(text string, options map[string]any, emit Emit) (err error) {
	if err := s.ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("TTS 文本不能为空")
	}
	input := map[string]any{}
	for key, value := range s.input {
		input[key] = value
	}
	for key, value := range options {
		input[key] = value
	}
	input["text"] = text
	if s.provider.Kind != "bailian" || !s.provider.Streaming {
		return s.bus.Stream(s.ctx, "tts", input, s.metadata, emit)
	}
	ctx, cancel := context.WithTimeout(s.ctx, time.Duration(s.provider.TimeoutMS)*time.Millisecond)
	defer cancel()
	start := time.Now()
	select {
	case <-s.ready:
	case <-ctx.Done():
		return ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.err != nil {
		return s.err
	}
	stop := context.AfterFunc(ctx, func() { _ = s.conn.Close() })
	defer stop()
	defer func() {
		err = realtimeError(ctx, err)
		if err != nil {
			s.err = err
			_ = s.conn.Close()
		}
	}()
	deadline, _ := ctx.Deadline()
	_ = s.conn.SetReadDeadline(deadline)
	_ = s.conn.SetWriteDeadline(deadline)
	if err = emit(map[string]any{"type": "progress", "stage": "tts.connection", "dial_ms": s.dialMS, "configure_ms": s.configMS, "wait_ms": float64(time.Since(start).Microseconds()) / 1000, "reused": s.used}); err != nil {
		return err
	}
	if isQwenAudioTTS(s.provider) {
		_, err = qwenAudioTTS(s.conn, s.provider, input, emit)
		if err == nil {
			s.used = true
		}
		return err
	}
	if err = realtimeSend(s.conn, "input_text_buffer.append", map[string]any{"text": text}); err != nil {
		return err
	}
	if err = realtimeSend(s.conn, "input_text_buffer.commit", nil); err != nil {
		return err
	}
	s.used = true
	_, err = readBailianTTS(s.conn, s.rate, emit, false)
	return err
}

func (s *TTSStream) Close() {
	s.closeOnce.Do(func() {
		s.cancel()
		<-s.ready
		if s.stop != nil {
			s.stop()
		}
		if s.conn != nil {
			_ = s.conn.Close()
		}
	})
}
