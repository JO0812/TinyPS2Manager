package logging

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSinkWritesJSON(t *testing.T) {
	dir := t.TempDir()
	sink, err := open(dir, time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC), MaxBytes, KeepFiles)
	if err != nil {
		t.Fatal(err)
	}
	fixed := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	sink.writer.clock = func() time.Time { return fixed }
	sink.Logger().Info("job started", "job_id", 7)
	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(filepath.Join(dir, "oplbm-20260923.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var record map[string]any
	if err := json.NewDecoder(file).Decode(&record); err != nil {
		t.Fatal(err)
	}
	if record["msg"] != "job started" || record["job_id"] != float64(7) {
		t.Fatalf("record = %#v", record)
	}
}

func TestSinkRotatesAndRetains(t *testing.T) {
	dir := t.TempDir()
	sink, err := open(dir, time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC), 80, 2)
	if err != nil {
		t.Fatal(err)
	}
	fixed := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	sink.writer.clock = func() time.Time { return fixed }
	for i := 0; i < 8; i++ {
		sink.Logger().Info("event", "index", i, "payload", strings.Repeat("x", 30))
	}
	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"oplbm-20260923.log",
		"oplbm-20260923.log.1",
		"oplbm-20260923.log.2",
	} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("missing %s: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "oplbm-20260923.log.3")); !os.IsNotExist(err) {
		t.Errorf("retention exceeded: %v", err)
	}
}

func TestSinkRefusesWritesAfterClose(t *testing.T) {
	sink, err := open(t.TempDir(), time.Now(), MaxBytes, KeepFiles)
	if err != nil {
		t.Fatal(err)
	}
	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := sink.writer.Write([]byte("event\n")); err == nil {
		t.Fatal("write after close succeeded")
	}
}

func TestSinkSwitchesDate(t *testing.T) {
	dir := t.TempDir()
	fixed := time.Date(2026, 9, 23, 23, 59, 0, 0, time.UTC)
	sink, err := open(dir, fixed, MaxBytes, KeepFiles)
	if err != nil {
		t.Fatal(err)
	}
	sink.writer.clock = func() time.Time { return fixed }
	sink.Logger().Info("before midnight")
	fixed = fixed.Add(2 * time.Minute)
	sink.Logger().Info("after midnight")
	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"oplbm-20260923.log", "oplbm-20260924.log"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("missing %s: %v", name, err)
		}
	}
}

func TestLogFilesRemainReadableAcrossRecords(t *testing.T) {
	dir := t.TempDir()
	sink, err := open(dir, time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC), MaxBytes, KeepFiles)
	if err != nil {
		t.Fatal(err)
	}
	fixed := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	sink.writer.clock = func() time.Time { return fixed }
	for i := 0; i < 3; i++ {
		sink.Logger().Info("event", "index", i)
	}
	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(filepath.Join(dir, "oplbm-20260923.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	count := 0
	for scanner.Scan() {
		var record map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatal(err)
		}
		count++
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("records = %d, want 3", count)
	}
}
