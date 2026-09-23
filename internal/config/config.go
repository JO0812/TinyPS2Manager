// Package config persists user settings as JSON (no migration overhead for
// a flat key set): theme, staging directory, default split threshold, BDM
// prefix default, and destination filesystem default. Served via
// GET/PUT /api/settings in M2-4.
package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Theme options.
const (
	ThemeSystem = "system"
	ThemeLight  = "light"
	ThemeDark   = "dark"
)

// Settings is the full user-configurable set.
type Settings struct {
	Theme             string `json:"theme"`
	StagingDir        string `json:"stagingDir"`
	SplitThreshold    int64  `json:"splitThreshold"`
	BDMprefixDefault  string `json:"bdmPrefixDefault"`
	FilesystemDefault string `json:"filesystemDefault"`
}

// DefaultSplitThreshold is 4 GiB − 1 byte (the FAT32 ceiling, spec §2.2).
const DefaultSplitThreshold = int64(4294967295)

// Defaults returns factory settings (StagingDir falls back to os.TempDir
// when empty at use time).
func Defaults() Settings {
	return Settings{
		Theme:             ThemeSystem,
		StagingDir:        "",
		SplitThreshold:    DefaultSplitThreshold,
		BDMprefixDefault:  "",
		FilesystemDefault: "fat32",
	}
}

// Validate rejects unknown themes, negative thresholds, and filesystems
// outside the known set.
func (s Settings) Validate() error {
	switch s.Theme {
	case ThemeSystem, ThemeLight, ThemeDark:
	default:
		return fmt.Errorf("bad theme %q", s.Theme)
	}
	if s.SplitThreshold < 0 {
		return fmt.Errorf("negative split threshold %d", s.SplitThreshold)
	}
	switch s.FilesystemDefault {
	case "fat32", "exfat":
	default:
		return fmt.Errorf("bad filesystem default %q", s.FilesystemDefault)
	}
	return nil
}

// DefaultPath is $CONFIG/oplbm/settings.json.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "oplbm", "settings.json"), nil
}

// Load reads settings, returning Defaults when the file is absent.
func Load(path string) (Settings, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Defaults(), nil
		}
		return Settings{}, err
	}
	var s Settings
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return Settings{}, fmt.Errorf("parse %s: %w", path, err)
	}
	if err := s.Validate(); err != nil {
		return Settings{}, err
	}
	return s, nil
}

// Save validates then writes settings atomically (temp + rename).
func Save(path string, s Settings) error {
	if err := s.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".settings.*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(append(raw, '\n')); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}
