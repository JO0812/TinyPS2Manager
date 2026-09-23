package api

import (
	"encoding/json"
	"net/http"
	"os"

	"github.com/jo/TinyPS2Manager/internal/config"
	"github.com/jo/TinyPS2Manager/internal/library"
	"github.com/jo/TinyPS2Manager/internal/oplfs"
	"github.com/jo/TinyPS2Manager/internal/queue"
	"github.com/jo/TinyPS2Manager/internal/transfer"
)

func (s *Server) handleLibraryList(w http.ResponseWriter, r *http.Request) {
	items, err := s.lib.List()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "", err.Error())
		return
	}
	out := make([]libraryItemJSON, 0, len(items))
	for _, it := range items {
		out = append(out, toLibraryItemJSON(it))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleLibraryImport(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
	}
	if err := decodeStrict(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "body", err.Error())
		return
	}
	if body.Path == "" {
		writeErr(w, http.StatusBadRequest, "path", "path is required")
		return
	}
	if fi, err := os.Stat(body.Path); err != nil || !fi.IsDir() {
		writeErr(w, http.StatusBadRequest, "path", "not a readable directory")
		return
	}
	titledb, err := library.LoadBundled()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "", err.Error())
		return
	}
	if userPath, err := library.DefaultUserDBPath(); err == nil {
		if err := titledb.MergeUser(userPath); err != nil {
			writeErr(w, http.StatusInternalServerError, "", err.Error())
			return
		}
	}
	items, err := library.ImportDir(s.lib, body.Path, titledb)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "", err.Error())
		return
	}
	out := make([]libraryItemJSON, 0, len(items))
	for _, it := range items {
		out = append(out, toLibraryItemJSON(it))
	}
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) handleLibraryPatch(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "id", err.Error())
		return
	}
	var raw map[string]json.RawMessage
	if err := decodeStrict(r, &raw); err != nil {
		writeErr(w, http.StatusBadRequest, "body", err.Error())
		return
	}
	if len(raw) == 0 {
		writeErr(w, http.StatusBadRequest, "body", "nothing to update")
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
	for key, val := range raw {
		switch key {
		case "discType":
			var dt string
			if err := json.Unmarshal(val, &dt); err != nil {
				writeErr(w, http.StatusBadRequest, "discType", "want \"cd\" or \"dvd\"")
				return
			}
			if dt != "cd" && dt != "dvd" {
				writeErr(w, http.StatusBadRequest, "discType", "want \"cd\" or \"dvd\"")
				return
			}
			if err := s.lib.SetDiscOverride(it.ContentHash, library.DiscType(dt)); err != nil {
				writeErr(w, http.StatusInternalServerError, "", err.Error())
				return
			}
		case "title":
			var title string
			if err := json.Unmarshal(val, &title); err != nil || title == "" {
				writeErr(w, http.StatusBadRequest, "title", "want a non-empty string")
				return
			}
			if err := s.lib.UpdateTitle(id, title); err != nil {
				writeErr(w, http.StatusInternalServerError, "", err.Error())
				return
			}
		case "discGroupId":
			var group *int64
			if string(val) != "null" {
				var g int64
				if err := json.Unmarshal(val, &g); err != nil || g <= 0 {
					writeErr(w, http.StatusBadRequest, "discGroupId", "want a positive id or null")
					return
				}
				group = &g
			}
			if err := s.lib.SetGroup(id, group); err != nil {
				writeErr(w, http.StatusInternalServerError, "", err.Error())
				return
			}
		default:
			writeErr(w, http.StatusBadRequest, key, "unknown field")
			return
		}
	}
	updated, err := s.lib.Get(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toLibraryItemJSON(*updated))
}

func (s *Server) handleDestinationsList(w http.ResponseWriter, r *http.Request) {
	dests, err := s.qstore.ListDestinations()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "", err.Error())
		return
	}
	out := make([]destinationJSON, 0, len(dests))
	for _, d := range dests {
		probed, err := transfer.Probe(d.Path)
		if err == nil {
			_ = s.qstore.RefreshDestinationStats(d.ID, string(probed.Filesystem), probed.FreeBytes, probed.TotalBytes)
			d.Filesystem, d.FreeBytes, d.TotalBytes = string(probed.Filesystem), probed.FreeBytes, probed.TotalBytes
		}
		out = append(out, toDestinationJSON(d))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleDestinationsCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path               string `json:"path"`
		Kind               string `json:"kind"`
		FilesystemOverride string `json:"filesystemOverride"`
		BDMPrefix          string `json:"bdmPrefix"`
	}
	if err := decodeStrict(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "body", err.Error())
		return
	}
	if body.Path == "" {
		writeErr(w, http.StatusBadRequest, "path", "path is required")
		return
	}
	if fi, err := os.Stat(body.Path); err != nil || !fi.IsDir() {
		writeErr(w, http.StatusBadRequest, "path", "not an existing directory")
		return
	}
	kind := queue.DestinationKind(body.Kind)
	if body.Kind == "" {
		kind = queue.DestFolder
	}
	if kind != queue.DestDrive && kind != queue.DestFolder {
		writeErr(w, http.StatusBadRequest, "kind", "want \"drive\" or \"folder\"")
		return
	}
	if body.FilesystemOverride != "" && body.FilesystemOverride != "fat32" &&
		body.FilesystemOverride != "exfat" {
		writeErr(w, http.StatusBadRequest, "filesystemOverride", "want \"fat32\" or \"exfat\"")
		return
	}
	if err := oplfs.ValidateBDMPrefix(body.BDMPrefix); err != nil {
		writeErr(w, http.StatusBadRequest, "bdmPrefix", err.Error())
		return
	}
	probed, _ := transfer.Probe(body.Path)
	d, err := s.qstore.AddDestination(queue.Destination{
		Path: body.Path, Kind: kind, Filesystem: string(probed.Filesystem),
		FSOverride: body.FilesystemOverride, BDMPrefix: body.BDMPrefix,
		FreeBytes: probed.FreeBytes, TotalBytes: probed.TotalBytes,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, toDestinationJSON(d))
}

func (s *Server) handleDestinationsPatch(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "id", err.Error())
		return
	}
	var raw map[string]json.RawMessage
	if err := decodeStrict(r, &raw); err != nil {
		writeErr(w, http.StatusBadRequest, "body", err.Error())
		return
	}
	if len(raw) == 0 {
		writeErr(w, http.StatusBadRequest, "body", "nothing to update")
		return
	}
	d, err := s.qstore.GetDestination(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "", err.Error())
		return
	}
	if d == nil {
		writeErr(w, http.StatusNotFound, "id", "no such destination")
		return
	}
	for key, val := range raw {
		switch key {
		case "bdmPrefix":
			var prefix string
			if err := json.Unmarshal(val, &prefix); err != nil {
				writeErr(w, http.StatusBadRequest, "bdmPrefix", "want a string")
				return
			}
			if err := oplfs.ValidateBDMPrefix(prefix); err != nil {
				writeErr(w, http.StatusBadRequest, "bdmPrefix", err.Error())
				return
			}
			if err := s.qstore.UpdateDestinationPrefix(id, prefix); err != nil {
				writeErr(w, http.StatusInternalServerError, "", err.Error())
				return
			}
		case "filesystemOverride":
			var ov string
			if err := json.Unmarshal(val, &ov); err != nil ||
				(ov != "" && ov != "fat32" && ov != "exfat") {
				writeErr(w, http.StatusBadRequest, "filesystemOverride", "want \"\", \"fat32\" or \"exfat\"")
				return
			}
			if err := s.qstore.UpdateDestinationOverride(id, ov); err != nil {
				writeErr(w, http.StatusInternalServerError, "", err.Error())
				return
			}
		default:
			writeErr(w, http.StatusBadRequest, key, "unknown field")
			return
		}
	}
	updated, err := s.qstore.GetDestination(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toDestinationJSON(*updated))
}

func (s *Server) handleSettingsGet(w http.ResponseWriter, r *http.Request) {
	settings, err := config.Load(s.settingsPath)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (s *Server) handleSettingsPut(w http.ResponseWriter, r *http.Request) {
	var settings config.Settings
	if err := decodeStrict(r, &settings); err != nil {
		writeErr(w, http.StatusBadRequest, "body", err.Error())
		return
	}
	if err := config.Save(s.settingsPath, settings); err != nil {
		writeErr(w, http.StatusBadRequest, "body", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, settings)
}
