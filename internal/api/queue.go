package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/jo/TinyPS2Manager/internal/queue"
)

func (s *Server) handleQueueEnqueue(w http.ResponseWriter, r *http.Request) {
	var body struct {
		DestinationID int64   `json:"destinationId"`
		ItemIDs       []int64 `json:"itemIds"`
	}
	if err := decodeStrict(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "body", err.Error())
		return
	}
	jobs, apiErr := s.enqueueItems(body.DestinationID, body.ItemIDs)
	if apiErr != nil {
		writeErr(w, apiErr.status, apiErr.field, apiErr.msg)
		return
	}
	out := make([]jobJSON, 0, len(jobs))
	for _, j := range jobs {
		out = append(out, toJobJSON(j))
	}
	writeJSON(w, http.StatusCreated, out)
}

// apiError is a validation failure with its HTTP mapping.
type apiError struct {
	status int
	field  string
	msg    string
}

func (e *apiError) Error() string { return e.msg }

// enqueueItems validates (N4: manifests, chunks, fs, free space — all
// through the executor's own Estimate planners) and inserts jobs. Shared
// by POST /api/queue and the prepare-execute flow.
func (s *Server) enqueueItems(destinationID int64, itemIDs []int64) ([]queue.Job, *apiError) {
	fail := func(status int, field, msg string) ([]queue.Job, *apiError) {
		return nil, &apiError{status: status, field: field, msg: msg}
	}
	if destinationID <= 0 {
		return fail(http.StatusBadRequest, "destinationId", "want a positive id")
	}
	if len(itemIDs) == 0 {
		return fail(http.StatusBadRequest, "itemIds", "want at least one item id")
	}
	dest, err := s.qstore.GetDestination(destinationID)
	if err != nil {
		return fail(http.StatusInternalServerError, "", err.Error())
	}
	if dest == nil {
		return fail(http.StatusNotFound, "destinationId", "no such destination")
	}
	if dest.EffectiveFilesystem() != "fat32" && dest.EffectiveFilesystem() != "exfat" {
		return fail(http.StatusUnprocessableEntity, "destination",
			"filesystem unknown: set an explicit FAT32/exFAT choice first")
	}

	type staged struct {
		kind  queue.JobKind
		total int64
		item  int64
	}
	var plan []staged
	var need int64
	for _, itemID := range itemIDs {
		if itemID <= 0 {
			return fail(http.StatusBadRequest, "itemIds", "bad id")
		}
		it, err := s.lib.Get(itemID)
		if err != nil {
			return fail(http.StatusInternalServerError, "", err.Error())
		}
		if it == nil {
			return fail(http.StatusNotFound, "itemIds", "no such library item")
		}
		kind, total, err := queue.Estimate(it, dest, s.lib)
		if err != nil {
			return fail(http.StatusUnprocessableEntity, "itemIds", err.Error())
		}
		plan = append(plan, staged{kind: kind, total: total, item: itemID})
		need += total
	}
	inFlight, err := s.qstore.InFlightBytes(dest.ID)
	if err != nil {
		return fail(http.StatusInternalServerError, "", err.Error())
	}
	if dest.FreeBytes >= 0 && need+inFlight > dest.FreeBytes {
		return fail(http.StatusUnprocessableEntity, "destination", "not enough free space")
	}

	var rows []queue.Job
	for _, p := range plan {
		rows = append(rows, queue.Job{
			LibraryItemID: p.item, DestinationID: dest.ID,
			Kind: p.kind, BytesTotal: p.total,
		})
	}
	jobs, err := s.qstore.Enqueue(rows)
	if err != nil {
		return fail(http.StatusInternalServerError, "", err.Error())
	}
	return jobs, nil
}

func (s *Server) handleQueueList(w http.ResponseWriter, r *http.Request) {
	jobs, err := s.qstore.ListJobs()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "", err.Error())
		return
	}
	out := make([]jobJSON, 0, len(jobs))
	for _, j := range jobs {
		out = append(out, toJobJSON(j))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleQueuePatch(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "jobId")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "jobId", err.Error())
		return
	}
	var body struct {
		Action *string `json:"action"`
		Order  *int    `json:"order"`
	}
	if err := decodeStrict(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "body", err.Error())
		return
	}
	if (body.Action == nil) == (body.Order == nil) {
		writeErr(w, http.StatusBadRequest, "body", "want exactly one of action, order")
		return
	}
	if body.Order != nil {
		if err := s.qstore.Reorder(id, *body.Order); err != nil {
			writeErr(w, http.StatusNotFound, "order", err.Error())
			return
		}
		job, err := s.qstore.GetJob(id)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, toJobJSON(*job))
		return
	}
	switch *body.Action {
	case "retry":
		if err := s.qstore.RetryJob(id); err != nil {
			writeErr(w, http.StatusConflict, "action", err.Error())
			return
		}
	case "pause":
		if err := s.qstore.PauseJob(id); err != nil {
			writeErr(w, http.StatusConflict, "action", err.Error())
			return
		}
	case "resume":
		if err := s.qstore.ResumeJob(id); err != nil {
			writeErr(w, http.StatusConflict, "action", err.Error())
			return
		}
	case "skip":
		if err := s.qstore.CancelJob(id); err != nil {
			writeErr(w, http.StatusConflict, "action", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
		return
	case "cancel":
		if err := s.cancelJob(id); err != nil {
			writeErr(w, http.StatusConflict, "action", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
		return
	default:
		writeErr(w, http.StatusBadRequest, "action",
			"want retry, skip, pause, resume, or cancel")
		return
	}
	job, err := s.qstore.GetJob(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "", err.Error())
		return
	}
	if job == nil {
		writeErr(w, http.StatusNotFound, "jobId", "no such job")
		return
	}
	writeJSON(w, http.StatusOK, toJobJSON(*job))
}

// cancelJob aborts a running job via the executor (waiting for it to stop)
// then deletes it; non-running jobs delete directly.
func (s *Server) cancelJob(id int64) error {
	job, err := s.qstore.GetJob(id)
	if err != nil {
		return err
	}
	if job == nil {
		return fmt.Errorf("no such job")
	}
	if job.Status == queue.JobRunning && s.exec != nil {
		if !s.exec.CancelDestination(job.DestinationID) {
			return fmt.Errorf("executor is not running this job")
		}
		deadline := time.Now().Add(10 * time.Second)
		for {
			cur, err := s.qstore.GetJob(id)
			if err != nil {
				return err
			}
			if cur == nil || cur.Status != queue.JobRunning {
				break
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("job did not stop in time")
			}
			time.Sleep(50 * time.Millisecond)
		}
		return s.qstore.DeleteJob(id)
	}
	return s.qstore.CancelJob(id)
}

func (s *Server) handleQueuePause(w http.ResponseWriter, r *http.Request) {
	if err := s.qstore.SetPaused(true); err != nil {
		writeErr(w, http.StatusInternalServerError, "", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"paused": true})
}

func (s *Server) handleQueueResume(w http.ResponseWriter, r *http.Request) {
	if err := s.qstore.SetPaused(false); err != nil {
		writeErr(w, http.StatusInternalServerError, "", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"paused": false})
}
