package server

import (
	"encoding/base64"
	"net/http"

	"avatarharness/internal/store"
)

// Keep model connections and provider credentials on the local Go side.
func (a *App) driver(w http.ResponseWriter, r *http.Request) {
	session, ok := a.session(w, r)
	if !ok {
		return
	}
	character, _ := a.Runtime.Character(session.CharacterID)
	if character.Renderer.Kind != "lam" || character.Renderer.Driver != "remote" {
		problem(w, 409, "当前角色未启用远程 LAM 驱动")
		return
	}
	input := map[string]any{"operation": "health"}
	if r.Method == "POST" {
		r.Body = http.MaxBytesReader(w, r.Body, 200000)
		var body struct {
			Audio string `json:"audio_base64"`
		}
		if !decode(w, r, &body) {
			return
		}
		raw, err := base64.StdEncoding.DecodeString(body.Audio)
		if err != nil || len(raw) != 34133*4 {
			problem(w, 422, "需要 34133 个 16kHz 单声道 float32 音频采样")
			return
		}
		input = map[string]any{"operation": "infer_window", "audio_base64": body.Audio, "sample_rate": 16000, "format": "pcm_f32le"}
	}
	result := a.Runtime.Bus.Invoke(r.Context(), "avatar", input, map[string]any{"session_id": session.ID, "request_id": store.ID()})
	if result.Status != "ok" {
		problem(w, 502, result.Error)
		return
	}
	write(w, 200, result.Output)
}
