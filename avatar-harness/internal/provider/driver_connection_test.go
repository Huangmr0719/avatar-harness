package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"avatarharness/internal/config"
)

func TestStatelessDriverWindowRecoversClosedReusedConnection(t *testing.T) {
	var calls atomic.Int32
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		n := calls.Add(1)
		if n == 2 {
			connection, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			connection.Close() // The old keep-alive connection dies without a response.
			return
		}
		if req.Input["operation"] == "infer_window" && (req.Input["audio_base64"] != "same-window" || r.Header.Get("Idempotency-Key") != "window-request") {
			t.Error("inference replay did not preserve its request", req, r.Header)
		}
		json.NewEncoder(w).Encode(OK(map[string]any{"backend": "cuda"}))
	}))
	defer remote.Close()
	c := config.Default()
	c.Providers["avatar"] = config.Provider{Kind: "fastapi", BaseURL: remote.URL, TimeoutMS: 2000}
	bus, _ := New(&c, nil)
	if result := bus.Invoke(context.Background(), "avatar", map[string]any{"operation": "health"}, nil); result.Status != "ok" {
		t.Fatal(result)
	}
	result := bus.Invoke(context.Background(), "avatar", map[string]any{"operation": "infer_window", "audio_base64": "same-window"}, map[string]any{"request_id": "window-request"})
	if result.Status != "ok" || calls.Load() != 3 {
		t.Fatal("closed idle connection interrupted the driver", result, calls.Load())
	}
}
