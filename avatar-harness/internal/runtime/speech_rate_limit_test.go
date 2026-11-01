package runtime

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"avatarharness/internal/config"
	"avatarharness/internal/provider"
	"github.com/gorilla/websocket"
)

func TestSpeechRetriesRateLimitOnlyBeforeAudio(t *testing.T) {
	for _, mode := range []string{"before_audio", "after_audio", "persistent", "cancel_retry"} {
		t.Run(mode, func(t *testing.T) {
			partial := mode == "after_audio"
			t.Setenv(config.BailianKeyEnv, "local-rate-limit-fixture")
			var requests atomic.Int32
			remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
				if err != nil {
					return
				}
				defer conn.Close()
				_ = conn.SetReadDeadline(time.Now().Add(4 * time.Second))
				var event map[string]any
				if conn.ReadJSON(&event) != nil {
					return
				}
				id := event["header"].(map[string]any)["task_id"]
				n := requests.Add(1)
				if (n == 1 || mode == "persistent") && !partial {
					_ = conn.WriteJSON(map[string]any{"header": map[string]any{"event": "task-failed", "task_id": id, "error_code": "Throttling.RateQuota", "error_message": "Requests rate limit exceeded"}})
					return
				}
				_ = conn.WriteJSON(map[string]any{"header": map[string]any{"event": "task-started", "task_id": id}})
				for range 2 {
					if conn.ReadJSON(&event) != nil {
						return
					}
				}
				_ = conn.WriteMessage(websocket.BinaryMessage, []byte{1, 0, 2, 0})
				if partial {
					_ = conn.WriteJSON(map[string]any{"header": map[string]any{"event": "task-failed", "task_id": id, "error_code": "Throttling.RateQuota", "error_message": "Requests rate limit exceeded"}})
				} else {
					_ = conn.WriteJSON(map[string]any{"header": map[string]any{"event": "task-finished", "task_id": id}})
				}
			}))
			defer remote.Close()
			c := config.Default()
			_ = c.UseBailian("")
			c.Limits.TTSConcurrency = 1
			p := c.Providers["tts"]
			p.BaseURL = "ws" + strings.TrimPrefix(remote.URL, "http") + "/api-ws/v1/inference"
			p.TimeoutMS = 3000
			c.Providers["tts"] = p
			b, err := provider.New(&c, nil)
			if err != nil {
				t.Fatal(err)
			}
			var audio []byte
			var failures int
			retrying := make(chan struct{}, 2)
			s := NewSpeech(context.Background(), &c, c.Characters[0], b, nil, func(kind string, data map[string]any) {
				if kind == "pipeline.timing" && data["stage"] == "tts.retry" {
					retrying <- struct{}{}
				}
				if kind == "pipeline.error" {
					failures++
				}
				if kind == "pipeline.audio" {
					raw, _ := base64.StdEncoding.DecodeString(provider.String(data["audio_base64"]))
					audio = append(audio, raw...)
				}
			})
			defer s.Stop()
			if err = s.Feed("限流恢复测试。", false); err != nil {
				t.Fatal(err)
			}
			if mode == "cancel_retry" {
				done := make(chan error, 1)
				go func() { done <- s.Finish() }()
				select {
				case <-retrying:
				case <-time.After(time.Second):
					t.Fatal("retry never started")
				}
				started := time.Now()
				s.Stop()
				<-done
				if time.Since(started) > 200*time.Millisecond || requests.Load() != 1 || len(audio) != 0 || failures != 0 {
					t.Fatal("cancelled retry produced an old request, audio or error")
				}
				return
			}
			if err = s.Finish(); err != nil {
				t.Fatal(err)
			}
			wantRequests, wantFailures := int32(2), 0
			wantBytes := 4
			if partial {
				wantRequests, wantFailures = 1, 1
			}
			if mode == "persistent" {
				wantRequests, wantFailures, wantBytes = 3, 1, 0
			}
			if requests.Load() != wantRequests || failures != wantFailures || len(audio) != wantBytes {
				t.Fatalf("requests=%d failures=%d audioBytes=%d; want %d/%d/%d", requests.Load(), failures, len(audio), wantRequests, wantFailures, wantBytes)
			}
		})
	}
}
