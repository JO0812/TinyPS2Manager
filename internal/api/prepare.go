package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/jo/TinyPS2Manager/internal/library"
	"github.com/jo/TinyPS2Manager/internal/queue"
	"github.com/jo/TinyPS2Manager/internal/riptopl"
	"github.com/jo/TinyPS2Manager/internal/transfer"
)

// riptoplClient builds the release client, honoring the test override.
func (s *Server) riptoplClient() *riptopl.Client {
	return &riptopl.Client{APIBase: s.riptoplBase}
}

type prepareRequest struct {
	Mode       string  `json:"mode"` // preview | execute
	ItemIDs    []int64 `json:"itemIds"`
	RiptoplTag string  `json:"riptoplTag"` // "" = skip the loader
	Flavour    string  `json:"flavour"`    // "" = preference order
	Kind       *string `json:"kind"`       // optional: "copy-ps1-ember" for PS1 Ember preview
}

type riptoplPreview struct {
	Tag       string   `json:"tag"`
	Asset     string   `json:"asset"`
	URL       string   `json:"url"`
	SizeBytes int64    `json:"sizeBytes"`
	Digest    string   `json:"digest"`
	Flavours  []string `json:"flavours"`
}

type preparePreview struct {
	Dirs      []string        `json:"dirs"`
	Files     []string        `json:"files"`
	Riptopl   *riptoplPreview `json:"riptopl,omitempty"`
	Warnings  []string        `json:"warnings"`
	Checklist []string        `json:"checklist"`
}

type stagedLoader struct {
	Tag      string `json:"tag"`
	Asset    string `json:"asset"`
	Digest   string `json:"digest"`
	Flavour  string `json:"flavour"`
	ELFPath  string `json:"elfPath"`
	ELFSize  int64  `json:"elfSize"`
	StagedAt string `json:"stagedAt"`
}

type prepareResult struct {
	Jobs      []jobJSON     `json:"jobs"`
	Riptopl   *stagedLoader `json:"riptopl,omitempty"`
	Dirs      []string      `json:"dirs"`
	Files     []string      `json:"files"`
	Checklist []string      `json:"checklist"`
}

func (s *Server) handlePrepare(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "id", err.Error())
		return
	}
	var body prepareRequest
	if err := decodeStrict(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "body", err.Error())
		return
	}
	if body.Mode != "preview" && body.Mode != "execute" {
		writeErr(w, http.StatusBadRequest, "mode", "want \"preview\" or \"execute\"")
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
	if fi, err := os.Stat(dest.Path); err != nil || !fi.IsDir() {
		writeErr(w, http.StatusUnprocessableEntity, "id", "destination path is gone")
		return
	}

	if body.Mode == "preview" {
		preview, apiErr := s.previewPrepare(r.Context(), dest, &body)
		if apiErr != nil {
			writeErr(w, apiErr.status, apiErr.field, apiErr.msg)
			return
		}
		writeJSON(w, http.StatusOK, preview)
		return
	}
	result, apiErr := s.executePrepare(r.Context(), dest, &body)
	if apiErr != nil {
		writeErr(w, apiErr.status, apiErr.field, apiErr.msg)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// dirOf returns a file path's parent directory.
func dirOf(p string) string { return filepath.Dir(p) }

// treePreview dry-runs the destination tree for items: per-item estimate
// errors become warnings (the set may still be useful), never writes.
// If forcedKind is KindEmberCopy, PS1 items use the Ember layout.
func (s *Server) treePreview(dest *queue.Destination, itemIDs []int64, forcedKind queue.JobKind) (dirs, files []string, warnings []string) {
	dirSet, fileSet := map[string]bool{}, map[string]bool{}
	addFile := func(p string) { fileSet[p] = true }
	addDir := func(p string) { dirSet[p] = true }
	for _, itemID := range itemIDs {
		it, err := s.lib.Get(itemID)
		if err != nil || it == nil {
			warnings = append(warnings, fmt.Sprintf("item %d: not found", itemID))
			continue
		}
		var kind queue.JobKind
		var estErr error
		if forcedKind == queue.KindEmberCopy && it.Platform == library.PlatformPS1 {
			kind, _, estErr = queue.EstimateEmber(it, dest)
		} else {
			kind, _, estErr = queue.Estimate(it, dest, s.lib)
		}
		if estErr != nil {
			warnings = append(warnings, fmt.Sprintf("%s: %v", it.Title, estErr))
			continue
		}
		switch kind {
		case queue.KindCopy:
			cp, err := queue.PreviewCopy(it, dest)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("%s: %v", it.Title, err))
				continue
			}
			addFile(cp.DestPath)
			addDir(dirOf(cp.DestPath))
		case queue.KindConvertCopy:
			cp, err := queue.PreviewConvert(it, dest, s.lib)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("%s: %v", it.Title, err))
				continue
			}
			addFile(cp.VCDPath)
			addDir(cp.PopsDir)
			for _, m := range cp.Manifests {
				addFile(m.Path)
				addDir(m.Dir)
			}
			for _, d := range cp.VMCDirs {
				addDir(d)
			}
		case queue.KindSplitAndCopy:
			sp, err := queue.PreviewSplit(it, dest)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("%s: %v", it.Title, err))
				continue
			}
			addFile(sp.ULCfg)
			for _, c := range sp.Chunks {
				addFile(c)
			}
			addDir(sp.Root)
		case queue.KindEmberCopy:
			ep, err := queue.PreviewEmber(it, dest)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("%s: %v", it.Title, err))
				continue
			}
			for _, f := range ep.Files {
				addFile(f)
			}
			addDir(ep.DestDir)
		default:
			warnings = append(warnings, fmt.Sprintf("%s: kind %s not plannable", it.Title, kind))
		}
	}
	for d := range dirSet {
		dirs = append(dirs, d)
	}
	for f := range fileSet {
		files = append(files, f)
	}
	sort.Strings(dirs)
	sort.Strings(files)
	return dirs, files, warnings
}

func (s *Server) previewPrepare(ctx context.Context, dest *queue.Destination, body *prepareRequest) (*preparePreview, *apiError) {
	forced := queue.JobKind("")
	if body.Kind != nil {
		forced = queue.JobKind(*body.Kind)
	}
	dirs, files, warnings := s.treePreview(dest, body.ItemIDs, forced)
	pv := &preparePreview{Dirs: dirs, Files: files, Warnings: warnings, Checklist: riptopl.Checklist()}
	if body.RiptoplTag == "" {
		return pv, nil
	}
	rel, err := s.riptoplClient().Resolve(ctx, body.RiptoplTag)
	if err != nil {
		return nil, &apiError{status: http.StatusBadGateway, field: "riptoplTag", msg: err.Error()}
	}
	flavours := riptopl.FlavourPreference
	if body.Flavour != "" {
		flavours = []string{body.Flavour}
	}
	pv.Riptopl = &riptoplPreview{
		Tag: rel.Tag, Asset: rel.AssetName, URL: rel.AssetURL,
		SizeBytes: rel.AssetSize, Digest: rel.Digest, Flavours: flavours,
	}
	return pv, nil
}

func (s *Server) executePrepare(ctx context.Context, dest *queue.Destination, body *prepareRequest) (*prepareResult, *apiError) {
	fail := func(status int, field, msg string) (*prepareResult, *apiError) {
		return nil, &apiError{status: status, field: field, msg: msg}
	}
	// Validate everything before writing anything.
	forced := queue.JobKind("")
	if body.Kind != nil {
		forced = queue.JobKind(*body.Kind)
	}
	for _, itemID := range body.ItemIDs {
		it, err := s.lib.Get(itemID)
		if err != nil {
			return fail(http.StatusInternalServerError, "", err.Error())
		}
		if it == nil {
			return fail(http.StatusNotFound, "itemIds", "no such library item")
		}
		var estErr error
		if forced == queue.KindEmberCopy && it.Platform == library.PlatformPS1 {
			_, _, estErr = queue.EstimateEmber(it, dest)
		} else {
			_, _, estErr = queue.Estimate(it, dest, s.lib)
		}
		if estErr != nil {
			return fail(http.StatusUnprocessableEntity, "itemIds", estErr.Error())
		}
	}
	if dest.EffectiveFilesystem() != "fat32" && dest.EffectiveFilesystem() != "exfat" {
		return fail(http.StatusUnprocessableEntity, "destination",
			"filesystem unknown: set an explicit FAT32/exFAT choice first")
	}

	dirs, files, warnings := s.treePreview(dest, body.ItemIDs, forced)
	_ = warnings // execute validated cleanly above; preview warnings are moot
	disk := transfer.FileDisk{}
	for _, d := range dirs {
		if err := disk.MkdirAll(d); err != nil {
			return fail(http.StatusInternalServerError, "", err.Error())
		}
	}

	res := &prepareResult{Dirs: dirs, Files: files, Checklist: riptopl.Checklist()}
	if body.RiptoplTag != "" {
		staged, apiErr := s.stageLoader(ctx, dest, body)
		if apiErr != nil {
			return fail(apiErr.status, apiErr.field, apiErr.msg)
		}
		res.Riptopl = staged
	}

	jobs, apiErr := s.enqueueItems(dest.ID, body.ItemIDs, forced)
	if apiErr != nil {
		return fail(apiErr.status, apiErr.field, apiErr.msg)
	}
	for _, j := range jobs {
		res.Jobs = append(res.Jobs, toJobJSON(j))
	}
	if res.Jobs == nil {
		res.Jobs = []jobJSON{}
	}
	return res, nil
}

// stageLoader downloads, verifies, stages, and records the loader build.
// The release URL was already shown at preview time (§9.7).
func (s *Server) stageLoader(ctx context.Context, dest *queue.Destination, body *prepareRequest) (*stagedLoader, *apiError) {
	fail := func(err error) (*stagedLoader, *apiError) {
		return nil, &apiError{status: http.StatusBadGateway, field: "riptoplTag", msg: err.Error()}
	}
	client := s.riptoplClient()
	rel, err := client.Resolve(ctx, body.RiptoplTag)
	if err != nil {
		return fail(err)
	}
	zipPath, err := client.Download(ctx, rel, nil)
	if err != nil {
		return fail(err)
	}
	defer func() { _ = os.Remove(zipPath) }()
	flavours := riptopl.FlavourPreference
	if body.Flavour != "" {
		flavours = []string{body.Flavour}
	}
	st, err := riptopl.Stage(ctx, transfer.FileDisk{}, dest.Path, dest.BDMPrefix, zipPath, flavours)
	if err != nil {
		return fail(err)
	}
	rec := stagedLoader{
		Tag: rel.Tag, Asset: rel.AssetName, Digest: rel.Digest,
		Flavour: st.Flavour, ELFPath: st.ELFPath, ELFSize: st.ELFSize,
		StagedAt: time.Now().UTC().Format(time.RFC3339),
	}
	raw, _ := json.Marshal(rec)
	if err := s.qstore.SetState(fmt.Sprintf("loader.%d", dest.ID), string(raw)); err != nil {
		return nil, &apiError{status: http.StatusInternalServerError, field: "", msg: err.Error()}
	}
	// Re-run the tree preview so the response lists the staged ELF too.
	return &rec, nil
}
