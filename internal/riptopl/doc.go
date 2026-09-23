// Package riptopl stages the RiptOPL loader onto the destination (spec §2.7):
// APPS/RIPTOPL/RIPTOPL.ELF plus settings home (settings_riptopl.cfg).
//
// RiptOPL is a moving target (rolling rebuilds), so the app downloads a
// pinned build on demand (spec §10 Q5 — enrichment downloads allowed;
// ROMs/BIOS never). Implemented in M5; stub only in Phase 0.
package riptopl