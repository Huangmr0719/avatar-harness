package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	pc "avatarharness/internal/promptcompare"
	kernel "avatarharness/internal/runtime"
)

func (a *App) comparisonRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/prompt-comparisons/scenarios", func(w http.ResponseWriter, r *http.Request) {
		conditions, err := a.Comparisons.Conditions(r.URL.Query().Get("character_id"))
		if err != nil {
			problem(w, 422, err.Error())
			return
		}
		characters := []map[string]string{}
		for _, c := range a.Config.Characters {
			characters = append(characters, map[string]string{"id": c.ID, "name": c.Name})
		}
		write(w, 200, map[string]any{"scenarios": pc.Scenarios(), "variants": kernel.PromptVariants(), "conditions": conditions, "characters": characters})
	})
	mux.HandleFunc("POST /api/prompt-comparisons", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 256<<10)
		var input pc.Request
		if !decode(w, r, &input) {
			return
		}
		run, err := a.Comparisons.Start(input)
		if err != nil {
			status := 422
			if strings.Contains(err.Error(), "已有对照") {
				status = 409
			}
			problem(w, status, err.Error())
			return
		}
		write(w, 202, run)
	})
	mux.HandleFunc("GET /api/prompt-comparisons/{id}", func(w http.ResponseWriter, r *http.Request) {
		value, ok := a.Comparisons.Snapshot(r.PathValue("id"))
		if !ok {
			problem(w, 404, "对照记录已过期或不存在")
			return
		}
		write(w, 200, value)
	})
	mux.HandleFunc("POST /api/prompt-comparisons/{id}/stop", func(w http.ResponseWriter, r *http.Request) {
		if !a.Comparisons.Stop(r.PathValue("id")) {
			problem(w, 404, "对照记录不存在")
			return
		}
		write(w, 200, map[string]any{"status": "stopping"})
	})
	mux.HandleFunc("GET /api/prompt-comparisons/{id}/events", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		changed, unsubscribe, ok := a.Comparisons.Subscribe(id)
		if !ok {
			problem(w, 404, "对照记录已过期或不存在")
			return
		}
		defer unsubscribe()
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Accel-Buffering", "no")
		heartbeat := time.NewTicker(15 * time.Second)
		defer heartbeat.Stop()
		for {
			value, exists := a.Comparisons.Snapshot(id)
			if !exists {
				return
			}
			raw, err := json.Marshal(value)
			if err != nil {
				return
			}
			control := http.NewResponseController(w)
			_ = control.SetWriteDeadline(time.Now().Add(5 * time.Second))
			_, err = fmt.Fprintf(w, "id: %d\ndata: %s\n\n", value.Revision, raw)
			if err != nil {
				return
			}
			if err = control.Flush(); err != nil {
				return
			}
			_ = control.SetWriteDeadline(time.Time{})
			if value.Status != "running" {
				return
			}
			select {
			case <-r.Context().Done():
				return
			case <-a.done.Done():
				return
			case <-changed:
			case <-heartbeat.C:
			}
		}
	})
}
