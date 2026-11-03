package server

import (
	"net/http"
	"os"
)

func (a *App) practiceVideo() map[string]any {
	v := a.Config.PracticeVideo
	if v == nil {
		return map[string]any{"title": "光点观察练习（演示）", "url": "/support/attention-observation.mp4", "duration_seconds": 120, "has_audio": false, "available": true}
	}
	info, err := os.Stat(v.Path)
	available := err == nil && info.Mode().IsRegular() && info.Size() > 0
	return map[string]any{"title": v.Title, "url": "/api/practice-video/file", "duration_seconds": v.DurationSeconds, "has_audio": v.HasAudio, "source_url": v.SourceURL, "available": available}
}

func (a *App) practiceVideoFile(w http.ResponseWriter, r *http.Request) {
	v := a.Config.PracticeVideo
	if v == nil {
		http.NotFound(w, r)
		return
	}
	// ServeFile supports byte ranges without reading the full video into RAM.
	// The configured media file stays outside the executable and Git assets.
	w.Header().Set("Content-Type", "video/mp4")
	http.ServeFile(w, r, v.Path)
}
