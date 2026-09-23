package cuebin

import (
	"strings"
	"unicode/utf8"
)

// VCD filename limits (spec §2.5): 89 chars per the POPSTARTER wiki,
// 73 chars for POPSLoader's stricter practical path-buffer limit.
const (
	vcdNameMaxChars    = 89
	vcdNameTargetChars = 73
	vcdExtension       = ".VCD"
)

// filenameForbidden are runes that must never appear in a VCD name.
// Spaces are legal (spec example: "Final Fantasy VII").
const filenameForbidden = `/\:*?"<>|`

// SanitizeTitle replaces forbidden filename characters with '_' and trims
// surrounding spaces and dots (which are hazardous on FAT32/exFAT).
func SanitizeTitle(title string) string {
	var b strings.Builder
	for _, r := range title {
		switch {
		case strings.ContainsRune(filenameForbidden, r) || r < 0x20:
			b.WriteByte('_')
		default:
			b.WriteRune(r)
		}
	}
	return strings.Trim(b.String(), " .")
}

// VCDFileName builds the destination filename for a converted disc
// (spec §2.4): `SXXX_NNN.NN.Title.VCD` when the serial is known, else a
// sanitized title-only `Title.VCD`. The title is trimmed so the full name
// targets ≤ 73 chars and never exceeds 89.
func VCDFileName(serial, title string) string {
	title = SanitizeTitle(title)
	if title == "" {
		title = "UNTITLED"
	}
	if !ValidSerial(serial) {
		room := vcdNameTargetChars - len(vcdExtension)
		return truncateRunes(title, room) + vcdExtension
	}
	prefix := serial + "."
	room := vcdNameTargetChars - utf8.RuneCountInString(prefix) - utf8.RuneCountInString(vcdExtension)
	if room < 1 {
		room = 1
	}
	name := prefix + truncateRunes(title, room) + vcdExtension
	if utf8.RuneCountInString(name) > vcdNameMaxChars {
		name = prefix + truncateRunes(title, vcdNameMaxChars-
			utf8.RuneCountInString(prefix)-utf8.RuneCountInString(vcdExtension)) + vcdExtension
	}
	return name
}

// truncateRunes shortens s to at most n runes.
func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	runes := []rune(s)
	return string(runes[:n])
}
