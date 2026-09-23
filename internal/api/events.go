package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// progressEvent is the SSE payload (plan §4.4): per-job phase, bytes,
// ETA, and a human message.
type progressEvent struct {
	JobID      int64  `json:"jobId"`
	Phase      string `json:"phase"`
	BytesDone  int64  `json:"bytesDone"`
	BytesTotal int64  `json:"bytesTotal"`
	ETAsec     int64  `json:"etaSec"`
	Message    string `json:"message"`
}

// eventTick is the snapshot cadence (≤10/sec server-side per spec §7,
// with wide margin: the UI additionally throttles client-side).
const eventTick = time.Second

type jobSig struct {
	status string
	phase  string
	done   int64
}

type jobSample struct {
	done int64
	at   time.Time
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "", "streaming unsupported")
		return
	}
	last := map[int64]jobSig{}
	prev := map[int64]jobSample{}
	seen := map[int64]bool{}
	emit := func(ev progressEvent) bool {
		if _, err := fmt.Fprintf(w, "data: %s\n\n", mustJSON(ev)); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}
	snapshot := func() bool {
		jobs, err := s.qstore.ListJobs()
		if err != nil {
			return true // transient DB hiccup: skip this tick
		}
		now := time.Now()
		live := map[int64]bool{}
		for _, j := range jobs {
			live[j.ID] = true
			seen[j.ID] = true
			sig := jobSig{status: string(j.Status), phase: j.Phase, done: j.BytesDone}
			eta := int64(-1)
			if p, ok := prev[j.ID]; ok && j.BytesDone > p.done {
				dt := now.Sub(p.at).Seconds()
				rate := float64(j.BytesDone-p.done) / dt
				if rate > 0 && j.BytesTotal > j.BytesDone {
					eta = int64(float64(j.BytesTotal-j.BytesDone) / rate)
				} else if j.BytesTotal <= j.BytesDone {
					eta = 0
				}
			}
			prev[j.ID] = jobSample{done: j.BytesDone, at: now}
			if last[j.ID] == sig {
				continue
			}
			last[j.ID] = sig
			msg := string(j.Status)
			if j.Status == "error" && j.Error != "" {
				msg = j.Error
			}
			if !emit(progressEvent{
				JobID: j.ID, Phase: j.Phase, BytesDone: j.BytesDone,
				BytesTotal: j.BytesTotal, ETAsec: eta, Message: msg,
			}) {
				return false
			}
		}
		for id := range seen {
			if !live[id] {
				delete(seen, id)
				delete(last, id)
				delete(prev, id)
				if !emit(progressEvent{JobID: id, Message: "removed"}) {
					return false
				}
			}
		}
		return true
	}
	if !snapshot() {
		return
	}
	tick := time.NewTicker(eventTick)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-tick.C:
			if !snapshot() {
				return
			}
		}
	}
}

func mustJSON(v any) string {
	// Progress events only carry strings/ints; encoding cannot fail.
	raw, err := json.Marshal(v)
	if err != nil {
		return `{"message":"encode error"}`
	}
	return string(raw)
}
