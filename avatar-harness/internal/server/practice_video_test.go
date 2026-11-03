package server

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"avatarharness/internal/config"
	"avatarharness/internal/intervention"
)

func TestLocalPracticeVideoStreamsRangesAndStartsFullLength(t *testing.T) {
	c := config.Default()
	path := filepath.Join(t.TempDir(), "practice.mp4")
	if err := os.WriteFile(path, []byte("0123456789abcdef"), 0600); err != nil {
		t.Fatal(err)
	}
	c.PracticeVideo = &config.PracticeVideo{Path: path, Title: "三分钟呼吸空间练习", DurationSeconds: 264, HasAudio: true}
	a := setup(t, &c)
	state := request(t, a, "GET", "/api/state", nil)
	var value struct {
		Video map[string]any `json:"practice_video"`
	}
	if err := json.Unmarshal(state.Body.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	if value.Video["available"] != true || value.Video["has_audio"] != true || value.Video["url"] != "/api/practice-video/file" || strings.Contains(state.Body.String(), path) {
		t.Fatal("local video descriptor missing or leaked its filesystem path")
	}
	req := httptest.NewRequest("GET", "/api/practice-video/file", nil)
	req.Header.Set("Range", "bytes=4-7")
	w := httptest.NewRecorder()
	a.Handler.ServeHTTP(w, req)
	if w.Code != 206 || w.Body.String() != "4567" || w.Header().Get("Content-Range") != "bytes 4-7/16" || w.Header().Get("Content-Type") != "video/mp4" {
		t.Fatal("seeking did not return the requested media byte range", w.Code, w.Body.String())
	}
	sid := a.Runtime.Store.Create(c.Characters[0].ID).ID
	s, err := a.Runtime.SupportCommand(sid, intervention.Command{Operation: "start", Mode: intervention.Mindfulness})
	if err != nil || s.Execution == nil || s.Execution.Parameters.DurationSeconds != 264 {
		t.Fatal("full video length was replaced by the former short demo limit", s, err)
	}
	if err := s.Execution.Parameters.Validate(); err != nil {
		t.Fatal(err)
	}
}
