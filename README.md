# TinyPS2Manager — OPL Backup Manager

Cross-platform desktop app that prepares a PS2/PS1 library for OPL/RiptOPL on a USB or MX4SIO drive (or a local staging folder). It ingests raw PS2 `.iso` and PS1 `.cue`/`.bin` sources, classifies PS2 discs as `CD` vs `DVD`, converts PS1 sets to `.VCD` (and validates `DISCS.TXT`/`VMCDIR.TXT` for multi-disc), splits >4 GiB sets into USBExtreme for FAT32, builds the correct OPL tree (`DVD/ CD/ POPS/ ART/ CHT/ APPS/ …` with optional BDM prefix), and copies **one file at a time in queue order** to avoid fragmentation on BDM devices.

Sources of truth: [`OPL-Backup-Manager-SPEC.md`](OPL-Backup-Manager-SPEC.md) (requirements, OPL/POPStarter rules) and [`BUILD-PLAN.md`](BUILD-PLAN.md) (implementation plan, milestones, invariants).

## Quickstart

### Prereqs

- Go 1.22+ (stdlib `slog`, `modernc.org/sqlite` pure-Go — no cgo)
- Node 22 + npm (only for the Svelte SPA; headless `oplbm serve` works without it)
- Wails CLI v2.16+ (only for the native desktop window: `go install github.com/wailsapp/wails/v2/cmd/wails@latest`)

### Build & run

```bash
# 1) Frontend (embeds into the Go binary via web/embed.go)
./scripts/build-web.sh          # npm ci + vite build -> web/dist

# 2) Full build (web + Go + third-party notices)
./scripts/build.sh              # also runs gen-third-party.sh; wails desktop when available
# headless only (no wails/webkit needed):
#   GOOS=windows GOARCH=amd64 go build -o dist/oplbm.exe ./cmd/oplbm
#   GOOS=linux   GOARCH=amd64 go build -o dist/oplbm-linux-amd64 ./cmd/oplbm

# 3) Run
./dist/oplbm serve --bind 127.0.0.1:41337   # REST + SSE on localhost, SPA at /
# Desktop window (no TCP bind needed, asset server hosts API+SPA same-origin):
./dist/oplbm                                   # or: wails dev / wails build -tags desktop

# 4) CLI (scriptable, no browser)
./dist/oplbm import /path/to/sources --db /tmp/oplbm.db
./dist/oplbm inspect <id|path>
./dist/oplbm convert --out /tmp/vcds <ps1-id|.cue>
./dist/oplbm split  --out /tmp/split <game.iso>
./dist/oplbm tree   --plan jobs.json --dest /media/usb --prefix OPL
```

### Desktop flow

1. **Library** — pick a source folder (`POST /api/library/import`), see PS2 `CD`/`DVD`/PS1 badges and detection method, fix type inline, group multi-disc (PS2 `Title (Disc N).iso`, PS1 up to 4 `*.VCD`).
2. **Destination** — pick a mounted volume or folder, see filesystem + free space, set BDM prefix, override FAT32/exFAT when detection is unknown.
3. **Queue** — reorder jobs, watch phase progress (Converting → Splitting → Copying → Verifying → Done) and ETA; pause/resume, retry/skip. Banner: *“Transfers run one at a time to prevent disc fragmentation on your OPL device.”*
4. **Activity/Settings** — live run, job history, theme (light/dark/system), staging dir, split threshold.

First OPL boot after staging is manual (RiptOPL docs): enable USB/MX4SIO in Game Sources → start mode → Save Changes → L3 for PS1.

## Key constraints (enforced)

- Pure Go, no cgo (except the `desktop` build tag for Wails), no shelling to external binaries.
- Streaming I/O only — buffers ≤ 4 MiB, never loads a full ISO/BIN.
- Source files are read-only; never mutates/deletes originals.
- Exactly one destination writer per volume at a time (sequential queue per device).
- Validation first: `DISCS.TXT`/`VMCDIR.TXT` and USBExtreme output validated before enqueue (`oplfs.ValidatePlan` → 422 on violation).
- State persists in SQLite so kill/restart resumes and partial `.oplbm.*` files are cleaned or resumed.

## Project layout

```
cmd/oplbm/          CLI + serve + Wails desktop (desktop.go //go:build desktop)
internal/
  library/          scan, hash, 4-step detection (override/inspected/heuristic/database), SQLite store, GameID
  isotool/          ISO9660/UDF inspection
  cuebin/           CUE parser + streaming VCD writer
  usbextreme/       1 GiB split writer + ul.cfg index
  oplfs/            OPL tree builder + DISCS/VMCDIR validators
  queue/            sequential executor (per-device locks, prep-ahead, verify, pause/resume, attempt cap 3)
  transfer/ + fsinfo  destination abstraction + per-OS FAT32/exFAT probing
  api/              REST + SSE (/api/*, localhost-only by default)
  config/           settings store + staging path
  logging/          slog JSON file sink (~/.oplbm/logs/oplbm-YYYYMMDD.log, 10 MiB × 5, date roll)
  riptopl/art/cheats  M5 stubs (loader provisioning & enrichment)
web/                Svelte + Vite SPA (web/dist embedded via go:embed)
scripts/            build-web.sh, build.sh, gen-third-party.sh, e2e.sh, m1-demo.sh
build/              Wails resources (appicon, darwin Info.plist, windows manifest; /build/bin is output)
```

## Lint / vet / typecheck / tests

```bash
go vet ./...
go vet -tags desktop ./...
gofmt -l cmd internal testdata scripts   # must be empty

go test ./...                             # unit + integration
go test -tags=genfixtures ./testdata      # regenerate synthetic fixtures

# cross-build (pure-Go, no CGO — desktop tag excluded)
GOOS=windows GOARCH=amd64 go build -o dist/oplbm.exe ./cmd/oplbm
GOOS=linux   GOARCH=amd64 go build -o dist/oplbm-linux-amd64 ./cmd/oplbm
GOOS=linux   GOARCH=arm64 go build -o dist/oplbm-linux-arm64 ./cmd/oplbm
GOOS=darwin  GOARCH=amd64 go build -o dist/oplbm-darwin-amd64 ./cmd/oplbm
GOOS=darwin  GOARCH=arm64 go build -o dist/oplbm-darwin-arm64 ./cmd/oplbm

# Wails desktop (requires webkit/gtk on Linux: sudo apt install libwebkit2gtk-4.1-dev)
wails build -tags desktop          # -> build/bin/oplbm (plus per-platform CI bundles)
./scripts/gen-third-party.sh        # refresh THIRD_PARTY.md + licenses/

cd web && npm run check             # svelte-check
cd web && npm run build             # vite

./scripts/e2e.sh                    # bootup: CLI + API + browser UI (UI skips without Chromium)
```

## Packaging

- `scripts/build.sh` builds `web/dist`, regenerates `THIRD_PARTY.md` + `licenses/`, produces `dist/oplbm` (headless) and, when Wails is installed, `build/bin/oplbm` desktop bundle.
- CI (`.github/workflows/ci.yml`): `vet-test`, `web`, `cross-build` (pure Go), and `wails` matrix (linux/windows/darwin amd64+arm64) that installs Wails deps, builds `-platform` with `-tags desktop`, and bundles each `build/bin/` with `THIRD_PARTY.md` + `licenses/` into `dist/oplbm-<os>-<arch>.{tar.gz,zip}`.
- Each archive ships `THIRD_PARTY.md` (all transitive Go modules, from `go list -m all`) and `licenses/<module>/` — source via `scripts/gen-third-party.sh`.

## Logs

Structured JSON logs at `~/.oplbm/logs/oplbm-YYYYMMDD.log` (rotated at 10 MiB, keep 5, date roll). In-app job history is the same SQLite `jobs` rows (N6 resumability).

## Screenshots

> Placeholder — run `oplbm serve`, open `http://127.0.0.1:41337`, capture Library / Destination / Queue / Settings views. PRs should attach screenshots for UI changes.

## License & third party

No license file yet. Third-party Go modules bundled with the binary are listed in [`THIRD_PARTY.md`](THIRD_PARTY.md) with full texts in `licenses/` (regenerated by `scripts/gen-third-party.sh`).

## Further reading

- Spec: `OPL-Backup-Manager-SPEC.md` §§2.1–2.11 (folder rules, splitting, detection, VCD, multi-disc, fragmentation, RiptOPL/art/cheats/Ember/pre-flight — §2.7–2.11 are M5).
- Plan: `BUILD-PLAN.md` §§3–6b (M1 engine → M2 queue/API → M3 frontend → M4 polish/packaging → M5 loader & enrichment), §7 cross-cutting tests, §8 definition of done.
