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
	if body.DestinationID <= 0 {
		writeErr(w, http.StatusBadRequest, "destinationId", "want a positive id")
		return
	}
	if len(body.ItemIDs) == 0 {
		writeErr(w, http.StatusBadRequest, "itemIds", "want at least one item id")
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
	if dest.EffectiveFilesystem() != "fat32" && dest.EffectiveFilesystem() != "exfat" {
		writeErr(w, http.StatusUnprocessableEntity, "destination",
			"filesystem unknown: set an explicit FAT32/exFAT choice first")
		return
	}

	// Validation-first (N4): derive every kind + total through the same
	// planners the executor runs, so manifest/chunk/size violations fail
	// here with 422 instead of mid-queue.
	type staged struct {
		kind  queue.JobKind
		total int64
		item  int64
	}
	var plan []staged
	var need int64
	for _, itemID := range body.ItemIDs {
		if itemID <= 0 {
			writeErr(w, http.StatusBadRequest, "itemIds", fmt.Sprintf("bad id %d", itemID))
			return
		}
		it, err := s.lib.Get(itemID)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "", err.Error())
			return
		}
		if it == nil {
			writeErr(w, http.StatusNotFound, "itemIds", fmt.Sprintf("no library item %d", itemID))
			return
		}
		kind, total, err := queue.Estimate(it, dest, s.lib)
		if err != nil {
			writeErr(w, http.StatusUnprocessableEntity, "itemIds",
				fmt.Sprintf("item %d: %v", itemID, err))
			return
		}
		plan = append(plan, staged{kind: kind, total: total, item: itemID})
		need += total
	}
	inFlight, err := s.qstore.InFlightBytes(dest.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "", err.Error())
		return
	}
	if dest.FreeBytes >= 0 && need+inFlight > dest.FreeBytes {
		writeErr(w, http.StatusUnprocessableEntity, "destination",
			fmt.Sprintf("needs %d bytes, %d free (%d in flight)", need, dest.FreeBytes, inFlight))
		return
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
		writeErr(w, http.StatusInternalServerError, "", err.Error())
		return
	}
	out := make([]jobJSON, 0, len(jobs))
	for _, j := range jobs {
		out = append(out, toJobJSON(j))
	}
	writeJSON(w, http.StatusCreated, out)
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
