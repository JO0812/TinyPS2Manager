package cuebin

import (
	"strings"
	"testing"
)

func mustParse(t *testing.T, input string) *Sheet {
	t.Helper()
	s, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	return s
}

func TestParseSingleTrack(t *testing.T) {
	s := mustParse(t, `FILE "game.bin" BINARY
  TRACK 01 MODE2/2352
    INDEX 01 00:00:00
`)
	if len(s.Tracks) != 1 {
		t.Fatalf("got %d tracks, want 1", len(s.Tracks))
	}
	tr := s.Tracks[0]
	if tr.Number != 1 || tr.Mode != TrackMode2_2352 {
		t.Errorf("track = %+v, want number 1 mode MODE2/2352", tr)
	}
	if tr.File != "game.bin" || tr.FileType != "BINARY" {
		t.Errorf("file = %q type %q", tr.File, tr.FileType)
	}
	pos, ok := tr.IndexPos(1)
	if !ok || pos != (MSF{}) {
		t.Errorf("INDEX 01 = %+v,%v, want zero MSF,true", pos, ok)
	}
}

func TestParseMultiTrackMultiFile(t *testing.T) {
	s := mustParse(t, `
REM a comment
FILE "track1.bin" BINARY
  TRACK 01 MODE2/2352
    INDEX 01 00:00:00
FILE "track2.bin" BINARY
  TRACK 02 AUDIO
    INDEX 00 00:00:00
    INDEX 01 00:02:00
  TRACK 03 AUDIO
    PREGAP 00:02:00
    INDEX 01 00:00:00
`)
	if len(s.Tracks) != 3 {
		t.Fatalf("got %d tracks, want 3", len(s.Tracks))
	}
	if s.Tracks[0].File != "track1.bin" || s.Tracks[1].File != "track2.bin" {
		t.Errorf("file binding wrong: %+v", s.Tracks)
	}
	if _, ok := s.Tracks[1].IndexPos(0); !ok {
		t.Error("track 2 missing INDEX 00")
	}
	if !s.Tracks[2].HasPregap || s.Tracks[2].Pregap.Frames() != 150 {
		t.Errorf("track 3 pregap = %+v, want 00:02:00", s.Tracks[2].Pregap)
	}
	// 00:02:00 -> LBA 0 (150 frames minus 150 lead-in).
	if got := (MSF{M: 0, S: 2, F: 0}).LBA(); got != 0 {
		t.Errorf("LBA(00:02:00) = %d, want 0", got)
	}
}

func TestParseCRLF(t *testing.T) {
	s := mustParse(t, "FILE \"g.bin\" BINARY\r\n  TRACK 01 AUDIO\r\n    INDEX 01 00:00:00\r\n")
	if len(s.Tracks) != 1 {
		t.Fatalf("got %d tracks, want 1", len(s.Tracks))
	}
}

func TestParseSkipsMetadata(t *testing.T) {
	s := mustParse(t, `CATALOG 1234567890123
FILE "g.bin" BINARY
  TITLE "Game"
  PERFORMER "Artist"
  TRACK 01 AUDIO
    ISRC ABCDE1234567
    FLAGS DCP
    INDEX 01 00:00:00
`)
	if len(s.Tracks) != 1 || len(s.Tracks[0].Indices) != 1 {
		t.Errorf("metadata not skipped cleanly: %+v", s.Tracks)
	}
}

func TestParseErrors(t *testing.T) {
	cases := map[string]string{
		"track before file":  "TRACK 01 AUDIO\n    INDEX 01 00:00:00\n",
		"index outside":      "FILE \"g.bin\" BINARY\n    INDEX 01 00:00:00\n",
		"no tracks":          "FILE \"g.bin\" BINARY\n",
		"bad msf":            "FILE \"g.bin\" BINARY\n  TRACK 01 AUDIO\n    INDEX 01 99:99:99\n",
		"bad frames":         "FILE \"g.bin\" BINARY\n  TRACK 01 AUDIO\n    INDEX 01 00:00:75\n",
		"unknown directive":  "FILE \"g.bin\" BINARY\n  TRACK 01 AUDIO\n    FROBNICATE 1\n",
		"unknown track mode": "FILE \"g.bin\" BINARY\n  TRACK 01 LASERDISC\n",
		"unterminated quote": "FILE \"g.bin BINARY\n",
		"empty":              "",
	}
	for name, input := range cases {
		if _, err := Parse(input); err == nil {
			t.Errorf("%s: expected error, got nil", name)
		} else if !strings.Contains(err.Error(), "cue line") {
			t.Errorf("%s: error %q missing line context", name, err)
		}
	}
}

func TestTrackModeSectorSize(t *testing.T) {
	cases := map[TrackMode]int{
		TrackAudio: 2352, TrackMode1_2352: 2352, TrackMode2_2352: 2352,
		TrackMode1_2048: 2048, TrackMode2_2336: 2336,
		TrackMode("BOGUS"): -1,
	}
	for mode, want := range cases {
		if got := mode.SectorSize(); got != want {
			t.Errorf("%s: size %d, want %d", mode, got, want)
		}
	}
	if !TrackAudio.VCDNative() || TrackMode1_2048.VCDNative() {
		t.Error("VCDNative classification wrong")
	}
}
