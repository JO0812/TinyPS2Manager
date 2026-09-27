package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jo/TinyPS2Manager/internal/art"
	"github.com/jo/TinyPS2Manager/internal/cheats"
	"github.com/jo/TinyPS2Manager/internal/config"
	"github.com/jo/TinyPS2Manager/internal/library"
	"github.com/jo/TinyPS2Manager/internal/queue"
	"github.com/jo/TinyPS2Manager/internal/riptopl"
	"github.com/jo/TinyPS2Manager/internal/transfer"
)

// handleEnrich dispatches POST /api/enrich/{kind} where kind is art, cheats, or riptopl.
// Body: {destinationPath: string, itemIds?: []int, tag?: string, confirmUncertain?: bool}
func (s *Server) handleEnrich(w http.ResponseWriter, r *http.Request) {
	kind := chi.URLParam(r, "kind")
	switch kind {
	case "art", "cheats", "riptopl":
	default:
		writeErr(w, http.StatusNotFound, "kind", "want art, cheats, or riptopl")
		return
	}
	var body struct {
		DestinationPath  string  `json:"destinationPath"`
		ItemIDs          []int64 `json:"itemIds"`
		Tag              string  `json:"tag"`
		ConfirmUncertain bool    `json:"confirmUncertain"`
		MissingOnly      bool    `json:"missingOnly"`
		CheatSource      string  `json:"cheatSource"`
	}
	if err := decodeStrict(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "body", err.Error())
		return
	}
	if body.DestinationPath == "" {
		writeErr(w, http.StatusBadRequest, "destinationPath", "want a destination path")
		return
	}
	dest, err := queue.ResolveDestination(s.qstore, body.DestinationPath)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "", err.Error())
		return
	}
	switch kind {
	case "art":
		s.enrichArt(w, r, dest, body.ItemIDs, body.MissingOnly)
	case "cheats":
		s.enrichCheats(w, r, dest, body.ItemIDs, body.ConfirmUncertain, body.CheatSource)
	case "riptopl":
		s.enrichRiptopl(w, r, dest, body.Tag)
	}
}

// stageArt writes one cover under the destination write gate (spec §2.8:
// enrich output lands strictly between game jobs, never interleaved).
func stageArt(ctx context.Context, disk transfer.Disk, dest *queue.Destination, key string, data []byte) error {
	unlock := queue.LockDestination(dest.Path)
	defer unlock()
	return art.Stage(ctx, disk, dest.Path, dest.BDMPrefix, key, data)
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
			if serr := stageArt(ctx, disk, dest, key, data); serr != nil {
				results = append(results, result{ID: it.ID, Key: key, Status: "failed", Error: serr.Error()})
				continue
			}
			results = append(results, result{ID: it.ID, Key: key, Status: "custom"})
			continue
		}
		if norm, err := art.ValidateAndNormalize(data, it.Platform == library.PlatformPS2); err == nil {
			data = norm
		}
		if serr := stageArt(ctx, disk, dest, key, data); serr != nil {
			results = append(results, result{ID: it.ID, Key: key, Status: "failed", Error: serr.Error()})
			continue
		}
		results = append(results, result{ID: it.ID, Key: key, Status: "staged"})
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

// cheatSourceOrder is the auto winner order (spec §2.9): hand-authored
// files first (highest trust, never overwritten), then the widescreen
// pack, then the title-keyed database. Never merge: one winner per game.
var cheatSourceOrder = []string{"hand", "widescreen", "database"}

func (s *Server) enrichCheats(w http.ResponseWriter, r *http.Request, dest *queue.Destination, itemIDs []int64, confirm bool, source string) {
	switch source {
	case "", "auto", "hand", "widescreen", "database":
	default:
		writeErr(w, http.StatusBadRequest, "cheatSource", "want auto, hand, widescreen or database")
		return
	}
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
	settings, err := config.Load(s.settingsPath)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "", err.Error())
		return
	}
	dbPath := firstNonEmpty(settings.CheatDatabasePath, "CheatDatabase.txt")
	wideDir := firstNonEmpty(settings.WidescreenDir, "widescreen")
	handDir := settings.HandCheatDir
	// Load each source once. Missing sources are not errors: a forced
	// source that finds nothing reports "missing" per item below.
	var wideMap map[string][]byte
	if source == "" || source == "auto" || source == "widescreen" {
		wideMap, _ = cheats.ParseWidescreenDir(wideDir)
	}
	var dbMap map[string]cheats.RawGame
	if source == "" || source == "auto" || source == "database" {
		if raw, err := os.ReadFile(dbPath); err == nil {
			dbMap, _ = cheats.ParseDatabase(raw)
		}
	}
	disk := transfer.FileDisk{}
	ctx := r.Context()
	type result struct {
		ID            int64  `json:"id"`
		GameID        string `json:"gameId"`
		Status        string `json:"status"`
		Source        string `json:"source,omitempty"`
		RegionMatched bool   `json:"regionMatched"`
		EngineSkipped bool   `json:"engineSkipped,omitempty"`
		Error         string `json:"error,omitempty"`
	}
	var results []result
	for _, it := range items {
		if it.Platform != library.PlatformPS2 || it.GameID == "" {
			continue
		}
		matched := cheats.RegionMatches(it.GameID, it.Title)
		content, winner, warns, werr := pickCheatContent(it, source, wideMap, dbMap, handDir)
		if werr != nil {
			results = append(results, result{ID: it.ID, GameID: it.GameID, Status: "missing", RegionMatched: matched, Error: werr.Error()})
			continue
		}
		unlock := queue.LockDestination(dest.Path)
		serr := cheats.Stage(ctx, disk, dest.Path, dest.BDMPrefix, it, content, confirm)
		unlock()
		if serr != nil {
			status := "failed"
			if strings.Contains(serr.Error(), "explicit confirm") {
				status = "needs_confirm"
			}
			results = append(results, result{ID: it.ID, GameID: it.GameID, Status: status, Source: winner, RegionMatched: matched, EngineSkipped: warns.HasEngineSkipped, Error: serr.Error()})
			continue
		}
		results = append(results, result{ID: it.ID, GameID: it.GameID, Status: "staged", Source: winner, RegionMatched: matched, EngineSkipped: warns.HasEngineSkipped})
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

// pickCheatContent selects one winner per game (never merged). source is
// "auto" (hand > widescreen > database) or a forced source; a forced
// source with nothing for this title is "missing", not a fallback.
func pickCheatContent(it library.LibraryItem, source string, wideMap map[string][]byte, dbMap map[string]cheats.RawGame, handDir string) (content, winner string, warns cheats.Warnings, err error) {
	try := func(name string) (string, cheats.Warnings, error) {
		switch name {
		case "hand":
			if handDir == "" {
				return "", cheats.Warnings{}, fmt.Errorf("no hand-cheat dir configured")
			}
			raw, rerr := readHandFile(handDir, it.GameID)
			if rerr != nil {
				return "", cheats.Warnings{}, rerr
			}
			w, verr := cheats.ValidateHand(string(raw))
			if verr != nil {
				return "", w, verr
			}
			return string(raw), w, nil
		case "widescreen":
			data, ok := wideMap[it.GameID]
			if !ok {
				return "", cheats.Warnings{}, fmt.Errorf("no widescreen entry for %s", it.GameID)
			}
			return string(data), cheats.Warnings{}, nil
		case "database":
			g, ok := dbMap[it.Title]
			if !ok {
				return "", cheats.Warnings{}, fmt.Errorf("no CheatDatabase entry for %q", it.Title)
			}
			return cheats.Build(it.GameID, g.Cheats)
		}
		return "", cheats.Warnings{}, fmt.Errorf("unknown cheat source %q", name)
	}
	if source != "" && source != "auto" {
		c, w, e := try(source)
		return c, source, w, e
	}
	var lastErr error
	for _, name := range cheatSourceOrder {
		c, w, e := try(name)
		if e == nil {
			return c, name, w, nil
		}
		lastErr = e
	}
	return "", "", cheats.Warnings{}, lastErr
}

// readHandFile reads <GameID>.cht (or .CHT) from a hand-authored dir.
func readHandFile(dir, gameID string) ([]byte, error) {
	for _, name := range []string{gameID + ".cht", gameID + ".CHT"} {
		if raw, err := os.ReadFile(filepath.Join(dir, name)); err == nil {
			return raw, nil
		}
	}
	return nil, fmt.Errorf("no hand file for %s", gameID)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
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
	unlock := queue.LockDestination(dest.Path)
	st, err := riptopl.Stage(ctx, transfer.FileDisk{}, dest.Path, dest.BDMPrefix, zipPath, nil)
	unlock()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "", err.Error())
		return
	}
	// Record pinned version
	raw, _ := json.Marshal(map[string]any{"tag": rel.Tag, "asset": rel.AssetName, "digest": rel.Digest, "flavour": st.Flavour})
	_ = s.qstore.SetState(fmt.Sprintf("loader.%s", dest.Path), string(raw))
	writeJSON(w, http.StatusOK, map[string]any{
		"tag": rel.Tag, "asset": rel.AssetName, "url": rel.AssetURL, "digest": rel.Digest,
		"flavour": st.Flavour, "elfPath": st.ELFPath,
	})
}
