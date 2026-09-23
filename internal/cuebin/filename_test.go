package cuebin

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestVCDFileNameSerial(t *testing.T) {
	got := VCDFileName("SCUS_945.67", "Final Fantasy VII")
	want := "SCUS_945.67.Final Fantasy VII.VCD"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestVCDFileNameTitleOnly(t *testing.T) {
	got := VCDFileName("", "Spyro 2 (Ripto's Rage)")
	want := "Spyro 2 (Ripto's Rage).VCD"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	// Invalid serials degrade to title-only, never corrupt the name.
	got = VCDFileName("bogus", "Game")
	if got != "Game.VCD" {
		t.Errorf("invalid serial: got %q, want Game.VCD", got)
	}
}

func TestVCDFileNameEmptyTitle(t *testing.T) {
	if got := VCDFileName("SCUS_945.67", ""); got != "SCUS_945.67.UNTITLED.VCD" {
		t.Errorf("got %q", got)
	}
	if got := VCDFileName("", "   ...   "); got != "UNTITLED.VCD" {
		t.Errorf("got %q", got)
	}
}

func TestSanitizeTitle(t *testing.T) {
	got := SanitizeTitle(`A/B\C:D*E?F"G<H>I|J`)
	want := "A_B_C_D_E_F_G_H_I_J"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	// Spaces survive (spec example keeps them); controls become '_'.
	if got := SanitizeTitle("A B\tC"); got != "A B_C" {
		t.Errorf("got %q", got)
	}
}

func TestVCDFileNameLengthCaps(t *testing.T) {
	long := strings.Repeat("A", 200)
	for _, tc := range []struct {
		name   string
		serial string
	}{
		{"with serial", "SCUS_945.67"},
		{"title only", ""},
	} {
		got := VCDFileName(tc.serial, long)
		n := utf8.RuneCountInString(got)
		if n > vcdNameTargetChars {
			t.Errorf("%s: %d runes, want <= %d", tc.name, n, vcdNameTargetChars)
		}
		if n > vcdNameMaxChars {
			t.Errorf("%s: %d runes exceeds hard cap %d", tc.name, n, vcdNameMaxChars)
		}
		if !strings.HasSuffix(got, ".VCD") {
			t.Errorf("%s: %q lost its extension", tc.name, got)
		}
		if tc.serial != "" && !strings.HasPrefix(got, tc.serial+".") {
			t.Errorf("%s: %q lost its serial prefix", tc.name, got)
		}
	}
}
