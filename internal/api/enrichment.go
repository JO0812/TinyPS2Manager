package api

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/jo/TinyPS2Manager/internal/art"
	"github.com/jo/TinyPS2Manager/internal/cheats"
	"github.com/jo/TinyPS2Manager/internal/oplfs"
	"github.com/jo/TinyPS2Manager/internal/queue"
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
	// RegionMatched compares the CheatDatabase title's build region against
	// the disc's region (spec §2.9); uncertain GameIDs never match so the UI
	// defaults those to on-console Select mode.
	regionMatched := !it.GameIDUncertain && it.GameID != "" && cheats.RegionMatches(it.GameID, it.Title)
	artStatus := "missing"
	cheatStatus := "missing"
	if it.GameID != "" {
		cheatStatus = "available"
		if it.GameIDUncertain {
			cheatStatus = "needs_confirm"
		}
	}
	// With ?destinationPath=, report staged state from the actual device:
	// ART/<key> present → found; CHT/<GameID>.cht present → staged.
	if destPath := r.URL.Query().Get("destinationPath"); destPath != "" {
		if dest, err := queue.ResolveDestination(s.qstore, destPath); err == nil {
			artFile := filepath.Join(dest.Path, dest.BDMPrefix, string(oplfs.BucketART), artKey)
			if fi, serr := os.Stat(artFile); serr == nil && !fi.IsDir() {
				artStatus = "found"
			}
			if it.GameID != "" {
				chtFile := filepath.Join(dest.Path, dest.BDMPrefix, string(oplfs.BucketCHT), it.GameID+".cht")
				if fi, serr := os.Stat(chtFile); serr == nil && !fi.IsDir() {
					cheatStatus = "staged"
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"gameId":          it.GameID,
		"gameIdUncertain": it.GameIDUncertain,
		"artKey":          artKey,
		"artStatus":       artStatus,
		"cheatStatus":     cheatStatus,
		"regionMatched":   regionMatched,
	})
}
