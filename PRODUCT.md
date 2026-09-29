# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Users

Primary: PS2 owners preserving discs they already own. Situation: at a PC (Windows, macOS, or Linux) preparing a USB / MX4SIO drive for a modded PS2 (FMCB / Matrix units, RiptOPL / OPL plus POPStarter / Ember for PS1). Job: ingest `.iso` + `.cue/.bin` sources, get a correct OPL device tree, and copy it to the drive so games boot on real hardware.

## Product Purpose

Prepare a PS2/PS1 backup library so it is 100% compatible with OPL's BDM device layout and POPStarter's VCD / multi-disc conventions. The product exists because FAT32 BDM drives filled in parallel fragment past OPL's 64-fragments-per-file cap and fail to boot. Success means: the staged drive passes pre-flight, copies land sequentially and verified, and the library boots on-console with correct listings, covers, and cheats where staged.

## Positioning

A correctness-first core (classify, convert, split, lay out, sequential verified copy) wrapped with on-demand enrichment and drive pre-flight: pinned RiptOPL staging, per-title cover art and PS2RD cheats, a no-conversion Ember path for PS1, plus MBR / filesystem / alignment validation and a first-boot checklist. Neighboring approaches stage files without combining validation-first transfers, resume-safe sequential copies, and explicit per-action enrichment in one tool.

## Operating Context

Workflows: Library (scan, disc-type badges, inline CD/DVD correction, multi-disc grouping) → Destination (removable-drive picker, FAT32/exFAT toggle, BDM prefix, pre-flight pass/fail/warn) → Queue (user-ordered, Converting → Splitting → Copying → Verifying → Done, pause/resume/retry) → Activity/Settings. Also scriptable CLI (`import/inspect/convert/split/tree/serve/gameid/art/cheats/riptopl/preflight`) and `serve` on `127.0.0.1:41337`.

Environments: PC-side staging; destination is a mounted USB/SD volume or a local folder for later imaging. PS2 ports are USB 1.1 with weak power — plain flash drives preferred; multi-GB staging is a PC-side job (~1 MB/s on-console). First PS2 boot is always manual: enable USB/MX4SIO under Game Sources → start mode → Save Changes → L3 for PS1. Console mod install (FMCB/Matrix/Fortuna) and drive formatting/partitioning stay manual with OS tools.

## Capabilities and Constraints

Confirmed functionality: 4-step PS2 CD/DVD detector (override → ISO9660/UDF inspection → 700 MiB heuristic → bundled DB) with GameID from streaming `SYSTEM.CNF` `BOOT2`; PS1 CUE/BIN → streaming 2352-byte VCD; multi-disc PS1 `DISCS.TXT` (≤4×73) + `VMCDIR.TXT` (1×103) generation and validation; USBExtreme 1 GiB `ul.cfg` + `ul.*` splits at device root on FAT32, plain `.iso` on exFAT; SQLite sequential queue (one writer per volume, prep-ahead pool, same-volume temp + rename, SHA-256 verify, pause/resume, attempts cap 3); explicit per-action enrichment (RiptOPL flavour-pinned download, GameTDB/libretro art normalized to 512×730 / 512×512, CheatDevice + widescreen `.cht` with mandatory `9`-type master, ≤250 cheats); `copy-ps1-ember` plain CUE+BIN path gated on user-supplied 512 KiB `EMBER/bios.bin`; destination pre-flight (MBR `dos`, partition type vs fs toggle, free space, `ul.cfg` parse) with warnings for power/adapter/fragmentation risks.

Hard constraints: N1 streaming I/O (buffers ≤4 MiB); N2 no cgo / `os/exec` outside `desktop` tag; N3 sources read-only, never deleted or mutated; N4 `DISCS.TXT` / `VMCDIR.TXT` / `ul.cfg` validated before enqueue (422), bad art never blocks games; N5 exactly one destination writer; N6 SQLite persistence for resume. The app never formats/partitions, never downloads or fabricates ROMs/BIOS, and works fully offline for import/convert/split/queue — enrichment is explicit per-action network with URL shown pre-fetch and digest recorded after. Undecided: none material at this level.

## Brand Commitments

Name `TinyPS2Manager`, presented as "OPL Backup Manager — the safe, correct way to prepare a PS2/PS1 library for OPL / RiptOPL". Voice is factual and cautionary (correctness, verification, guidance over automation for destructive steps). Assets on hand: app icon at `flatpak/io.github.JO0812.TinyPS2Manager.svg`. Sources of truth: `OPL-Backup-Manager-SPEC.md` (§§2.1–2.11) and `BUILD-PLAN.md` (M1→M5, Q1–Q7, N1–N6). No binding visual constraint was volunteered in init.

## Evidence on Hand

Real content: README quickstart and feature table; SPEC v0.2 field rules (RiptOPL rolling `v1.2.0-Beta` layout, art keying, cheat engine limits, Ember layout, drive-prep facts); BUILD-PLAN milestones and Q1–Q7 resolutions; Go + Svelte/Vite codebase with `go:embed web/dist`. Test data: synthetic ISO9660 + CUE/BIN fixtures only via `testdata/gensrc` (deterministic, <100 MiB) — no real game images in repo, ROMs/BIOS never downloaded. Absences future work must not fabricate: user testimonials, case studies, benchmarks, pricing/licensing claims, stock cover art.

## Product Principles

1. Correctness over speed: one sequential verified copy beats fast parallel writes that fragment.
2. Validate before bytes: pre-flight and manifest checks block enqueue, never clean up after failure.
3. Sources are sacred: read-only inputs, new files only, resumable state across kills.
4. Explicit network only: offline core always works; every fetch is user-initiated, shown, and recorded.
5. Guide, don't destroy: formatting, partitioning, and first boot stay with OS tools and on-console steps the app checklists but never performs.
