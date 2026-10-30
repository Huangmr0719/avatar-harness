package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"avatarharness/internal/config"
	"avatarharness/internal/provider"
	"github.com/gorilla/websocket"
)

type voiceUtterance struct {
	id        string
	frames    chan []byte
	cancel    context.CancelFunc
	committed bool
	endedAt   time.Time
	deadline  *time.Timer
}

type voiceConnection struct {
	app    *App
	sid    string
	conn   *websocket.Conn
	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.Mutex
	write  sync.Mutex
	input  *voiceUtterance
}

func (a *App) interruptVoice(sid string) bool {
	a.voiceMu.Lock()
	v := a.voices[sid]
	a.voiceMu.Unlock()
	if v != nil {
		return v.interrupt()
	}
	return false
}

func (v *voiceConnection) send(event map[string]any) error {
	v.write.Lock()
	defer v.write.Unlock()
	_ = v.conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	return v.conn.WriteJSON(event)
}

// Call with mu held. A new cloud session is warmed while the user is idle;
// readers of older sessions can never start a replacement turn.
func (v *voiceConnection) warm() {
	ctx, cancel := context.WithCancel(v.ctx)
	u := &voiceUtterance{frames: make(chan []byte, 96), cancel: cancel}
	v.input = u
	go func() {
		err := v.app.Runtime.Bus.LiveASR(ctx, u.frames, func(event map[string]any) error {
			v.mu.Lock()
			defer v.mu.Unlock()
			if v.input != u || ctx.Err() != nil {
				return context.Canceled
			}
			switch event["type"] {
			case "ready":
				return v.send(map[string]any{"type": "ready", "sample_rate": 16000})
			case "transcript":
				if u.id == "" {
					return nil
				}
				text := strings.TrimSpace(provider.String(event["text"]))
				final := event["final"] == true
				if err := v.send(map[string]any{"type": "transcript", "input_id": u.id, "text": text, "final": final}); err != nil {
					return err
				}
				if !final {
					return nil
				}
				if !u.committed {
					return fmt.Errorf("ASR 在提交之前返回了最终结果")
				}
				if u.deadline != nil {
					u.deadline.Stop()
				}
				if text != "" {
					turn, err := v.app.Runtime.StartVoice(v.sid, text, u.id, u.endedAt)
					if err != nil {
						return err
					}
					_ = v.send(map[string]any{"type": "turn", "input_id": u.id, "turn_id": turn.ID})
				}
				v.warm()
			}
			return nil
		})
		v.mu.Lock()
		defer v.mu.Unlock()
		if v.input == u && ctx.Err() == nil && err != nil {
			_ = v.send(map[string]any{"type": "error", "message": err.Error(), "input_id": u.id})
			v.cancel()
		}
		cancel()
	}()
}

func (v *voiceConnection) abandon() {
	if u := v.input; u != nil {
		u.cancel()
		if u.deadline != nil {
			u.deadline.Stop()
		}
	}
	v.input = nil
}

func (v *voiceConnection) interrupt() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	hadInput := v.input != nil && v.input.id != ""
	v.abandon()
	cancelled := v.app.Runtime.Cancel(v.sid, "用户打断")
	if v.ctx.Err() == nil {
		v.warm()
	}
	return hadInput || cancelled
}

func (a *App) voice(w http.ResponseWriter, r *http.Request) {
	session, ok := a.session(w, r)
	if !ok {
		return
	}
	p := a.Config.Providers["asr"]
	if p.Kind != "bailian" || config.State(p)["configured"] != true {
		problem(w, 503, "请先配置百炼实时语音及密钥")
		return
	}
	conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
	if err != nil {
		return
	}
	ctx, cancel := context.WithCancel(a.done)
	v := &voiceConnection{app: a, sid: session.ID, conn: conn, ctx: ctx, cancel: cancel}
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	defer cancel()
	defer conn.Close()
	a.voiceMu.Lock()
	if old := a.voices[session.ID]; old != nil {
		old.cancel()
	}
	a.voices[session.ID] = v
	a.voiceMu.Unlock()
	defer func() {
		v.mu.Lock()
		v.abandon()
		v.mu.Unlock()
		a.voiceMu.Lock()
		if a.voices[session.ID] == v {
			delete(a.voices, session.ID)
			a.Runtime.Cancel(session.ID, "语音连接关闭")
		}
		a.voiceMu.Unlock()
	}()
	v.mu.Lock()
	v.warm()
	v.mu.Unlock()
	conn.SetReadLimit(64 << 10)
	for {
		kind, raw, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if kind == websocket.BinaryMessage {
			v.mu.Lock()
			u := v.input
			if u == nil || u.id == "" || u.committed || len(raw) == 0 || len(raw)%2 != 0 || len(raw) > 6400 {
				err = fmt.Errorf("实时音频帧无效或未开始语音")
			} else {
				select {
				case u.frames <- raw:
				default:
					err = fmt.Errorf("识别连接积压，已停止本次录音，请重试")
				}
			}
			v.mu.Unlock()
		} else {
			var message struct {
				Type      string  `json:"type"`
				InputID   string  `json:"input_id"`
				RequestID string  `json:"request_id"`
				EndAgeMS  float64 `json:"end_age_ms"`
			}
			if json.Unmarshal(raw, &message) != nil {
				return
			}
			v.mu.Lock()
			switch message.Type {
			case "start":
				if message.InputID == "" || len(message.InputID) > 64 {
					err = fmt.Errorf("语音输入 ID 无效")
					break
				}
				if v.input == nil || v.input.id != "" {
					v.abandon()
					v.warm()
				}
				v.input.id = message.InputID
				a.Runtime.Cancel(session.ID, "新的语音输入")
			case "commit":
				u := v.input
				if u == nil || u.id != message.InputID || u.committed || message.EndAgeMS < 0 || message.EndAgeMS > 10000 {
					err = fmt.Errorf("语音提交状态无效")
					break
				}
				u.committed = true
				u.endedAt = time.Now().Add(-time.Duration(message.EndAgeMS * float64(time.Millisecond)))
				close(u.frames)
				u.deadline = time.AfterFunc(time.Duration(p.TimeoutMS)*time.Millisecond, func() {
					v.mu.Lock()
					defer v.mu.Unlock()
					if v.input == u {
						_ = v.send(map[string]any{"type": "error", "input_id": u.id, "message": "语音识别等待超时"})
						v.cancel()
					}
				})
			case "cancel":
				v.abandon()
				a.Runtime.Cancel(session.ID, "用户停止")
				v.warm()
				_ = v.send(map[string]any{"type": "cancelled", "request_id": message.RequestID})
			default:
				err = fmt.Errorf("未知语音事件")
			}
			v.mu.Unlock()
		}
		if err != nil {
			_ = v.send(map[string]any{"type": "error", "message": err.Error()})
			return
		}
	}
}
