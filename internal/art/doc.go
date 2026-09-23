// Package art fetches and stages per-title cover art into ART/ (spec §2.8).
//
// Keying (exact, from RiptOPL docs): PS2 game → disc ID
// (ART/SLUS_213.85_COV.png); PS1 POPSTARTER VCD → VCD filename; PS1 Ember →
// game folder name. Missing/custom states are tracked per title.
// Implemented in M5; stub only in Phase 0.
package art