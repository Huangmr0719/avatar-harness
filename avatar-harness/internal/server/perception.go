package server

import (
	"encoding/base64"
	"net/http"
	"time"
)

// No camera images or high-rate predictions are written to the session log.
func (a *App) perception(w http.ResponseWriter, r *http.Request) {
	s, ok := a.session(w, r)
	if !ok {
		return
	}
	var body struct {
		Operation string  `json:"operation"`
		Token     string  `json:"token"`
		Frame     int64   `json:"frame"`
		AgeMS     float64 `json:"age_ms"`
		Image     string  `json:"image_base64"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 600<<10)
	if !decode(w, r, &body) {
		return
	}
	if body.Operation == "stop" {
		a.Runtime.EndPerception(s.ID, body.Token)
		write(w, 200, map[string]any{"status": "off"})
		return
	}
	if a.Config.Perception == nil {
		problem(w, 503, "摄像头分析尚未配置")
		return
	}
	if body.Operation == "start" {
		write(w, 200, map[string]any{"token": a.Runtime.BeginPerception(s.ID)})
		return
	}
	if body.Operation != "frame" || !a.Runtime.PerceptionActive(s.ID, body.Token) {
		problem(w, 409, "本次分析已关闭")
		return
	}
	if body.Frame < 1 || body.AgeMS < 0 || body.AgeMS > 1500 || len(body.Image) > 500000 {
		problem(w, 422, "摄像头帧无效或已过期")
		return
	}
	raw, err := base64.StdEncoding.DecodeString(body.Image)
	if err != nil || len(raw) < 4 || len(raw) > 350000 {
		problem(w, 422, "摄像头图像无效")
		return
	}
	at := time.Now().Add(-time.Duration(body.AgeMS * float64(time.Millisecond)))
	result := a.Runtime.Bus.Perceive(r.Context(), map[string]any{"operation": "analyze", "image_base64": body.Image}, map[string]any{"session_id": s.ID})
	if r.Context().Err() != nil {
		return
	}
	if result.Status != "ok" {
		problem(w, 502, result.Error)
		return
	}
	if !a.Runtime.ObserveFace(s.ID, body.Token, body.Frame, at, result.Output) {
		problem(w, 409, "摄像头结果已过期")
		return
	}
	write(w, 200, result.Output)
}
