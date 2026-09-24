package api

import (
	"net/http"
	"path/filepath"

	"github.com/jo/TinyPS2Manager/internal/art"
)

func (s *Server) handleLibraryEnrichment(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "id", err.Error())
		return
	}
	it, err := s.lib.Get(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "", err.Error())
		return
	}
	if it == nil {
		writeErr(w, http.StatusNotFound, "id", "no such library item")
		return
	}
	// Art key (spec §2.8)
	artKey := art.KeyFor(*it, "", "", false)
	// For PS1 VCD, if the item is part of a multi-disc group, the VCD name
	// would be used; for now we use title-based key.
	// Check if art file exists in any destination? For enrichment status we
	// just report the key and whether GameID is uncertain.
	// Cheat status: if GameID present and not uncertain, we could check for
	// a .cht file in a default location, but without destination we report
	// "unknown" — the per-destination preflight will show actual file presence.
	// Region matched is true when GameID was extracted cleanly (not uncertain)
	// and a cheat DB entry would match; for now we report based on uncertain.
	regionMatched := !it.GameIDUncertain && it.GameID != ""
	// Determine Art status by checking if a file exists in any known destination?
	// For now, report missing (UI will show badge and allow fetch).
	artStatus := "missing"
	// If the library item has a custom art flag? Not tracked yet; future
	// customArt could be persisted. For now, missing.
	// Cheat status
	cheatStatus := "missing"
	if it.GameID != "" {
		cheatStatus = "available"
		if it.GameIDUncertain {
			cheatStatus = "needs_confirm"
		}
	}
	// If the destination's ART file exists, we could check — but this endpoint
	// is per-library item without destination, so we return the computed key.
	_ = filepath.Join
	writeJSON(w, http.StatusOK, map[string]any{
		"gameId":          it.GameID,
		"gameIdUncertain": it.GameIDUncertain,
		"artKey":          artKey,
		"artStatus":       artStatus,
		"cheatStatus":     cheatStatus,
		"regionMatched":   regionMatched,
	})
}
