// Package api exposes the REST + SSE surface (spec §6.5) served on
// localhost. Handlers are thin: library/queue/transfer do the work, and
// enqueue goes through queue.Estimate before inserting jobs, so the
// DISCS.TXT/VMCDIR.TXT/chunk-count/free-space rules (N4) reject bad plans
// with 422 before any destination write. Progress streams over SSE
// (polled store snapshots, ~1/sec); the executor itself stays DB-driven.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jo/TinyPS2Manager/internal/library"
	"github.com/jo/TinyPS2Manager/internal/queue"
)

// Server wires stores, settings, and the executor to HTTP routes.
type Server struct {
	qstore       *queue.Store
	lib          *library.Store
	settingsPath string
	exec         *queue.Executor
	riptoplBase  string // GitHub API base override (tests); "" = default
	mux          *chi.Mux
}

// New builds the server; StartExecutor runs the background writer.
func New(qstore *queue.Store, lib *library.Store, settingsPath string, exec *queue.Executor) *Server {
	s := &Server{qstore: qstore, lib: lib, settingsPath: settingsPath, exec: exec}
	m := chi.NewRouter()
	m.Use(middleware.Recoverer)
	m.NotFound(func(w http.ResponseWriter, r *http.Request) {
		writeErr(w, http.StatusNotFound, "", "not found")
	})
	m.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		writeErr(w, http.StatusMethodNotAllowed, "", "method not allowed")
	})
	m.Route("/api", func(r chi.Router) {
		r.Get("/library", s.handleLibraryList)
		r.Post("/library/import", s.handleLibraryImport)
		r.Patch("/library/{id}", s.handleLibraryPatch)
		r.Get("/library/{id}/enrichment", s.handleLibraryEnrichment)
		r.Get("/destinations", s.handleDestinationsList)
		r.Get("/destinations/volumes", s.handleVolumesList)
		r.Post("/destinations", s.handleDestinationsCreate)
		r.Patch("/destinations/{id}", s.handleDestinationsPatch)
		r.Delete("/destinations/{id}", s.handleDestinationsDelete)
		r.Post("/destinations/{id}/prepare", s.handlePrepare)
		r.Get("/destinations/{id}/preflight", s.handlePreflight)
		r.Post("/queue", s.handleQueueEnqueue)
		r.Get("/queue", s.handleQueueList)
		r.Patch("/queue/{jobId}", s.handleQueuePatch)
		r.Post("/queue/pause", s.handleQueuePause)
		r.Post("/queue/resume", s.handleQueueResume)
		r.Get("/queue/events", s.handleEvents)
		r.Post("/enrich/{kind}", s.handleEnrich)
		r.Get("/settings", s.handleSettingsGet)
		r.Put("/settings", s.handleSettingsPut)
	})
	s.mux = m
	return s
}

// Handler serves the API.
func (s *Server) Handler() http.Handler { return s.mux }

// WithRiptoplBase overrides the GitHub API base (hermetic tests).
func (s *Server) WithRiptoplBase(base string) *Server {
	s.riptoplBase = base
	return s
}

// StartExecutor runs the writer supervisor in the background.
func (s *Server) StartExecutor(ctx context.Context) {
	go func() { _ = s.exec.Run(ctx) }()
}

// --- JSON plumbing ---

type fieldError struct {
	Field string `json:"field"`
	Msg   string `json:"msg"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, field, msg string) {
	writeJSON(w, status, map[string]any{
		"errors": []fieldError{{Field: field, Msg: msg}},
	})
}

func writeErrs(w http.ResponseWriter, status int, errs []fieldError) {
	writeJSON(w, status, map[string]any{"errors": errs})
}

// decodeStrict parses JSON with unknown fields rejected.
func decodeStrict(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("bad JSON: %w", err)
	}
	return nil
}

// pathID parses a chi URL integer parameter.
func pathID(r *http.Request, name string) (int64, error) {
	v, err := strconv.ParseInt(chi.URLParam(r, name), 10, 64)
	if err != nil || v <= 0 {
		return 0, fmt.Errorf("bad %s %q", name, chi.URLParam(r, name))
	}
	return v, nil
}

// --- DTOs (spec §8 shapes, camelCase) ---

type libraryItemJSON struct {
	ID              int64  `json:"id"`
	SourcePath      string `json:"sourcePath"`
	ContentHash     string `json:"contentHash"`
	Platform        string `json:"platform"`
	DiscType        string `json:"discType"`
	DetectionMethod string `json:"detectionMethod"`
	Title           string `json:"title"`
	DiscIndex       int    `json:"discIndex"`
	DiscGroupID     *int64 `json:"discGroupId"`
	SizeBytes       int64  `json:"sizeBytes"`
	Status          string `json:"status"`
	GameID          string `json:"gameId"`
	GameIDUncertain bool   `json:"gameIdUncertain"`
}

func toLibraryItemJSON(it library.LibraryItem) libraryItemJSON {
	return libraryItemJSON{
		ID: it.ID, SourcePath: it.SourcePath, ContentHash: it.ContentHash,
		Platform: string(it.Platform), DiscType: string(it.DiscType),
		DetectionMethod: string(it.DetectionMethod), Title: it.Title,
		DiscIndex: it.DiscIndex, DiscGroupID: it.DiscGroupID,
		SizeBytes: it.SizeBytes, Status: string(it.Status),
		GameID: it.GameID, GameIDUncertain: it.GameIDUncertain,
	}
}

type jobJSON struct {
	ID            int64  `json:"id"`
	LibraryItemID int64  `json:"libraryItemId"`
	DestinationID int64  `json:"destinationId"`
	Kind          string `json:"kind"`
	Order         int    `json:"order"`
	Status        string `json:"status"`
	Phase         string `json:"phase"`
	BytesTotal    int64  `json:"bytesTotal"`
	BytesDone     int64  `json:"bytesDone"`
	Error         string `json:"error"`
	Attempts      int    `json:"attempts"`
	CreatedAt     string `json:"createdAt"`
	UpdatedAt     string `json:"updatedAt"`
}

func toJobJSON(j queue.Job) jobJSON {
	return jobJSON{
		ID: j.ID, LibraryItemID: j.LibraryItemID, DestinationID: j.DestinationID,
		Kind: string(j.Kind), Order: j.Order, Status: string(j.Status),
		Phase: j.Phase, BytesTotal: j.BytesTotal, BytesDone: j.BytesDone,
		Error: j.Error, Attempts: j.Attempts, CreatedAt: j.CreatedAt, UpdatedAt: j.UpdatedAt,
	}
}

type destinationJSON struct {
	ID         int64  `json:"id"`
	Path       string `json:"path"`
	Kind       string `json:"kind"`
	Filesystem string `json:"filesystem"`
	FSOverride string `json:"fsOverride"`
	BDMPrefix  string `json:"bdmPrefix"`
	FreeBytes  int64  `json:"freeBytes"`
	TotalBytes int64  `json:"totalBytes"`
	Reachable  bool   `json:"reachable"`
	UpdatedAt  string `json:"updatedAt"`
}

// reachable reports whether the destination path exists right now. A
// plain os.Stat (no Statfs, which can block on stale mounts): false
// means unplugged or deleted, and the UI says so instead of showing
// "-1 B free" with no explanation.
func reachable(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func toDestinationJSON(d queue.Destination) destinationJSON {
	return destinationJSON{
		ID: d.ID, Path: d.Path, Kind: string(d.Kind), Filesystem: d.Filesystem,
		FSOverride: d.FSOverride, BDMPrefix: d.BDMPrefix,
		FreeBytes: d.FreeBytes, TotalBytes: d.TotalBytes,
		Reachable: reachable(d.Path), UpdatedAt: d.UpdatedAt,
	}
}
