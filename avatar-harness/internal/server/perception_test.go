package server

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"avatarharness/internal/config"
	"avatarharness/internal/provider"
)

func TestCameraRequiresActiveTokenAndLateInferenceCannotReviveIt(t *testing.T) {
	var calls atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		close(started)
		<-release
		_ = json.NewEncoder(w).Encode(provider.OK(map[string]any{"face_detected": true, "au_intensities": map[string]any{"AU12": 2}}))
	}))
	defer remote.Close()
	c := config.Default()
	c.Perception = &config.Provider{Kind: "fastapi", BaseURL: remote.URL, TimeoutMS: 2000}
	a := setup(t, &c)
	sid := a.Runtime.Store.Create(c.Characters[0].ID).ID
	url := "/api/sessions/" + sid + "/perception"
	frame := map[string]any{"operation": "frame", "token": "missing", "frame": 1, "image_base64": base64.StdEncoding.EncodeToString([]byte("camera fixture"))}
	if res := request(t, a, "POST", url, frame); res.Code != 409 || calls.Load() != 0 {
		t.Fatal("camera uploaded without active consent", res.Code)
	}
	res := request(t, a, "POST", url, map[string]any{"operation": "start"})
	var body map[string]any
	_ = json.Unmarshal(res.Body.Bytes(), &body)
	token := provider.String(body["token"])
	frame["token"] = token
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- request(t, a, "POST", url, frame) }()
	<-started
	request(t, a, "POST", url, map[string]any{"operation": "stop", "token": token})
	close(release)
	if result := <-done; result.Code != 409 {
		t.Fatal("late result accepted after stop", result.Code)
	}
	if a.Runtime.PerceptionActive(sid, token) {
		t.Fatal("stopped camera became active")
	}
	if result := request(t, a, "POST", url, frame); result.Code != 409 || calls.Load() != 1 {
		t.Fatal("stopped camera forwarded another frame")
	}
}
