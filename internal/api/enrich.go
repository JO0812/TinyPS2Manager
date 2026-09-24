package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jo/TinyPS2Manager/internal/art"
	"github.com/jo/TinyPS2Manager/internal/cheats"
	"github.com/jo/TinyPS2Manager/internal/library"
	"github.com/jo/TinyPS2Manager/internal/queue"
	"github.com/jo/TinyPS2Manager/internal/riptopl"
	"github.com/jo/TinyPS2Manager/internal/transfer"
)

// handleEnrich dispatches POST /api/enrich/{kind} where kind is art, cheats, or riptopl.
// Body: {destinationId: int, itemIds?: []int, tag?: string, confirmUncertain?: bool}
func (s *Server) handleEnrich(w http.ResponseWriter, r *http.Request) {
	kind := chi.URLParam(r, "kind")
	switch kind {
	case "art", "cheats", "riptopl":
	default:
		writeErr(w, http.StatusNotFound, "kind", "want art, cheats, or riptopl")
		return
	}
	var body struct {
		DestinationID    int64   `json:"destinationId"`
		ItemIDs          []int64 `json:"itemIds"`
		Tag              string  `json:"tag"`
		ConfirmUncertain bool    `json:"confirmUncertain"`
		MissingOnly      bool    `json:"missingOnly"`
	}
	if err := decodeStrict(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "body", err.Error())
		return
	}
	if body.DestinationID == 0 {
		writeErr(w, http.StatusBadRequest, "destinationId", "want a positive id")
		return
	}
	dest, err := s.qstore.GetDestination(body.DestinationID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "", err.Error())
		return
	}
	if dest == nil {
		writeErr(w, http.StatusNotFound, "destinationId", "no such destination")
		return
	}
	switch kind {
	case "art":
		s.enrichArt(w, r, dest, body.ItemIDs, body.MissingOnly)
	case "cheats":
		s.enrichCheats(w, r, dest, body.ItemIDs, body.ConfirmUncertain)
	case "riptopl":
		s.enrichRiptopl(w, r, dest, body.Tag)
	}
}

func (s *Server) enrichArt(w http.ResponseWriter, r *http.Request, dest *queue.Destination, itemIDs []int64, missingOnly bool) {
	var items []library.LibraryItem
	if len(itemIDs) > 0 {
		for _, id := range itemIDs {
			it, err := s.lib.Get(id)
			if err != nil {
				writeErr(w, http.StatusInternalServerError, "", err.Error())
				return
			}
			if it != nil {
				items = append(items, *it)
			}
		}
	} else {
		all, err := s.lib.List()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "", err.Error())
			return
		}
		items = all
	}
	client := &art.Client{}
	ctx := r.Context()
	disk := transfer.FileDisk{}
	type result struct {
		ID     int64  `json:"id"`
		Key    string `json:"key"`
		Status string `json:"status"` // staged, skipped, failed, custom
		Error  string `json:"error,omitempty"`
	}
	var results []result
	for _, it := range items {
		key := art.KeyFor(it, "", "", false)
		artPath := filepath.Join(dest.Path, dest.BDMPrefix, "ART", key)
		if missingOnly {
			if _, err := os.Stat(artPath); err == nil {
				results = append(results, result{ID: it.ID, Key: key, Status: "skipped"})
				continue
			}
		}
		if _, err := disk.Stat(artPath); err == nil {
			results = append(results, result{ID: it.ID, Key: key, Status: "skipped"})
			continue
		}
		var urls []string
		if it.Platform == library.PlatformPS2 {
			if it.GameID == "" {
				results = append(results, result{ID: it.ID, Key: key, Status: "failed", Error: "no GameID"})
				continue
			}
			urls = art.PS2CoverURLs(it.GameID)
		} else {
			urls = art.PS1CoverURLs(it.Title)
		}
		if len(urls) == 0 {
			results = append(results, result{ID: it.ID, Key: key, Status: "failed", Error: "no URL"})
			continue
		}
		artURL := urls[0]
		data, err := client.Fetch(ctx, artURL)
		if err != nil {
			// Generate custom as fallback, flagged customArt
			data = art.GenerateCustom(it.Title, it.Platform == library.PlatformPS2)
			if err := art.Stage(ctx, disk, dest.Path, dest.BDMPrefix, key, data); err != nil {
				results = append(results, result{ID: it.ID, Key: key, Status: "failed", Error: err.Error()})
				continue
			}
			results = append(results, result{ID: it.ID, Key: key, Status: "custom"})
			continue
		}
		if norm, err := art.ValidateAndNormalize(data, it.Platform == library.PlatformPS2); err == nil {
			data = norm
		}
		if err := art.Stage(ctx, disk, dest.Path, dest.BDMPrefix, key, data); err != nil {
			results = append(results, result{ID: it.ID, Key: key, Status: "failed", Error: err.Error()})
			continue
		}
		results = append(results, result{ID: it.ID, Key: key, Status: "staged"})
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

func (s *Server) enrichCheats(w http.ResponseWriter, r *http.Request, dest *queue.Destination, itemIDs []int64, confirm bool) {
	var items []library.LibraryItem
	if len(itemIDs) > 0 {
		for _, id := range itemIDs {
			it, err := s.lib.Get(id)
			if err != nil {
				writeErr(w, http.StatusInternalServerError, "", err.Error())
				return
			}
			if it != nil {
				items = append(items, *it)
			}
		}
	} else {
		all, err := s.lib.List()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "", err.Error())
			return
		}
		items = all
	}
	// Load widescreen pack if present (optional dir)
	wideMap, _ := cheats.ParseWidescreenDir("widescreen")
	// Also try CheatDatabase.txt in cwd
	var dbMap map[string]cheats.RawGame
	if raw, err := os.ReadFile("CheatDatabase.txt"); err == nil {
		dbMap, _ = cheats.ParseDatabase(raw)
	}
	disk := transfer.FileDisk{}
	ctx := r.Context()
	type result struct {
		ID     int64  `json:"id"`
		GameID string `json:"gameId"`
		Status string `json:"status"`
		Error  string `json:"error,omitempty"`
	}
	var results []result
	for _, it := range items {
		if it.Platform != library.PlatformPS2 || it.GameID == "" {
			continue
		}
		var content string
		var source string
		if data, ok := wideMap[it.GameID]; ok {
			content = string(data)
			source = "widescreen"
		} else if g, ok := dbMap[it.Title]; ok {
			built, warns, err := cheats.Build(it.GameID, g.Cheats)
			if err != nil {
				results = append(results, result{ID: it.ID, GameID: it.GameID, Status: "failed", Error: err.Error()})
				continue
			}
			if warns.HasEngineSkipped {
				// still stage, but note warning
			}
			content = built
			source = "database"
			_ = source
		} else {
			results = append(results, result{ID: it.ID, GameID: it.GameID, Status: "missing"})
			continue
		}
		// Stage
		err := cheats.Stage(ctx, disk, dest.Path, dest.BDMPrefix, it, content, confirm)
		if err != nil {
			if strings.Contains(err.Error(), "explicit confirm") {
				results = append(results, result{ID: it.ID, GameID: it.GameID, Status: "needs_confirm", Error: err.Error()})
			} else if err == nil {
				results = append(results, result{ID: it.ID, GameID: it.GameID, Status: "skipped"})
			} else {
				results = append(results, result{ID: it.ID, GameID: it.GameID, Status: "failed", Error: err.Error()})
			}
			continue
		}
		results = append(results, result{ID: it.ID, GameID: it.GameID, Status: "staged"})
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

func (s *Server) enrichRiptopl(w http.ResponseWriter, r *http.Request, dest *queue.Destination, tag string) {
	if tag == "" {
		tag = "current-fan-favorite"
	}
	client := s.riptoplClient()
	ctx := r.Context()
	rel, err := client.Resolve(ctx, tag)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "tag", err.Error())
		return
	}
	zipPath, err := client.Download(ctx, rel, nil)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "download", err.Error())
		return
	}
	defer os.Remove(zipPath)
	st, err := riptopl.Stage(ctx, transfer.FileDisk{}, dest.Path, dest.BDMPrefix, zipPath, nil)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "", err.Error())
		return
	}
	// Record pinned version
	raw, _ := json.Marshal(map[string]any{"tag": rel.Tag, "asset": rel.AssetName, "digest": rel.Digest, "flavour": st.Flavour})
	_ = s.qstore.SetState(fmt.Sprintf("loader.%d", dest.ID), string(raw))
	writeJSON(w, http.StatusOK, map[string]any{
		"tag": rel.Tag, "asset": rel.AssetName, "url": rel.AssetURL, "digest": rel.Digest,
		"flavour": st.Flavour, "elfPath": st.ELFPath,
	})
}
