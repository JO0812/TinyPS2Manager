package api

import (
	"testing"

	"github.com/jo/TinyPS2Manager/internal/queue"
)

func TestJobJSONIncludesAttempts(t *testing.T) {
	got := toJobJSON(queue.Job{Attempts: 2})
	if got.Attempts != 2 {
		t.Fatalf("attempts = %d, want 2", got.Attempts)
	}
}
