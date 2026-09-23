# OPL/POPSTARTER Backup Manager — Specification

**Version:** 0.2 (draft; supersedes v0.1 — see §12 changelog)
**Author:** (fill in)
**Status:** Draft for review
**Target loaders:** RiptOPL / Open PS2 Loader (PS2 discs), POPStarter / Ember (PS1), FreeMCBoot (boot)

---

## 1. Purpose

A cross-platform desktop application that prepares a PS2/PS1 backup library on a USB/MX4SIO drive
(or a local staging folder) so it is 100% compatible with OPL's BDM device layout and POPSTARTER's
VCD/multi-disc conventions. The app ingests raw `.iso` (PS2) and `.cue`/`.bin` (PS1) sources, converts
and organizes them into the correct OPL folder tree, splits or leaves files whole based on target
filesystem, and copies everything to the destination device **one file at a time, in a fixed
sequential order**, to avoid fragmentation on FAT32/exFAT BDM devices.

It is _not_ a game manager/launcher, a ROM downloader, or an on-console mod installer (FMCB/Matrix/
Fortuna flows stay manual — but see §2.11 for the drive-prep facts the app must know, and §2.7–§2.10
for the loader/art/cheat files the app stages onto the drive).

## 2. Background — target platform rules

These rules come directly from the RiptOPL and POPStarter/POPSLoader documentation and drive most of
the app's logic. They must be treated as source of truth by the implementation.

### 2.1 OPL folder layout (BDM devices: USB / MX4SIO / iLink / exFAT-HDD)

```
<root or BDM prefix>/
  DVD/     PS2 DVD5/DVD9 ISO or ZSO images (plain .iso only; split sets live at ROOT, see §2.2)
  CD/      PS2 CD-ROM ISO images ("blue-bottom" discs, ≤ ~700MB)
  POPS/    PS1 *.VCD images (+ POPSTARTER.ELF); OPL never creates this folder itself
  EMBER/   Ember PS1 core: ember.elf + user-supplied bios.bin (512 KB) + games/<Name>/*.cue|*.bin
  CFG/     Per-game config
  ART/     Cover art (see §2.8 for keying rules)
  VMC/     Virtual memory cards (PS2)
  CHT/     PS2RD cheat files, one <GameID>.cht per game (see §2.9)
  APPS/    Homebrew ELFs, incl. RiptOPL itself at APPS/RIPTOPL/RIPTOPL.ELF (see §2.7)
  THM/ LNG/ theme/lang folders
```

- Case-sensitive folder names: `DVD`, `CD`, `POPS`, `EMBER`, `ART`, `CHT`, `APPS`.
- An optional **BDM Prefix** subfolder may exist (e.g. `OPL/DVD/…`); the app must let the user set a
  prefix (default empty = drive root).

### 2.2 Filesystem rules that drive the splitting logic

| Filesystem | Max single file size | Notes                                                                             |
| ---------- | -------------------- | --------------------------------------------------------------------------------- |
| FAT32      | 4 GB − 1 byte        | Games over this must be split into **USBExtreme (.ul)** format                    |
| exFAT      | Unlimited            | Plain `.iso`/`.zso` files work at any size; default allocation unit size required |

- **CD folder**: PS2 CD-ROM titles, always ≤ ~700 MB. Never need splitting regardless of filesystem.
- **DVD folder**: PS2 DVD5 (~4.7GB) / DVD9 (~7-8GB) titles, always as **plain `.iso`** files.
- **USBExtreme split sets live at the device ROOT, not in `DVD/`** (field correction to v0.1, which
  placed them in `DVD/`): `ul.cfg` (index) + `ul.<CRC8>.<GameID>.00`, `.01`, … chunk files, all as
  siblings at `<root>/`. OPL auto-detects these sets; no other companion config is required. On FAT32,
  anything over 4GB − 1 byte must be split this way; on exFAT, DVD images stay single plain `.iso`.
- OPL's BDM driver caps at **64 fragments per file** (see §2.6); split chunks copied sequentially stay
  contiguous, which is part of why the queue exists.

### 2.3 Determining CD vs DVD placement

The app must decide the destination folder (`CD/` vs `DVD/`) for each PS2 `.iso` based on the game's
**original disc type**, not merely file size (some DVD games are small, and CD games are capped at
700MB by the format itself). Strategy (in priority order):

1. **User-provided/override metadata** — a per-title field in the app ("Disc type: CD / DVD") the user
   can set explicitly, persisted per source file (hash-keyed) so re-imports don't ask again.
2. **ISO9660/UDF volume inspection** — read the primary volume descriptor / sector count to compute
   the actual data size, and inspect for DVD-specific structures (UDF bridge format is a strong DVD
   signal; pure ISO9660 CD-XA sector layout at ≤700MB is a strong CD signal).
3. **Heuristic fallback** — if inspection is inconclusive: file size ≤ 700 MB → CD; > 700 MB → DVD.
4. If the heuristic and any known game database entry (optional local JSON list of known PS2 CD
   titles by serial, bundled with the app and user-extensible) disagree, prefer the database.

The UI must always show the detected/assumed disc type and let the user correct it before queuing.

5. **GameID (disc serial) extraction** — the `SXXX_NNN.NN` serial (e.g. `SLUS_213.85`) is the join key
   for cover art (§2.8) and cheats (§2.9) and must be extracted per title: read `SYSTEM.CNF` from the
   ISO (first file in the ISO9660 path table — stream only directory records + that file, never the
   whole image) and parse the `BOOT2 = cdrom0:\SXXX_NNN.NN;1` line. Record `gameId` on the
   `LibraryItem`; titles with no readable serial (or mods whose serial identifies a different base
   game, e.g. fan translations/league mods) are flagged `gameIdUncertain` so art/cheat matching asks
   for confirmation instead of silently attaching the wrong files.

### 2.4 PS1 → VCD conversion

- Source: matched pairs of `.cue` + one or more `.bin` tracks (standard CUE/BIN rip).
- Output: a single `.vcd` image per disc, placed in `POPS/`.
- `.vcd` for POPS/POPSTARTER is effectively a raw 2352-byte/sector CD image (mode2/raw) equivalent to
  a single merged BIN with the CUE sheet's track layout folded in — the app must merge multi-track
  CUE/BIN sets into one contiguous image using the CUE sheet's index/pregap data, matching the layout
  POPSTARTER's POPS core expects. (Implementation detail: reuse or reimplement the well-known
  bin/cue → VCD merge algorithm used by PSX2PSP-family tools; do not depend on GPL-incompatible
  external binaries — implement natively in Go, see §6.2.)
- Practical size ceiling: ~2 GB per VCD is called out by POPSTARTER docs for multi-disc combined
  images; the app should warn (not block) if a resulting VCD exceeds 2 GB.
- Filename convention: prefer the PS1 disc-ID pattern `SXXX_NNN.NN.Title.VCD` (e.g.
  `SCUS_945.67.Final Fantasy VII.VCD`) when the serial is known/extractable from the CUE/system area,
  because RiptOPL keys cover art and per-game config off that ID. Fall back to a sanitized title-only
  filename otherwise.

### 2.5 Multi-disc games

**PS2 multi-disc**: each disc is an independent `.iso`, just placed side by side in `DVD/` (or split
per §2.2 if needed). No special manifest is required by OPL for PS2 discs; the app just needs clear,
consistent naming (`Title (Disc 1).iso`, `Title (Disc 2).iso`, …) and must group multi-disc PS2 titles
in the UI so the user manages/queues them together.

**PS1 multi-disc** (POPSTARTER `DISCS.TXT` convention — must be implemented exactly):

- Up to **4 discs** supported by the in-game swap feature (Select+L2+R2 hotkeys). More than 4 discs is
  handled by manual reinstall waves, out of scope for automatic tooling beyond a warning.
- For each disc N, the app must produce, alongside `POPS/DiscN.VCD`, a per-disc **VMC folder**
  `POPS/DiscN/` containing:
  - `DISCS.TXT` — one `.VCD` filename per line (with extension), same content in **every** disc's
    folder, listing all discs of the game in order. Max 4 lines. Each filename ≤ 89 chars per the
    wiki spec, but the app should target ≤ 73 chars to respect POPSLoader's stricter practical
    path-buffer limit.
  - `VMCDIR.TXT` — single line, ≤ 103 bytes, no `/ \ :` characters, naming disc 1's VMC folder, placed
    identically into every disc's folder so all discs share one save (SLOT0/SLOT1 VMC pair). Target
    folder name must be a bare folder name (no path separators) and must resolve inside `POPS/`.
- The app must validate these constraints at generation time (line count, char limits, forbidden
  characters) and surface violations as errors before queuing the transfer.

### 2.6 Fragmentation sensitivity (why the sequential queue exists)

- OPL's BDM driver supports at most **64 fragments per file**; heavily fragmented FAT32 drives filled
  incrementally can exceed this and games will fail to load.
- The documented fix/best practice is to copy files back **one at a time, in a single sequential
  batch**, never in parallel, so each file lands contiguously.
- **Requirement:** the app's transfer engine must never write two large game files concurrently to the
  same destination volume. The queue is strictly sequential per destination device; parallelism is
  only permitted for independent conversion/preprocessing steps that don't touch the destination disk
  (e.g., converting the next CUE/BIN to VCD in a temp/staging area while the previous file is still
  being copied), never for the final write to the target drive.

### 2.7 RiptOPL provisioning

The app stages the loader itself onto the device so a freshly prepared drive boots straight into
games (all facts below verified against the RiptOPL docs + release layout, rolling `v1.2.0-Beta`):

- **Which binary:** the unified `RIPTOPL-<version>.zip` package carries labelled flavours; use
  `APP_RIPTOPL-PS2DEVPINNED/RIPTOPL.ELF` when present (docs-recommended primary, digest-pinned
  ps2dev SDK), else the next labelled flavour in documented order. The loose `RIPTOPL.ELF` asset is
  the `-OFFICIALROLLING` flavour — acceptable for updates, not for first installs. Single ~1.4 MB
  ELF, no per-feature variants to choose between (GSM/PADEMU/VMC/PS2RD/parental controls included).
- **Where:** FMCB convention `APPS/RIPTOPL/RIPTOPL.ELF` (device root or under the BDM prefix). The ELF
  filename itself is irrelevant to RiptOPL — only the launcher's menu entry must point at it.
- **Settings home:** `settings_riptopl.cfg` (migrates from `conf_riptopl.cfg`), private filename that
  never collides with stock OPL; artwork/themes/VMCs/`conf_network.cfg`/per-game CFG stay shared.
  Config is discovered at the folder the ELF launched from — so launching from `mass:` keeps settings
  on the USB stick, not on the memory card. The app must not assume `mc0:/OPL/`.
- **First boot is manual:** every device ships **Off**; the user must enable USB (or MX4SIO/iLink/SMB/
  HDD) under Settings → Game Sources, pick a start mode (Manual/Auto), then Save Changes — only then
  are `settings_riptopl.cfg` written and `DVD/ CD/ ART/ CFG/ VMC/` auto-created (never `POPS/`).
  The app pre-creates the folder tree (§2.1) so this step only enables + saves; it must surface this
  as a first-boot checklist item in the UI, not assume the drive is ready.
- **Do not bundle stale loaders:** RiptOPL is a moving target (rolling rebuilds). The app downloads a
  pinned flavour on demand (explicit user action, version recorded in the DB) rather than embedding a
  binary that rots. Offline use keeps whatever ELF is already staged.

### 2.8 Cover art (`ART/`)

- **Keying (exact, from RiptOPL docs):**
  - PS2 game → disc ID: `ART/SLUS_213.85_COV.png`.
  - PS1 POPSTARTER VCD → VCD filename (`ART/Spyro 2 (Ripto's Rage)_COV.png`), or the PS1 disc ID if
    the VCD follows `SXXX_NNN.NN.Title.VCD`.
  - PS1 Ember → game **folder** name (`ART/<folder>_COV.png`).
  - Suffix selects the image role (`_COV` cover, `_SCR`/`_SCR2` screenshots, `_BG`, `_ICO`); themes
    decide what to draw. Filenames are case-sensitive on the wire; keep the canonical upper-case form.
- **Format:** PNG; existing in-the-wild sets use 512×730 (PS2 portrait) and ~512×512 (PS1 square).
  The app normalizes downloads to these sizes (fit, not stretch) and validates PNG magic + dimensions
  before enqueue (N4 applies: bad art never blocks games — it is skipped with a warning, queued as a
  separate low-priority job class, never interleaved with game writes).
- **Sources (patterns, not scraped HTML):**
  - PS1: `libretro-thumbnails` per-system submodule
    `raw.githubusercontent.com/libretro-thumbnails/Sony_-_PlayStation/master/Named_Boxarts/<No-Intro name>.png`
    (note: the parent repo is only a `.gitmodules` index — the `Named_Boxarts/` path resolves on the
    per-system repo, not the parent).
  - PS2: GameTDB `art.gametdb.com/ps2/cover/<REGION>/<ID-without-separators>.png`
    (e.g. `.../ps2/cover/EN/SLUS21385.png`); region from the title's region tag (USA→US/EN,
    Europe→EN, Japan→JA).
  - Custom covers (fan mods/translations keyed to a base-game serial): generated locally —
    512×730 canvas, title text + palette motif — and recorded as `customArt: true` so updates never
    overwrite them with a stock download.
- **Missing art is never an error:** games without a cover still enqueue and boot; the UI shows a
  per-title art-status badge (found / custom / missing).

### 2.9 Cheats (`CHT/`)

- **File rule (exact, from RiptOPL docs):** one game per file, filename = startup disc ID +
  `.cht`: `CHT/SLUS_213.85.cht` (`.CHT` accepted). PS2RD text format: a name line followed by
  16-hex-digit `address value` lines; anything else shaped is a name/comment (`//` or `#`; `;` is
  NOT a comment marker; blank lines ignored; LF or CRLF).
- **Master Code is mandatory:** a `9`-type hook line (e.g. `90123ED8 …`) must head the file or no
  other code fires (silently). The app validates presence of exactly one usable master line before
  enqueue; files without one are rejected with "no master code", never written.
- **Engine limits enforced at build time:** ≤ 250 named cheats per file (extras dropped with a
  warning naming the count); types `8`/`A`/`B` parsed-but-skipped by the engine — the app warns when
  a file relies on them. Loader Core must stay on OPL (Neutrino ignores cheats entirely).
- **Sources and their trust levels:**
  1. `CheatDevicePS2/CheatDatabase.txt` (root670) — full gameplay cheats in near-PS2RD text form,
     but keyed by **title string, not GameID, and USA-build-centric**. The app maps title→GameID via
     the library DB and marks files `regionMatched: false` when the title's build region differs
     from the disc's (wrong-region codes usually don't fire; occasionally they crash — surface this,
     default per-game cheat mode to `Select`, never `Auto`, for region-mismatched files).
  2. `PS2-Widescreen/OPL-Widescreen-Cheats` `CHT/<GameID>.cht` — exact per-ID widescreen patches with
     embedded master codes; safe to apply blindly, ideal default content when no gameplay file exists.
  3. Hand-authored files dropped into a watched folder (highest trust, never overwritten).
- **Never merge two masters:** files are staged whole per game (gameplay OR widescreen OR hand file,
  user picks the winner per title in the UI). OPL honors only the first `9`-type line, so
  concatenating packs silently kills one set.
- **Enabling stays on-console:** Game Settings → Cheat Settings → PS2RD On → Auto/Select. The app
  only stages files; it documents the toggle in the first-boot checklist (§2.7).

### 2.10 PS1 via Ember (no-conversion path)

POPSTARTER VCD conversion (§2.4) stays the default for PS1, but field experience shows a second,
simpler path the app must support as a first-class job kind (`copy-ps1-ember`, no conversion step):

- **Layout:** `EMBER/ember.elf` + `EMBER/games/<Game Folder>/*.cue|*.bin` (+ any track bins, names
  preserved byte-for-byte since the CUE references them). Cover key = folder name (§2.8).
- **`bios.bin` (exactly 512 KB) is user-supplied, never distributed, never fabricated.** The app
  detects presence/absence/size at destination-validation time and blocks Ember jobs with "PS1 BIOS
  missing — place your own `EMBER/bios.bin`" until resolved. No download, no placeholder.
- ** Trade-offs surfaced in UI:** Ember needs no conversion and handles multi-track rips as-is, but
  is beta-quality on real hardware; POPSTARTER VCD is the mature core. Per-title core is chosen at
  PS2 boot, so both layouts may coexist for the same title.
- PS1 view discovery on-console: RiptOPL PS2/PS1 Game Display defaults to separate L3-switched
  views — the first-boot checklist (§2.7) must mention pressing L3 to reach PS1 titles.

### 2.11 Drive preparation and validation (field rules)

The app never formats drives itself (destructive ops stay with the OS tools), but it validates and
guides, because an incorrectly prepared drive fails in ways that look like app bugs:

- **Required layout for BDM mass devices:** MBR (`dos`) partition table — **GPT is unsupported**;
  single primary partition, type `0x0c` (W95 FAT32 LBA), filesystem FAT32 (`mkfs.vfat -F 32`,
  label ≤ 11 chars) or exFAT with **default** allocation unit size. Partition must start aligned
  (sector 2048, 1 MiB), not at legacy offsets (field case: a partition starting at sector 65535
  produced a superblock neither kernel nor PS2 would mount).
- **Pre-flight checks** (run at destination selection, block enqueue on failure): partition table
  is `dos`; partition type is `0x0c`/`0x07`/`0x83`-as-expected vs chosen fs; filesystem matches the
  user's FAT32/exFAT toggle (detect via `fsinfo`, §6.3); ≥1 MB contiguous free probe; `ul.cfg`+`ul.*`
  sets (if any pre-exist) parse.
- **Hardware realities to surface as warnings, not errors:** PS2 ports are USB 1.1 with weak
  power — spinning-HDD enclosures brown out mid-transfer on-console and SD/MMC adapters freeze
  `mass:/` on some units (ps2-home t=6876 class); recommend plain flash pendrives. USBExtreme splits
  must be copied in index order in one sequential batch (fragmentation, §2.6). PS2-side USB copy
  speed (~1 MB/s class) makes multi-GB staging a PC-side job — which is this app's raison d'être.
- **Mounting without privileges:** on Linux the app must prefer `udisksctl` (no sudo) and treat
  `sudo`-requiring flows as a hard error with guidance; `sudo` without a tty fails and its password
  prompt must never be scraped or stored.

## 3. Goals

1. Ingest a folder/batch of PS2 `.iso` and PS1 `.cue`/`.bin` sources.
2. Classify each PS2 title as CD or DVD (§2.3), each PS1 title as single- or multi-disc.
3. Convert PS1 CUE/BIN sets to `.VCD` (§2.4) and generate `DISCS.TXT`/`VMCDIR.TXT` for multi-disc sets
   (§2.5).
4. Split PS2 DVD isos exceeding 4GB into USBExtreme `.ul` format when the destination is FAT32; leave
   as plain `.iso` on exFAT.
5. Build/validate the correct OPL directory tree (`DVD/`, `CD/`, `POPS/`, with per-disc VMC subfolders)
   at the chosen destination (physical drive, mounted volume, or a local folder the user will image
   later).
6. Queue and execute all operations (convert + copy) **sequentially** against a chosen destination to
   protect against fragmentation, with resumability and clear progress reporting.
7. Run identically on Windows, macOS, and Linux, as a single lightweight binary + web-based UI.
8. Provide light/dark theme.
9. Stage the loader and its enrichment files onto the destination: pinned RiptOPL build (§2.7),
   per-title cover art with missing/custom states (§2.8), per-title PS2RD cheats with master-code
   validation (§2.9), and Ember-layout PS1 titles without conversion (§2.10).
10. Validate the destination drive's preparation (MBR/FAT32-or-exFAT/alignment, §2.11) before any
    byte is written, with actionable guidance when it fails.

## 4. Non-goals (v1)

- Downloading/scraping ROMs or BIOS files, ever. Sources are local files the user already owns.
- Acting as an on-console game manager (this app never talks to the PS2 itself).
- On-console mod installation (FMCB/Matrix/Fortuna/FreeDVDBoot flows stay manual; the app only
  stages drive files those flows consume).
- PS3/PS4/other platform support.
- Formatting/partitioning drives (validate + guide per §2.11; destruction stays with OS tools).
- DVD9 dual-layer burning to physical media (the "DVD folder" here means the OPL `DVD/` game folder on
  a USB/SD device, not optical burning — clarify this with the user if physical disc burning is
  actually desired; **assumption for this spec: "CD/DVD" = OPL's CD/DVD _folder_ buckets, not physical
  optical media**).

## 5. High-level architecture

```
┌─────────────────────────────┐        ┌──────────────────────────────┐
│         Frontend            │  HTTP  │            Backend            │
│  (Svelte/Preact SPA, static │◄──────►│  Go binary, embeds frontend   │
│  build, served by backend)  │  + WS  │  via embed.FS, single exe     │
└─────────────────────────────┘        └──────────────────────────────┘
                                                    │
                          ┌─────────────────────────┼─────────────────────────┐
                          ▼                         ▼                         ▼
                 Library/Import module      Conversion engine          Transfer engine
                 (scan sources, detect      (CUE/BIN→VCD, ISO          (sequential queue,
                 disc type, dedupe)         inspection, USBExtreme     checksum verify,
                                             split)                    resumable copy)
                          │                         │                         │
                          └─────────────────────────┴─────────────────────────┘
                                                    │
                                             SQLite (job/library DB)
```

- Single Go binary; the frontend is a compiled static SPA embedded with `go:embed` and served on
  `localhost` (with an optional pure-Electron/Wails-style desktop shell wrapping it for a native app
  icon/tray — see §9.5). No separate frontend runtime/process required at run time.
- Communication: REST for CRUD-style operations (library, jobs, settings), a WebSocket (or SSE) for
  live queue/progress events.
- All heavy lifting (I/O, hashing, conversion) happens in Go; the frontend is a thin, mostly reactive
  view over backend state.

## 6. Backend (Go) — detailed design

### 6.1 Why Go here

- Low, predictable memory footprint for large sequential file I/O (streaming copies/splits without
  loading whole ISOs into memory).
- `io.Copy`/buffered readers+writers with explicit buffer sizes give tight control over disk
  throughput and let us guarantee **one active writer to the destination at a time**.
- Static single-binary cross-compilation (`GOOS`/`GOARCH`) trivially covers Windows/macOS/Linux without
  per-platform runtimes.
- Goroutines + channels map cleanly onto the "parallel prep, sequential write" pipeline in §2.6.

### 6.2 Core packages/modules (suggested Go module layout)

```
/cmd/oplbm/                 main entrypoint, flag parsing, embeds frontend build
/internal/library/          source scanning, hashing (content-hash for dedupe/override cache),
                             disc-type detection (§2.3), metadata store
/internal/isotool/          ISO9660/UDF volume descriptor parsing (read-only), size/sector math
/internal/cuebin/           CUE sheet parser, BIN track merge → VCD writer (streamed, no full-file
                             buffering), disc-ID extraction from the PS-X system area
/internal/usbextreme/       USBExtreme (.ul) split writer + index file generation, per published
                             format (chunk size, naming, header)
/internal/oplfs/            OPL folder-tree builder/validator (DVD/CD/POPS/…, BDM prefix support),
                             DISCS.TXT / VMCDIR.TXT generator + validator (§2.5)
/internal/queue/            job model, persistence (SQLite via modernc.org/sqlite — pure Go, no
                             cgo — keeps the "lean binary, easy cross-compile" property), sequential
                             executor, resumability/checkpointing
/internal/transfer/         destination abstraction (local path / removable-drive path), free-space
                             checks, filesystem-type detection (FAT32 vs exFAT) per platform, atomic
                             write-then-rename, checksum verification post-copy
/internal/api/               HTTP handlers + WebSocket hub
/internal/config/            user settings (theme, default destination, BDM prefix, split threshold)
/web/                        frontend source (built separately, embedded at build time)
```

- No cgo, no external native dependencies (no shelling out to `dd`, `mkisofs`, etc.) — everything the
  spec requires (CUE/BIN merge, ISO inspection, USBExtreme split) is straightforward binary/streaming
  logic implementable in pure Go, which keeps cross-compilation and the "lean on memory" goal intact.
- SQLite (pure-Go driver) for the job queue and library DB so state survives app restarts and jobs are
  resumable; avoid requiring a running daemon/service.

### 6.3 Filesystem type & free-space detection

- Linux: parse `/proc/mounts` / `statfs` for fs type; macOS: `getmntinfo`/`diskutil info -plist`;
  Windows: `GetVolumeInformation` via `golang.org/x/sys/windows`. Wrap behind a small
  `internal/transfer/fsinfo` interface with per-OS build-tagged implementations.
- If detection fails or the destination is a "folder for later imaging" rather than a live removable
  drive, fall back to an explicit user choice: "Target filesystem: FAT32 / exFAT" toggle, defaulting to
  FAT32 (the safer, more restrictive assumption) so splitting is applied unless the user confirms
  exFAT.

### 6.4 Sequential transfer engine — requirements

1. Exactly one destination-writing operation in flight per destination device at any time.
2. Jobs are processed in **user-defined queue order** (drag-to-reorder in UI), FIFO by default.
3. Each job is one of: `convert-and-copy` (PS1 CUE/BIN → VCD → destination), `split-and-copy` (PS2 ISO
   → USBExtreme set → destination), or `copy` (PS2 ISO/VCD already correctly sized → destination
   as-is).
4. Conversion/splitting to a **local temp/staging directory** may run concurrently with an in-flight
   destination write for a _different_ job (prep pipelining), but the final move/copy onto the
   destination volume is always serialized.
5. Prefer writing the final file directly to destination in one contiguous streamed pass (temp file on
   the _same volume_ + atomic rename where the OS/filesystem allows it) rather than "write to PC temp
   dir then copy" when avoidable, to minimize total sequential writes; when a convert step is required
   (CUE/BIN, splitting) the conversion output should be produced directly on the destination when
   feasible, or via a same-volume temp file + rename, to preserve contiguity.
6. Verify each written file post-copy (checksum or size+sample-hash) before marking the job complete
   and advancing the queue.
7. On error, pause the queue (don't skip silently), surface the error in the UI, allow retry/skip/abort
   per job.
8. Support pause/resume of the whole queue and safe cancellation mid-file (delete partial output).
9. Persist queue state so an app restart can resume an interrupted run.

### 6.5 API surface (indicative)

```
GET    /api/library                 list imported source titles (with detected type/status)
POST   /api/library/import          scan a folder path for .iso/.cue+.bin sources
PATCH  /api/library/{id}            override disc type / title / multi-disc grouping
GET    /api/destinations            list detected removable volumes + free space + fs type
POST   /api/queue                   enqueue one or more library items to a destination
GET    /api/queue                   current queue + per-job status
PATCH  /api/queue/{jobId}           reorder / pause / cancel / retry a job
WS     /api/queue/events            live progress (bytes done, ETA, current phase)
GET    /api/settings  PUT /api/settings   theme, BDM prefix, default split threshold, etc.
```

## 7. Frontend requirements

- **Stack:** a lightweight SPA — Svelte (or Preact) + Vite recommended over React for smaller bundle
  size and less runtime overhead, consistent with the "lightweight" requirement; plain CSS
  variables/custom properties for theming rather than a heavy CSS framework.
- **Views:**
  1. **Import/Library** — pick source folders, see detected titles with disc-type badges
     (CD/DVD/PS1, single/multi-disc), inline correction controls.
  2. **Destination** — pick/select a drive or folder, shows detected filesystem, free space, and BDM
     prefix setting.
  3. **Queue** — reorderable list of pending jobs, live progress bars, per-job phase (Converting →
     Splitting → Copying → Verifying → Done), pause/cancel/retry controls.
  4. **Settings** — theme toggle (light/dark, following OS preference by default), split threshold
     override, staging/temp directory location.
- **Theming:** dark/light mode via a CSS variables palette + a `prefers-color-scheme` default and a
  manual override persisted in settings; no heavy component library required.
- **Progress/feedback:** WebSocket-driven live updates; must clearly communicate _why_ the queue is
  sequential (a short inline note: "Transfers run one at a time to prevent disc fragmentation on your
  OPL device") so the sequential behavior isn't mistaken for a bug.
- **Accessibility/perf:** must run smoothly on modest hardware; avoid unnecessary re-renders during
  high-frequency progress events (throttle UI updates to ~4–10/sec).

## 8. Data model (indicative)

```
LibraryItem {
  id, sourcePath, contentHash, platform: "ps2" | "ps1",
  discType: "cd" | "dvd" | null,          // ps2 only
  detectionMethod: "override"|"inspected"|"heuristic"|"database",
  title, discIndex, discGroupId,           // discGroupId links multi-disc titles
  sizeBytes, status: "new"|"queued"|"done"|"error"
}

Job {
  id, libraryItemId, destinationId, kind: "copy"|"split-and-copy"|"convert-and-copy",
  order, status: "pending"|"running"|"paused"|"error"|"done",
  phase, bytesTotal, bytesDone, error, createdAt, updatedAt
}

Destination {
  id, path, kind: "drive"|"folder", filesystem: "fat32"|"exfat"|"unknown"|"user-override",
  bdmPrefix, freeBytes
}
```

## 9. Non-functional requirements

1. **Memory footprint:** streaming I/O throughout; no operation should require loading a full ISO/BIN
   into memory (target: steady-state RSS independent of game file size, bounded buffer sizes e.g.
   1–4 MB).
2. **Cross-platform:** single Go binary per OS/arch (win/amd64, darwin/amd64, darwin/arm64,
   linux/amd64, linux/arm64); frontend embedded, no separate install step for a browser runtime.
3. **Resilience:** safe to kill the process mid-transfer; on relaunch, the queue and library state are
   intact and partial files are recognized and cleaned up or resumed.
4. **Data safety:** never delete or modify source files; all conversions/splits write new files only.
5. **Validation-first:** DISCS.TXT/VMCDIR.TXT and USBExtreme output must be validated against the
   documented constraints (§2.2, §2.5) _before_ a job is allowed to enqueue, not just at write time.
6. **Logging:** structured log file per run (rotated), plus in-app job history, for troubleshooting
   failed transfers.
7. **Network posture:** the app works fully offline for everything v0.1 covered (import, convert,
   split, queue). The v0.2 enrichment features (RiptOPL download, cover-art fetch, cheat-database
   fetch) are explicit, per-action, user-initiated network calls with the URL shown before fetch,
   pinned-version recording afterwards, and full functionality retained offline from cache/bundled
   data. No telemetry, no background calls, no "check for updates" beyond an explicit button.

## 10. Open questions for the user (please confirm before implementation starts)

1. Is "CD/DVD" here strictly OPL's `CD/`/`DVD/` _folder buckets on a storage device_, or do you also
   want actual optical disc burning support (a very different, additional feature)? This spec assumes
   folder buckets only. R: I don't need optical disc burning support
2. Should the app support writing directly to a mounted USB/MX4SIO drive, or only to a local staging
   folder that the user then copies/images themselves (simpler, safer v1 scope)? R. The app must support writing to a mounted USB/SD card drive
3. Do you want an optional bundled/known-games database (CD vs DVD, canonical titles, disc IDs) shipped
   with the app, or should disc-type detection rely solely on ISO inspection + manual override? R. I want a bundled/known games database
4. Desired packaging: plain CLI+localhost web UI, or a wrapped desktop app (e.g. via Wails, which pairs
   naturally with a Go backend and a Svelte/Preact frontend) with a native window/tray icon? Wrapped desktop app
5. (v0.2) Should the app download RiptOPL builds, cover art, and cheat packs on demand (explicit
   per-action network, §9.7), reversing the v0.1 "no art scraping" non-goal — while never touching
   ROMs/BIOS? R: Yes: loader + art + cheats are staged from named sources; ROMs/BIOS stay local-only.
6. (v0.2) Ember path (copy BIN/CUE as-is, no VCD conversion) as a first-class job kind alongside
   POPSTARTER VCD? R: Yes — `copy-ps1-ember`, with mandatory `bios.bin` presence gate.
7. (v0.2) Drive formatting stays out of scope (validate + guide only, §2.11)? R: Yes — the app never
   partitions/formats; destructive ops stay with OS tools.

## 11. Suggested milestones

1. **M1 – Core engine:** library import, disc-type detection, CUE/BIN→VCD conversion, USBExtreme
   split, OPL folder-tree writer — CLI-only, no UI, unit-tested against sample files.
2. **M2 – Sequential queue + API:** job model, SQLite persistence, sequential executor, REST/WS API.
3. **M3 – Frontend:** Import/Destination/Queue/Settings views, dark/light theme, live progress.
4. **M4 – Multi-disc & polish:** DISCS.TXT/VMCDIR.TXT generation/validation, resumability, packaging
   for Windows/macOS/Linux, docs.
5. **M5 – Loader provisioning & enrichment:** RiptOPL staging (§2.7), cover art (§2.8), cheats (§2.9),
   Ember path (§2.10), drive pre-flight (§2.11). See BUILD-PLAN.md.

## 12. Changelog

- **v0.2:** session-hardened revision from a full real-hardware bring-up (SCPH-77001/79010 Matrix
  units, FMCB 1.8b/1.953/1.966, RiptOPL rolling, FAT32 SD + USB-HDD). New §§2.7–2.11 (RiptOPL, art,
  cheats, Ember, drive prep); GameID extraction rule (§2.3); goals 9–10; §9.7 network posture
  replaces offline-absolute; Q5–Q7 resolved. **Corrections to v0.1:** USBExtreme sets live at device
  root (`ul.cfg` + `ul.*`), not in `DVD/`; folder table completed (`EMBER/ CHT/ APPS/` conventions).
