package api

import (
	"net/http"

	"github.com/jo/TinyPS2Manager/internal/queue"
)

func (s *Server) handlePreflight(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		writeErr(w, http.StatusBadRequest, "path", "want a destination path")
		return
	}
	dest, err := queue.ResolveDestination(s.qstore, path)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "", err.Error())
		return
	}
	res, err := queue.Preflight(dest)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}
