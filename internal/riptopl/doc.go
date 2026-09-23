// Package riptopl stages the RiptOPL loader onto the destination (spec
// §2.7): the chosen flavour's RIPTOPL.ELF under APPS/, downloaded as an
// explicit per-action fetch (§9.7) rather than bundled (a bundled loader
// would rot — RiptOPL rebuilds on every push).
//
// Release facts (verified against the live GitHub releases of
// NathanNeurotic/Open-PS2-Loader, Sept 2026):
//
//   - Package asset RIPTOPL-<version>.zip carries APPS/APP_RIPTOPL-*/,
//     ART/, APP_RIPTOPL.psu, POPS/, EMBER/, neutrino/.
//   - Flavours are SDK-toolchain variants of identical code, preferred:
//     PS2DEVPINNED (pinned digest, recommended), OFFICIALPINNED,
//     PS2DEVROLLING, OFFICIALROLLING.
//   - The loose RIPTOPL.ELF asset is OFFICIALROLLING: fine for updates,
//     not for first installs (so the package zip is the source here).
//   - Default tag is current-fan-favorite (stable snapshot); rolling
//     tracks master and may be unstable.
//
// Placement mirrors the package verbatim: APPS/APP_RIPTOPL-<FLAVOUR>/
// RIPTOPL.ELF (the docs reference these paths; a renamed APPS/RIPTOPL/
// would orphan the .psu shortcuts and guides).
package riptopl
