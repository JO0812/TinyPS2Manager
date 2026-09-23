package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultsValidate(t *testing.T) {
	if err := Defaults().Validate(); err != nil {
		t.Fatalf("defaults invalid: %v", err)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "settings.json")
	s := Defaults()
	s.Theme = ThemeDark
	s.BDMprefixDefault = "OPL"
	if err := Save(path, s); err != nil {
		t.Fatalf("Save: %v", err)
	}
	back, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if back != s {
		t.Errorf("round-trip = %+v, want %+v", back, s)
	}
}

func TestLoadMissingIsDefaults(t *testing.T) {
	back, err := Load(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil {
		t.Fatal(err)
	}
	if back != Defaults() {
		t.Errorf("got %+v, want defaults", back)
	}
}

func TestSaveRejectsBad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.json")
	for _, mutate := range []func(*Settings){
		func(s *Settings) { s.Theme = "neon" },
		func(s *Settings) { s.SplitThreshold = -1 },
		func(s *Settings) { s.FilesystemDefault = "ntfs" },
	} {
		s := Defaults()
		mutate(&s)
		if err := Save(path, s); err == nil {
			t.Errorf("%+v: expected error", s)
		}
	}
	// Failed saves must leave no file behind: Load still yields defaults.
	back, err := Load(path)
	if err != nil || back != Defaults() {
		t.Errorf("partial write leaked: %+v, %v", back, err)
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.json")
	if err := os.WriteFile(path, []byte(`{"theme":"dark","bogus":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Error("expected error for unknown field")
	}
}
