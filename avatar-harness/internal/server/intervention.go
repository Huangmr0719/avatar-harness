package server

import (
	"net/http"

	"avatarharness/internal/intervention"
)

func (a *App) interventionRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/sessions/{id}/intervention", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := a.session(w, r); !ok {
			return
		}
		write(w, 200, a.Runtime.Store.Intervention(r.PathValue("id")))
	})
	mux.HandleFunc("POST /api/sessions/{id}/intervention", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := a.session(w, r); !ok {
			return
		}
		var command intervention.Command
		if !decode(w, r, &command) {
			return
		}
		state, err := a.Runtime.SupportCommand(r.PathValue("id"), command)
		if err != nil {
			problem(w, 409, err.Error())
			return
		}
		write(w, 200, state)
	})
}
