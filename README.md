<p align="center">
  <img src="flatpak/io.github.JO0812.TinyPS2Manager.svg" width="128" height="128" alt="OPL Backup Manager icon" />
</p>

<h1 align="center">TinyPS2Manager</h1>
<p align="center"><strong>OPL Backup Manager</strong> — the safe, correct way to prepare a PS2/PS1 library for OPL / RiptOPL.</p>
<p align="center">Ingest <code>.iso</code> + <code>.cue/.bin</code> → classify, convert, split &amp; lay out a <code>DVD/CD/POPS/ART/CHT</code> tree — then copy <em>one file at a time</em> to avoid FAT32 fragmentation.</p>

<p align="center">
  <a href="https://github.com/JO0812/TinyPS2Manager/actions/workflows/ci.yml"><img src="https://github.com/JO0812/TinyPS2Manager/actions/workflows/ci.yml/badge.svg" alt="CI" /></a>
  <a href="https://golang.org"><img src="https://img.shields.io/badge/Go-1.26-00ADD8?style=flat&logo=go" alt="Go 1.26" /></a>
  <a href="https://wails.io"><img src="https://img.shields.io/badge/Wails-2.16-red?style=flat" alt="Wails" /></a>
  <a href="https://github.com/JO0812/TinyPS2Manager/releases"><img src="https://img.shields.io/github/v/release/JO0812/TinyPS2Manager?include_prereleases&label=release" alt="Release" /></a>
  <img src="https://img.shields.io/badge/platform-Windows%20%7C%20macOS%20%7C%20Linux-lightgrey" alt="Platform" />
  <img src="https://img.shields.io/badge/license-MIT-green" alt="License" />
</p>

<p align="center">
  <a href="#-install-prebuilt-30-seconds"><strong>Install</strong></a> •
  <a href="#-quickstart-build-from-source">Build</a> •
  <a href="#-how-it-works">How it works</a> •
  <a href="OPL-Backup-Manager-SPEC.md">Spec</a> •
  <a href="BUILD-PLAN.md">Plan</a>
</p>

---

### Why this exists

OPL's BDM driver caps at **64 fragments/file**. Filling a FAT32 stick in parallel fragments games until they won't boot. TinyPS2Manager stages the *entire* OPL tree correctly — then **queues every copy sequentially** with verification, resume, and validation *before* enqueue (`DISCS.TXT`/`VMCDIR.TXT`/`ul.cfg` → `422`).

> **Sources are never touched.** Streaming I/O (`≤4 MiB` buffers), SQLite resume (`~/.oplbm/logs/`), and a single writer per volume.

### ✨ Features

|  | What it does | Why it matters |
|---|---|---|
| **🎮 Auto-classify** | PS2 `CD` vs `DVD` via 4-step detector: `override → inspected (ISO9660/UDF) → heuristic (700 MiB) → bundled DB` + `GameID` from `SYSTEM.CNF:BOOT2` | Small DVDs stay DVD, CDs stay CD — no manual guessing |
| **💿 PS1 → VCD** | Hand-written CUE lexer + streaming `2352`-byte `VCD` writer (pregap `INDEX 00`, `2 MiB` buffer) | Matches POPStarter layout, no `bin/cue` merge tool needed |
| **🧩 Multi-disc** | Up to 4 `VCD`s + identical `DISCS.TXT` (≤73) + shared `VMCDIR.TXT` (≤103) in each `POPS/<Disc>/` | Real swap with Select+L2+R2, shared memory card |
| **✂️ USBExtreme** | `1 GiB` `ul.cfg` + `ul.*` at **device root** (never `DVD/`) | FAT32 `>4 GiB` games work; exFAT stays plain `.iso` |
| **⛓️ Sequential queue** | One writer/volume, prep-ahead pool, `0. oplbm.*` + `Rename`, SHA-256 verify, `attempts` cap `3` | No fragmentation, resumable after kill |
| **🎨 Enrichment** | On-demand `RiptOPL` (pinned flavour), `ART` (`GameTDB`/`libretro` → `512×730`/`512×512` normalize), `CHT` (CheatDevice + widescreen, `≤250` + `9`-type master) | Loader + covers + cheats without bundling stale data |
| **👻 Ember** | `copy-ps1-ember` — plain `CUE+BIN` → `EMBER/games/<Name>/`, `bios.bin` `512 KiB` gate | Beta path, no conversion, names preserved |
| **🛡️ Pre-flight** | MBR `dos` check, `0x0c/0x07` partition type, `fs` vs toggle, `ul.cfg` parse, free-space, `>100` files warning | Catches the “looks like an app bug” drive prep failures |

---

### 📦 Install (prebuilt, 30 seconds)

> Tagged `v*` releases via `softprops/action-gh-release` attach 8 artifacts — headless + Wails desktop — each with `THIRD_PARTY.md` + `licenses/`.

**Linux — tarball**

```bash
tar -xzf oplbm-linux-amd64.tar.gz      # Wails desktop (or -headless.tar.gz for pure-Go)
sudo install -Dm755 oplbm /usr/local/bin/oplbm
oplbm --help
```

**Linux — Flatpak (sandboxed)**

```bash
# Local build (Flathub submission in progress)
./scripts/build-flatpak.sh --install
flatpak run io.github.JO0812.TinyPS2Manager
# Permissions: --filesystem=host:rw --filesystem=/media:rw --filesystem=/run/media:rw --share=network
```

**Windows**

```powershell
Expand-Archive oplbm-windows-amd64.zip -DestinationPath .
.\oplbm-windows-amd64.exe --help
```

**macOS**

```bash
tar -xzf oplbm-darwin-arm64.tar.gz
open build/bin/oplbm.app  # or the extracted binary
```

### 🔨 Quickstart (build from source)

**Prereqs:** `Go 1.26` · `Node 22 + npm` · `Wails 2.16` (desktop only)

```bash
# 1) Web → go:embed
./scripts/build-web.sh          # vite build → web/dist (rewrites placeholder README.md)

# 2) Full build
./scripts/build.sh              # + gen-third-party.sh → dist/oplbm (+ build/bin/oplbm when Wails present)

# 3) Run
./dist/oplbm serve --bind 127.0.0.1:41337   # SPA at http://127.0.0.1:41337
./dist/oplbm                                 # desktop window (asset server, no TCP)

# 4) CLI (scriptable, no browser)
./dist/oplbm import /path/to/sources --db /tmp/oplbm.db
./dist/oplbm inspect 1
./dist/oplbm gameid /path/game.iso          # → SLUS_213.85
./dist/oplbm preflight --device 1
./dist/oplbm riptopl --device 1 --tag rolling
./dist/oplbm art --device 1 --missing-only
./dist/oplbm cheats --device 1
```

**Desktop flow:** `Library` (badges, fix `CD/DVD` inline, group 2-4 discs) → `Destination` (fs + free space, `BDM prefix`, pre-flight `pass/fail/warn`) → `Queue` (reorder, `Converting → Splitting → Copying → Verifying → Done`, pause/resume) → `Activity/Settings`. First PS2 boot is manual: *Game Sources → enable USB/MX4SIO → start mode → Save → L3 for PS1*.

---

### 🔧 How it works

```
Sources (.iso / .cue+.bin)
  → library scan (hash first 1 MiB + size, dedupe)
  → detector (override → isotool PVD/UDF → 700 MiB → DB) + GameID (stream SYSTEM.CNF)
  → oplfs tree (DVD/CD/POPS/EMBER/ART/CHT/APPS…) + DISCS/VMCDIR validators
  → queue (SQLite, one writer/volume, prep-ahead) → transfer (FileDisk, Statfs, Verify)
  → RiptOPL/ART/CHT staged on demand (URL shown pre-fetch, digest pinned post-fetch)
```

All heavy lifting is **streaming** (`io.Copy` + `1–2 MiB` ring, never `ReadFile` on ISOs). Desktop shell is `//go:build desktop` — default `go build` stays `cgo`-free.

<details>
<summary><strong>Project layout</strong></summary>

```
cmd/oplbm/          CLI + serve + Wails (desktop.go)
internal/
  library/          scan/hash/detect/database/GameID + SQLite
  isotool/          ISO9660 PVD + UDF NSR02/03
  cuebin/           CUE lexer + VCD 2352-sector writer
  usbextreme/       1 GiB ul.cfg/ul.* writer
  oplfs/            tree builder + DISCS.TXT/VMCDIR.TXT
  queue/            jobs/destinations + mutex/lease + Preflight
  transfer/fsinfo     FileDisk + per-OS Probe
  api/              chi REST + SSE (/api/library, /queue, /enrich/*, /preflight)
  riptopl/art/cheats  explicit per-action net/http + image/png
  logging/config/invariants  slog file sink (10 MiB×5, ~/.oplbm/logs/)
web/                Svelte + Vite, go:embed web/dist, plain CSS vars
scripts/            build-web.sh, build.sh, gen-third-party.sh, build-flatpak.sh, e2e.sh
build/              Wails icon/plist/manifest (tracked), build/bin/ (ignored)
flatpak/            manifest + appdata + desktop + icon (io.github.JO0812.TinyPS2Manager)
```
</details>

<details>
<summary><strong>Lint / test / verify</strong></summary>

```bash
go vet ./... && go vet -tags desktop ./...
gofmt -l cmd internal testdata scripts          # must be empty
go test ./...                                   # incl. invariants N1-N6
go test -run TestEmber ./internal/queue -count=1 -v
go test -tags=genfixtures ./testdata            # regen synthetic fixtures (<100 MiB)

GOOS=windows GOARCH=amd64 go build -o dist/oplbm.exe ./cmd/oplbm  # cross matrix: linux/amd64,arm64 darwin/amd64,arm64 windows/amd64
wails build -tags desktop                       # → build/bin/oplbm (needs libwebkit2gtk-4.1-dev on Linux)
./scripts/gen-third-party.sh                    # THIRD_PARTY.md + licenses/

cd web && npm run check && npm run build        # svelte-check, 250 KiB budget
scripts/e2e.sh                                  # CLI + API + browser UI (skips without Chromium)
```
</details>

### 🛡️ Constraints (enforced in CI/tests)

* **N1** Streaming only, **N2** no `cgo`/`os/exec` except `desktop`, **N3** sources read-only, **N4** validate `DISCS.TXT`/`VMCDIR.TXT`/`ul.cfg` before enqueue (`422`), **N5** single writer, **N6** SQLite resume. `licenses/` + `build/bin/` gitignored but shipped.

### 📄 Sources of truth

* `OPL-Backup-Manager-SPEC.md` — OPL/POPStarter/RiptOPL rules `§§2.1–2.11`
* `BUILD-PLAN.md` — `M1→M5` milestones, `Q1–Q7` decisions, `N1–N6`
* `.github/workflows/ci.yml` — `vet-test`, `web`, `cross-build`, `wails`, `release` (on `v*`), `flatpak`

---

<p align="center"><i>No real game images in repo — synthetic fixtures only. ROMs/BIOS never downloaded.</i></p>
