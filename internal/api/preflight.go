package api

import (
	"net/http"

	"github.com/jo/TinyPS2Manager/internal/queue"
)

func (s *Server) handlePreflight(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "id", err.Error())
		return
	}
	dest, err := s.qstore.GetDestination(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "", err.Error())
		return
	}
	if dest == nil {
		writeErr(w, http.StatusNotFound, "id", "no such destination")
		return
	}
	res, err := queue.Preflight(dest)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}
