# OPL/POPSTARTER Backup Manager — Build Plan

Derived from `OPL-Backup-Manager-SPEC.md` v0.1. Each spec section is reviewed below
with concrete, ordered, verifiable work items. Open questions from §10 are flagged
upfront because they gate scope.

---

## 0. Spec iteration summary

| Spec § | Topic | Translates to… |
|---|---|---|
| §2.1 | OPL folder layout | `internal/oplfs` tree builder |
| §2.2 | Filesystem splitting rules | `internal/usbextreme` + transfer fs-type detection |
| §2.3 | CD vs DVD detection | `internal/library` detector + `internal/isotool` |
| §2.4 | PS1 → VCD conversion | `internal/cuebin` merge engine |
| §2.5 | Multi-disc (PS1+PS2) | `internal/oplfs` DISCS.TXT/VMCDIR.TXT generators |
| §2.6 | Fragmentation → sequential queue | `internal/queue` executor + `internal/transfer` writer |
| §2.7 | RiptOPL provisioning | `internal/riptopl` stager (new, M5) |
| §2.8 | Cover art | `internal/art` fetcher/normalizer (new, M5) |
| §2.9 | Cheats | `internal/cheats` DB parse + `.cht` builder/validator (new, M5) |
| §2.10 | Ember path | `copy-ps1-ember` job kind + `bios.bin` gate (new, M5) |
| §2.11 | Drive prep/validation | `internal/transfer` pre-flight extension (new, M5) |
| §3 | Goals | Acceptance criteria checklist |
| §4 | Non-goals | Explicitly excluded features (keep scope honest) |
| §5 | Architecture (Go+SPA, embed.FS, REST+WS) | Repo scaffolding, build system |
| §6 | Backend detailed design | Module tree, packages, API surface |
| §7 | Frontend | `web/` Svelte app, theming, WS progress |
| §8 | Data model | SQLite schema, DTOs |
| §9 | Non-functional | Stream budgets, cross-build matrix, logging, validation-first |
| §11 | Suggested milestones | Phasing (M1–M4) — used as the plan backbone |

Non-functional invariants that permeate every work item (call them out once, enforce
everywhere):
- **N1** Streaming I/O only — no full-ISO loads; buffers ≤ 4 MB.
- **N2** No cgo, no shelling to external binaries.
- **N3** Source files are read-only; never delete/mutate.
- **N4** Validation-first: DISCS.TXT/VMCDIR.TXT and USBExtreme output validated
  *before* enqueue, not just at write.
- **N5** Exactly one destination writer per destination volume at any time.
- **N6** State persists (SQLite) so kill/restart resumes.

---

## 1. Resolved decisions (from spec §10, answered in OPL-Backup-Manager-SPEC.md)

Q1–Q4 are answered; fix the scope accordingly. These are source of truth and not
to be re-litigated without superseding the SPEC file itself.

| # | Answer | Affects |
|---|---|---|
| Q1 | No optical disc burning; `CD/DVD` = OPL folder buckets only. | Scope: no burning code path anywhere. |
| Q2 | Write directly to mounted USB/SD card drive (not staging-folder only). | `internal/transfer` must ship real drive impl in M2; fs-info detection is on the critical path. One in-flight writer per drive is mandatory. |
| Q3 | Bundle a known-games database with the app. | `internal/library` detector path #4 is required (not optional); design embed + user-extensibility. |
| Q4 | Wrapped desktop app (Wails). | Scaffolding pulls Wails; `cmd/oplbm` becomes a Wails app; single binary includes native shell. Frontend bundle still embedded via `go:embed`. Build matrix changes (per-OS Wails build). |
| Q5 | Enrichment downloads on demand (RiptOPL/art/cheats), ROMs/BIOS never. | New `internal/{riptopl,art,cheats}`; §9.7 network posture; M5 milestone. |
| Q6 | Ember `copy-ps1-ember` first-class job kind + `bios.bin` gate. | Queue dispatch + `oplfs` layout + validation; M5. |
| Q7 | No format/partition in app (validate + guide only). | `internal/transfer` pre-flight checks; M5. |

---

## 2. Repository scaffolding (Phase 0, ~1 day)

Work items, ordered:

1. `git init` already done. Create `.gitignore` (Go build output, `web/dist`,
   `web/node_modules`, SQLite test files, per-OS editor noise).
2. `go mod init github.com/jo/TinyPS2Manager` (or final path).
3. Create the directory skeleton exactly as spec §6.2, plus the v0.2 packages:
   `cmd/oplbm/`, `internal/{library,isotool,cuebin,usbextreme,oplfs,queue,transfer,api,config}/`
   (M1–M4) and `internal/{riptopl,art,cheats}/` (M5 — stubs now, implemented in M5).
   `web/`, `testdata/`, `scripts/`. No `docs/` (single source of truth: this file + SPEC).
4. Add pinned dependency list (write to `go.mod` via `go get`):
   - `modernc.org/sqlite` (pure-Go SQLite; still required under Wails)
   - `golang.org/x/sys` (per-OS fs info)
   - `github.com/wailsapp/wails/v2` (Q4 — wrapped desktop app)
   - `github.com/go-chi/chi/v5` for routing inside the Wails app.
   - WS: use SSE behind Wails instead of WebSocket — simpler with the embedded
     server (no WS upgrade needed); pick one and stick to it (SSE).
   - Validate *nothing* else is pulled in; reopen if a package introduces cgo.
5. Add CI scaffolding (`.github/workflows/ci.yml`):
   - `go vet`, `go test ./...`, `go build ./...`
   - Cross-compile matrix matching §9.2 (win/amd64, darwin/amd64, darwin/arm64,
     linux/amd64, linux/arm64) — catches platform-tagged code early.
6. Add test data fixtures under `testdata/` (see §3.7 for how to obtain them
   without committing real game images).
7. Create `AGENTS.md` with the lint/test/build commands so future sessions
   auto-run them (per opencode instructions).

Exit criteria: empty-binary build works on all 5 GOOS/GOARCH combos, CI green,
skeleton compiles with stub packages, `wails doctor` passes on dev host.

---

## 3. M1 — Core engine (CLI-only, spec §11.1)

Milestone owner command: `cmd/oplbm` has a `import`, `inspect`, `convert`, `split`,
`tree` subcommand hitting the real engines against `testdata/`. No HTTP, no UI.

### 3.1 `internal/isotool` — ISO9660/UDF read-only inspection (spec §2.3, §6.2)

1. Define `ISOInfo { VolumeLabel, SectorSize, SectorCount, DataSizeBytes,
   HasUDF, HasISO9660, DetectionConfidence }`.
2. Parser:
   - Open file with `os.Open` + `bufio.Reader`. Never `ioutil.ReadFile`.
   - Parse Primary Volume Descriptor at sector 16 (offset 0x8000) for ISO9660.
   - Detect UDF bridge by scanning VDP stack for tag `NSR02`/`NSR03`.
   - Compute `DataSizeBytes = SectorCount * SectorSize` (2048 for ISO9660).
3. Tests: synthetic tiny ISOs (use `testdata/mkiso.golden` + a Go helper that
   emits a minimal ISO9660 PVD) — no copyrighted game data.
4. Expose `Inspect(path string) (ISOInfo, error)`; no side effects.

### 3.2 `internal/library` — source scan + disc-type detection (spec §2.3)

Order of work:

1. Content-hash: SHA-256 of first 1 MiB + size, for dedupe/override-cache keying
   (cheap, deterministic enough for cache; full hash only if collision).
2. Scanner: walk a root, group `.cue`+`.bin*` pairs for PS1, treat `.iso` as PS2.
   Emit deduped `LibraryItem` stream.
3. Detector (priority order — must be literal in code with named steps):
   1. `override` — persisted per-hash field from SQLite.
   2. `inspected` — call `isotool.Inspect` and apply rules from §2.3 #2
      (UDF bridge → DVD; ISO9660 ≤700MB CD-XA → CD).
   3. `heuristic` — split at 700 MiB.
4. `database` — bundled JSON list at
       `internal/library/data/ps2_cd_titles.json` (embedded via `go:embed`),
       required (Q3) and user-extensible by dropping
       `ps2_cd_titles.user.json` in the user config dir.
   Final decision = first step that yields an answer; DB wins ties vs heuristic
   per §2.3 #4. Record `detectionMethod` on the item.
4. PS1 path: mark `platform=ps1`, `discType=null`; group multi-disc by sibling
   `.cue` files whose track listing shares a title prefix (fallback: user groups
   them via API).
6. **GameID extraction (spec §2.3.5, v0.2):** stream `SYSTEM.CNF` out of the ISO (directory
   records + that file only), parse `BOOT2`, record `gameId` + `gameIdUncertain` flag (set for
   mods/translations whose serial names a different base game). `gameId` is the join key for
   `internal/art` and `internal/cheats` in M5 — no GameID, no enrichment, games still transfer.
7. SQLite schema v1 for `library_items` table (see §4 below). Add `game_id TEXT NULL` +
   `game_id_uncertain INTEGER NOT NULL DEFAULT 0` columns now (M5 reads them later). Keep migrations
   in `internal/library/migrations/` with `modernc.org/sqlite`.

### 3.3 `internal/cuebin` — CUE/BIN → VCD (spec §2.4)

This is the highest-risk component; do it first within M1.

1. CUE sheet parser (hand-written lexer; format is small):
   - Tokens: `FILE … BINARY`, `TRACK NN AUDIO/MODE1/…`, `INDEX NN MM:SS:FF`.
   - Track types of interest: `AUDIO`, `MODE1/2048`, `MODE1/2352`, `MODE2/2352`.
   - Reject unsupported encodings with a clear error, not a silent wrong merge.
2. BIN merge → VCD writer:
   - Open all referenced BINs streaming; allocate a 2 MiB buffer per `io.Copy`
     slice.
   - For each track, emit sectors in **2352-byte raw form**. For MODE1/2048
     tracks, synthesize the missing header/subchannel bytes per the CUE
spec (or, if quality risk is too high initially, store raw 2048 and add
      a `--mode=raw` flag defaulting to synthesized — decide in commit message
      of the cuebin PR).
   - Fold pregaps: `INDEX 00` gap is written as silence/zero sectors preceding
     `INDEX 01`. This is the crux of matching POPStarter's layout.
   - Never hold the merged image in memory; stream BIN → VCD with a single
     pass.
3. Disc-ID extraction: parse the PS-X system area (sector at LBA 4) for the
   `SLUS-XXXXX` / `SCES-XXXXX` / `SCUS-XXXXX` serial. Fall back to title-only
   filename.
4. Filename builder: `SXXX_NNN.NN.Title.VCD` with a strict sanitizer
   (forbidden: `/ \ : * ? " < > |`, length ≤ 89 chars, target ≤ 73 per §2.5).
5. Tests:
   - Golden round-trip: small generated CUE/BIN → VCD compared byte-for-byte
     against a reference image checked into `testdata/` (reference generated by
     a trusted tool once; only tiny, synthetic, non-copyrighted payload).
   - Multi-track regression: 2 audio + 1 data track; assert sector count and
     pregap insertion.
   - Stream-budget test: RSS must remain flat across a 2 GiB synthetic merge
     (use a pipe-backed fake BIN to avoid storing 2 GiB on disk).

### 3.4 `internal/usbextreme` — split writer (spec §2.2)

1. Define chunk size constant from the published USBExtreme format
   (verify against OPL wiki before coding; spec defers to "published format").
2. Writer `Write(destDir, baseName, src io.Reader, size int64)`:
   - Emits `ul.cfg`-style index + `ul.<baseName>.0`, `.1`, … numbered chunks.
   - Streaming: never hold a chunk fully; small ring buffer.
3. Reader (for verification path): parse index, re-read chunks in order, return
   a combined `io.Reader`.
4. Round-trip test: random-data source ≥ 4 GiB (use `io.LimitReader` on
   `crypto/rand`) split then re-read, assert SHA-256 equality with the
   original generator.

### 3.5 `internal/oplfs` — OPL tree builder + validators (spec §2.1, §2.5)

1. Tree builder:
   - Given a destination root + BDM prefix + list of planned files with their
     bucket and optional per-disc subfolder, emit the full path list to create.
   - Buckets: `DVD/` + `CD/` (plain `.iso` only), **device root** for USBExtreme
     (`ul.cfg` + `ul.*` — spec §2.2 correction, never inside `DVD/`), `POPS/` (VCD),
     `EMBER/games/<Name>/` (Ember BIN/CUE, names preserved), `ART/ CFG/ VMC/
     CHT/ APPS/ THM/ LNG/`, `APPS/RIPTOPL/`.
   - Validate folder names are exactly `DVD`, `CD`, `POPS` (case-sensitive).
   - Refuse to create POPS unless the job set contains a VCD (per §2.1 note
     that OPL itself never creates POPS).
2. DISCS.TXT generator+validator (spec §2.5):
   - Max 4 lines; each ≤ 73 chars (POPSLoader practical limit, stricter than
     the 89 in POPStarter docs).
   - Identical content into every disc's folder.
   - Validator runs at enqueue time (N4).
3. VMCDIR.TXT generator+validator (spec §2.5):
   - Single line, ≤ 103 bytes, no `/ \ :` chars.
   - Bare folder name (resolves inside `POPS/`).
   - Placed identically in every disc's folder.
   - Shared Slot0/Slot1 VMC pair: builder creates `<POPS>/<vmcdir>/` once.
4. Disc count guard: > 4 discs → error with redirect to "manual reinstall
   waves" rather than silent truncation.

### 3.6 CLI wiring (`cmd/oplbm`)

Subcommands (kept simple, behind `flag`):
- `oplbm import <dir>` — scan, persist to DB.
- `oplbm inspect <id|path>` — print detected type + method.
- `oplbm convert <ps1-item-id> --out <dir>` — CUE/BIN → VCD (for testing).
- `oplbm split <iso-path> --out <dir>` — USBExtreme split.
- `oplbm tree --plan <jobs.json> --dest <root>` — dry-run emit the exact path
  tree that would be created (enables review before queue).

Exit criteria for M1: each subcommand exercises the real engine against
`testdata/`; `go test ./...` green; CI cross-build matrix green; one demo
script `scripts/m1-demo.sh` end-to-end on synthetic fixtures.

### 3.7 Test-data policy (M1 and onward)

- Never commit real game ISOs/BINs.
- Build a `testdata/gensrc` Go program that emits synthetic ISO9660 images and
  CUE/BIN sets at known sizes (4 MiB, 700 MiB, 4.5 GiB) using only deterministic
  filler bytes.
- Run `go test ./...` with `-tags=genfixtures` to regenerate; fixtures stay
  under 100 MiB total by streaming-limiting verifier tests.

---

## 4. M2 — Sequential queue, SQLite persistence, REST/WS API (spec §11.2)

### 4.1 Schema (`internal/queue/migrations/`)

Three tables mirroring spec §8 DTOs exactly:
- `library_items` — PK `id`, with `content_hash`, `detection_method`,
  `disc_type`, `disc_group_id` (nullable), `status`.
- `jobs` — PK `id`, FK to `library_item_id`, `destination_id`, `kind`,
  `order`, `status`, `phase`, `bytes_total`, `bytes_done`, `error`,
  timestamps.
- `destinations` — PK `id`, `path`, `kind`, `filesystem`, `bdm_prefix`,
  `free_bytes`, `updated_at`.
- Plus `schema_version` for migrations.

Indexes: `library_items(content_hash)` for dedupe; `jobs(status,order)` for the
executor's next-job lookup; `jobs(destination_id,status)` to enforce
"one writer per destination" cheaply.

### 4.2 Sequential executor (`internal/queue`)

Implement against spec §6.4 as literal numbered requirements:

1. Single-flight guard: per-destination mutex (in-process) **and** a DB lease
   row (`jobs.dest_lock` = `running`) so a second process can't double-write
   even if launched. Assert invariant with a unit test that starts two
   executors against the same DB and proves only one advances.
2. Ordered iteration: `SELECT … FROM jobs WHERE status='pending' AND
   destination_id=? ORDER BY "order"` — order adjustable via PATCH.
3. Job kind dispatch (copy | split-and-copy | convert-and-copy | copy-ps1-ember | enrich).
   Each kind has a `Prep(ctx, job)` step that may use the staging dir (concurrent with a
   prior job's *write* phase) and a `Write(ctx, job)` step that is serialized. `enrich`
   (art/cheat/loader files, M5) is a separate low-priority class that never interleaves with
   game writes to the same volume.
4. Prep/write pipeline: a fixed-size worker pool of size = number of distinct
   destinations (typically 1). Conversion happens in staging; the final
   move-to-destination is in the writer goroutine.
5. Contiguity preference (spec §6.4 #5): create the temp file on the *same
   volume* as the destination when a convert step is required, using
   `os.CreateTemp(destDir, ".oplbm.*")` then `os.Rename`. Document why this
   matters (single allocation extent) in the package doc comment.
6. Verify-on-write: streaming SHA-256 of the destination read-back vs the
   source hash; size sanity for plain copies.
7. Error handling: queue auto-pauses; keeps the failed job in `error` state;
   user-driven retry/skip/abort via PATCH.
8. Pause/resume: a `queue.Pause()` global flag checked between jobs; partial
   files deleted on cancel.
9. Checkpoint: each `bytes_done` increment is bounded to a DB write every 1 %
   (or 1 s, whichever first) to avoid SQLite write amplification during
   multi-GB copies.

Tests:
- A fake `Transfer` backend that records write calls in order → assert
  no two destinations ever have overlapping write windows when same-device.
- A fake that mid-copy returns an error → assert queue pauses, partial file
  deleted, state consistent across simulated restart (close+reopen DB).

### 4.3 `internal/transfer` — destination abstraction + fs detection

1. `Destination` interface with two impls: `localDrive`, `localFolder`.
2. `internal/transfer/fsinfo` per-OS implementations (build-tagged):
   - `fsinfo_linux.go`: parse `/proc/mounts`, `unix.Statfs`.
   - `fsinfo_darwin.go`: `getmntinfo` via cgo-free `unix.Statfs` + plist
     fallback `diskutil info -plist <mount>` parsed with
     `howett.net/plist` (pure Go).
   - `fsinfo_windows.go`: `GetVolumeInformationW` via `golang.org/x/sys/windows`.
   - Shared stub returning `unknown` for any OS not matched — caller falls
     back to user-supplied FAT32/exFAT toggle per §6.3.
3. Free-space check before each job — refuse enqueue if `bytes_total +
   existing_in_flight > freeBytes`.
4. Atomic write-then-rename where the OS/filesystem allows; on FAT32,
   `os.Rename` across same volume is atomic-ish, document the caveat.

### 4.4 HTTP API (`internal/api`, spec §6.5)

1. Router (chi) — mount endpoints exactly as spec §6.5 lists. Keep them thin;
   handlers translate to/from `internal/queue` and `internal/library`.
2. Input validation with a single `validate` helper returning
   `{field, msg}` slices; reject unknown fields.
3. **Validation-first hook** (N4): `POST /api/queue` runs
   `oplfs.ValidatePlan(plan)` (DISCS.TXT, VMCDIR.TXT, USBExtreme chunk count)
   *before* inserting jobs; returns 422 with the offending constraints.
4. SSE vs WebSocket: pick one. WS is spec-default; SSE is simpler if reverse-
   proxied. SSE is the choice (also matches the Wails app decision in §1).
    The event payload is:
   `{jobId, phase, bytesDone, bytesTotal, etaSec, message}`.
5. Throttle WS emits to ~10/sec per client (spec §7) — server-side, not client.
6. Auth: localhost-only bind by default (`127.0.0.1:<port>`); document the
   risk of binding `0.0.0.0` clearly; add a `--bind` flag.

### 4.5 M2 exit criteria

- `cmd/oplbm serve` exposes the full REST+WS surface; an integration test
  (`internal/api/integration_test.go`) drives the API end-to-end against a
  local-folder destination and a synthetic source.
- Simulated Kill (`SIGKILL`) mid-copy, relaunch → queue resumes from the last
  checkpoint; partial file cleaned or resumed correctly.
- Cross-build still green.

---

## 5. M3 — Frontend SPA (spec §7, §11.3)

### 5.1 Stack decisions

- **Svelte + Vite** (spec default). Preact alternative only if bundle-size
  regression forces it; measure, don't assume.
- Plain CSS custom properties for theming — no Tailwind/Bootstrap.
- `vite build` outputs to `web/dist`; `go:embed web/dist` in
  `cmd/oplbm/embed.go`. Build script `scripts/build-web.sh` runs npm install
  then `vite build` before `go build`.

### 5.2 Views (spec §7)

1. **Import/Library**:
   - Folder picker → POST `/api/library/import`.
   - Table: title, platform badge, disc-type badge (CD/DVD/PS1), size,
     detection method tooltip.
   - Inline correction: a per-row `<select>` for disc type; PATCH on blur.
   - Multi-disc grouping: drag rows into a group; persists `discGroupId`.
2. **Destination**:
   - Mountable-drive list from `GET /api/destinations`; filesystem + free
     space column. Manual fs-type override toggle (FAT32 default).
   - BDM prefix text input (default empty).
3. **Queue**:
   - Reorderable list (Svelte action `use:dragSort` or a tiny lib; verify
     bundle impact).
   - Per-job: phase progress bar (Converting → Splitting → Copying →
     Verifying → Done), ETA, retry/skip/abort buttons.
   - Banner: "Transfers run one at a time to prevent disc fragmentation on
     your OPL device." (spec §7 explicit copy).
   - WS-driven updates throttled to ~4–10/sec client-side (in addition to
     server-side throttle).
4. **Settings**:
   - Theme (light/dark/system), default split threshold bytes, staging temp
     dir, destination fs-type default.

### 5.3 Theming

- `:root` defines `--bg`, `--fg`, `--accent`, `--danger`, etc.
- `[data-theme="dark"]` override block.
- `prefers-color-scheme` media query sets initial; user override persisted
  via `PUT /api/settings` and reflected server-side so a fresh tab matches.

### 5.4 Performance/accessibility

- Throttle/`requestAnimationFrame`-batch progress renders (spec §7).
- Keyboard navigation for queue reorder; ARIA live region for phase changes.
- Bundle budget: warn in CI if `web/dist` uncompressed > 250 KiB.

### 5.5 M3 exit criteria

- Developer can: import a folder, classify, pick destination, enqueue, watch
  progress, pause/resume — fully from the browser UI without touching CLI.
- Lighthouse perf ≥ 90 on a cold load of the localhost UI.
- Cross-browser sanity (Chromium + Firefox; Safari best-effort).

---

## 6. M4 — Multi-disc polish, resumability, packaging (spec §11.4)

### 6.1 Multi-disc flows end-to-end

- **PS2 multi-disc**: enforce consistent naming `Title (Disc N).iso`; UI groups
  into one row that enqueues as a single atomic group (a sub-order).
- **PS1 multi-disc**: full path — pick up to 4 PS1 items in UI, the queue
  generates (VCD per disc, per-disc folder, DISCS.TXT in each, VMCDIR.TXT in
  each, shared VMC folder). All artifacts pass `oplfs.ValidatePlan` before
  enqueue (this is where N4 bites hardest).
- > 4 discs: hard error with a copy-pasteable manual-reinstall instruction.

### 6.2 Resumability hardening

- On boot: scan destination dir for `.oplbm.*` temp files → either resume (if
  matching job record exists and partial is byte-checksummable) or delete.
- Queue table has `attempt_count`; surface in UI; cap auto-retry at 3 with
  user-override.

### 6.3 Logging

- `slog` (stdlib) JSON handler → `~/.oplbm/logs/oplbm-YYYYMMDD.log`, rotated
  by size (10 MiB) keeping last 5.
- In-app job history page reading the same DB rows; no separate log viewer
  for v1.

### 6.4 Packaging

- Packaging uses Wails (Q4 — decided, not a stretch). GitHub Actions matrix
  produces per-platform Wails bundles: `oplbm-<os>-<arch>.{zip,tar.gz,dmg,AppImage}`
  as appropriate.
- Each archive contains: the Wails-built binary, `THIRD_PARTY.md`
  (Go deps + bundled JSON), `licenses/`.

### 6.5 Docs (kept minimal — single source of truth)

- `README.md` — user quickstart + screenshots (project root).
  The SPEC file (`OPL-Backup-Manager-SPEC.md`, project root) is the
  authoritative requirements source; this BUILD-PLAN.md is the authoritative
  implementation plan. No `docs/` directory; no ADR files — decisions
  live inline in §1 of this plan.

### 6.6 M4 exit criteria

- A real-world 3-disc PS1 game import through Queued copy onto a real FAT32
  USB stick, then verified to boot in OPL+POPStarter on a PS2 — the gold
  acceptance test. (Manual, recorded once.)
- All N1–N6 invariants covered by automated regression tests (one
  `invariants_test.go` that asserts each as a tagged suite).

---

## 6b. M5 — Loader provisioning & enrichment (spec §§2.7–2.11, v0.2)

Requires M1–M4 done (needs `gameId` column, queue dispatch, `oplfs` buckets, fsinfo). All network
is explicit per-action with the URL shown pre-fetch and the pinned version recorded in SQLite
afterwards (spec §9.7). Pure-Go HTTP via stdlib only; add no new module dependencies.

### 6b.1 `internal/riptopl` — loader stager (spec §2.7)

1. Release client: fetch the unified `RIPTOPL-<version>.zip` asset list for a tag (`rolling` or a
   preserved tag); select `APP_RIPTOPL-PS2DEVPINNED/RIPTOPL.ELF`, else next labelled flavour in
   documented order. Stream-download with progress reporting into staging; SHA-256 recorded.
2. Placement: `APPS/RIPTOPL/RIPTOPL.ELF` under root-or-prefix via `oplfs` (reuses tree builder).
3. First-boot checklist generator: returns the ordered manual steps (enable USB in Game Sources →
   start mode → Save Changes → L3 for PS1) rendered by the UI as a per-destination checklist.
   The app never pretends the drive is ready before this.
4. Tests: golden asset-selection against a fixture release JSON; placement golden vs `oplfs`.

### 6b.2 `internal/art` — cover fetcher/normalizer (spec §2.8)

1. Key builder: PS2 → `<GameID>_COV.png`; POPSTARTER VCD → `<VCD basename>_COV.png` (or PS1-ID form
   when the VCD follows `SXXX_NNN.NN.Title.VCD`); Ember → `<folder>_COV.png`.
2. Source patterns (constants, documented): libretro per-system submodule raw URLs (PS1), GameTDB
   cover URLs with region mapping (PS2). HTTP fetch with timeout + retries; validate PNG magic +
   dimensions; normalize to 512×730 (PS2) / 512×512 (PS1) with fit-not-stretch.
3. Custom covers: local generator path (canvas + title text) flagged `customArt: true`; hand files
   in the watched folder always win and are never overwritten.
4. Missing art is a warning badge, never an enqueue blocker; art jobs are `enrich`-class (never
   interleave with game writes to the same volume).

### 6b.3 `internal/cheats` — cheat DB + `.cht` builder (spec §2.9)

1. Parsers: CheatDevice `CheatDatabase.txt` (title-keyed sections → map via library title/GameID,
   record `regionMatched`); widescreen pack (already per-`<GameID>.cht`, used whole).
2. Builder: one file per game, gameplay OR widescreen OR hand file (user picks winner; never merge
   — OPL honors only the first `9`-type master). Validate: exactly one usable master line, ≤ 250
   named cheats (drop extras with warning), flag reliance on engine-skipped types `8/A/B`.
3. `gameIdUncertain` titles (mods) require explicit user confirm before any cheat file is staged.

### 6b.4 Ember job kind + `bios.bin` gate (spec §2.10)

1. Queue kind `copy-ps1-ember`: plain sequential copy of `.cue` + all referenced `.bin`s into
   `EMBER/games/<Name>/`, names preserved byte-for-byte (CUE references them).
2. Gate: destination validation fails closed when `EMBER/bios.bin` is absent or ≠ 512 KB, with the
   exact remediation message ("place your own `EMBER/bios.bin`"). The app never downloads, fabricates,
   or placeholders a BIOS.
3. VCD (`convert-and-copy`) stays the default recommendation; UI shows the Ember-beta vs POPSTARTER-
   mature trade-off per title.

### 6b.5 Drive pre-flight (spec §2.11, extends `internal/transfer`)

1. Checks at destination selection (block enqueue on fail): partition table is `dos` (extend fsinfo:
   read table type on Linux via sysfs/`blkid`-free parsing — pure Go, no shelling); partition type
   vs fs toggle; fs matches toggle; `ul.cfg`+`ul.*` pre-existing sets parse; free-space probe.
2. Warnings (non-blocking): non-flash device hints (spinning/SD-adapter heuristics where detectable),
   exFAT non-default cluster size, pre-existing fragmentation risk (> N files, suggest re-copy flow).
3. Linux mount path prefers `udisksctl` (no sudo); any `sudo`-required flow is a hard error with
   guidance text, never a password prompt.

### 6b.6 CLI / API / UI additions

- CLI: `oplbm gameid <iso>` (print serial), `oplbm art --device <id> [--missing-only]`,
  `oplbm cheats --device <id>`, `oplbm riptopl --device <id> --tag rolling`,
  `oplbm preflight --device <id>`.
- API: `POST /api/enrich/{art,cheats,riptopl}`, `GET /api/destinations/{id}/preflight`,
  `GET /api/library/{id}/enrichment` (art status, cheat source, region flags).
- UI: per-title art-status badge (found/custom/missing), cheat source picker + region warning,
  destination pre-flight panel with pass/fail/warn rows, first-boot checklist view.

### 6b.7 M5 exit criteria

- Real FAT32 SD card end-to-end: pre-flight passes → 1 PS2 ISO + 1 PS1 CUE/BIN (Ember) + RiptOPL +
  art + cheats staged → card boots RiptOPL on real hardware, game lists with cover, cheat menu
  present (manual on-console verification, recorded once).
- `go test ./...` green incl. golden `.cht` builds and art-normalize fixtures; cross-build green.

---

## 7. Cross-cutting test strategy

- **Unit**: per package, table-driven; golden file pattern for binary output.
- **Integration**: `internal/api/integration_test.go` (M2),
  `internal/queue/pipeline_test.go` with fake transfer.
- **End-to-end**: `scripts/e2e.sh` in M3/M4 driving the API against
  synthetic fixtures + a temp dir.
- **Property tests** for: CUE parser (random valid/invalid sheets),
  USBExtreme round-trip, DISCS.TXT constraint edge cases. Use `testing/quick`
  for the lower-stakes ones.
- **Stream-budget invariant** (N1): a tagged test that asserts RSS stays under
  a threshold while processing a 2 GiB pipe-backed fake source.

---

## 8. Definition of done (per milestone)

A milestone ships only when all of:
1. All work items above marked done with linked PRs.
2. `go vet`, `go test ./...`, cross-build matrix, and `npm run build` (where
   applicable) green on CI.
3. The milestone's exit criteria verified, with the verification
   command/step recorded in `BUILD-PLAN.md` §3.x/§4.x/§5.x/§6.x "Exit criteria".
4. Any decision deviating from spec §10 answers must be noted inline in §1
   before the PR merges.
5. `AGENTS.md` updated with new lint/test/build commands introduced.

---

## 9. Sequencing recommendation

- Phase 0 → M1 (engine + tests, no net) → M2 (API + queue) → M3 (UI) → M4
  (multidisc + polish) → M5 (loader provisioning + enrichment, the only milestone with
  user-initiated network) is the spec's own order.
- Highest technical risk is `internal/cuebin` (PS1 merge correctness). Begin
  its design + golden-image work on day 1 of M1, in parallel with the
  scaffolding, so it doesn't gate M1 exit late.
- Second-highest risk is per-OS filesystem detection; stub it during M1/M2
  with the user override and write the real implementations early in M2 so
  real-FAT32 integration tests can run.

---

## 10. Next action

Q1–Q7 are resolved (see §1). Start Phase 0 (`§2` of this plan): scaffolding,
Wails init, pinned deps, then M1 beginning with the highest-risk component
(`internal/cuebin` design + golden fixtures). M5 (`§6b`) starts only after M1–M4 exit.