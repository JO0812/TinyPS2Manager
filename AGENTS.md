# AGENTS.md — TinyPS2Manager

## Build
```
scripts/build-web.sh          # npm ci + vite build -> web/dist (rewrites web/dist/README.md placeholder)
scripts/build.sh              # build-web.sh + gen-third-party.sh + go build dist/oplbm + wails build -tags "desktop webkit2_41" -> build/bin/ when Wails installed
scripts/gen-third-party.sh    # regenerates THIRD_PARTY.md + licenses/ (licenses/ is gitignored, shipped in Wails bundles)
GOOS=windows GOARCH=amd64 go build -o dist/oplbm.exe ./cmd/oplbm  # pure-Go cross-build, no -tags desktop
```

* `web/dist/README.md` is a committed placeholder so `go:embed web/dist` compiles on fresh clones (Go ignores dotfiles). `vite build` empties `dist/`; `build-web.sh` rewrites it byte-identical — don't delete.
* `THIRD_PARTY.md` is committed; `licenses/` is not (CI generates it). Every Wails bundle must contain both.
* `cmd/oplbm/desktop.go` is `//go:build desktop` — isolates cgo/Wails. Default `go build ./...` stays cgo-free.

## Verify (run in order, CI does)
```
go vet ./...
go vet -tags desktop ./...                         # separate, desktop tag pulls Wails/cgo
gofmt -l cmd internal testdata scripts             # must be empty
go test ./...                                      # unit + integration + invariants (internal/invariants)
cd web && npm run check                            # svelte-check + tsc
cd web && npm run build                            # vite, budget 250 KiB uncompressed (CI warns)
```

Cross-build matrix is pure-Go (no cgo): `linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64`.

Wails bundle (needs `wails` CLI + GTK/WebKit on Linux):
```
go install github.com/wailsapp/wails/v2/cmd/wails@latest
# Linux: sudo apt install libwebkit2gtk-4.1-dev  (wails doctor otherwise fails, but CI covers it)
# If only 4.1 headers exist, Wails still asks pkg-config for webkit2gtk-4.0:
# shim it user-locally with: cp /usr/lib/x86_64-linux-gnu/pkgconfig/webkit2gtk-4.1.pc ~/.local/lib/pkgconfig/webkit2gtk-4.0.pc
# and export PKG_CONFIG_PATH="$HOME/.local/lib/pkgconfig"
wails build -tags "desktop webkit2_41"            # -> build/bin/oplbm (webkit2_41 mandatory: without it POST/PATCH bodies are dropped -> "Load failed")
```

## Tests — how to run one
```
go test -run TestName ./internal/library -count=1 -v
go test -run TestN1 ./internal/invariants -count=1 -v # N1-N6 invariants suite
go test ./internal/queue -run TestEmber -count=1 -v
go test -tags=genfixtures ./testdata               # regenerate synthetic ISO/CUE/BIN fixtures (<100 MiB)
scripts/e2e.sh                                     # CLI + API + browser UI; UI skips without node/Chromium
```

* No real game images in repo — `testdata/gensrc` emits synthetic ISO9660 + CUE/BIN at deterministic sizes.
* `preflight` and `fsinfo` tests use temp dirs; they expect `/proc/mounts` on Linux and fall back to `unknown` on other OS.
* Enrichment (art/cheats/riptopl) is explicit per-action network (show URL before fetch, record digest after). Offline still queues/boots.

## Architecture (where things live)
* `cmd/oplbm/` — CLI (`import/inspect/convert/split/tree/serve/gameid/art/cheats/riptopl/preflight`) + `serve` + Wails shell
* `internal/library` — scan/hash (SHA-256 of first 1 MiB + size), 4-step CD/DVD detector (`override/inspected/heuristic/database`), `GameID` via streaming `SYSTEM.CNF` `BOOT2`, SQLite store
* `internal/isotool` — ISO9660 PVD + UDF `NSR02/03` scan (read-only, streaming)
* `internal/cuebin` — hand-written CUE lexer + streaming `VCD` writer (2352-byte sectors, pregap `INDEX 00`, `2 MiB` buffer)
* `internal/usbextreme` — `1 GiB` chunk writer (`ul.cfg` + `ul.*` at device root, never inside `DVD/`)
* `internal/oplfs` — `DVD/CD/POPS/EMBER/ART/CHT/APPS/...` tree builder + `DISCS.TXT` (≤4×73) / `VMCDIR.TXT` (1×103) validators
* `internal/queue` — SQLite `jobs/destinations` + per-device mutex + DB lease, prep-ahead pool, `0. oplbm.*` temp + `os.Rename`, SHA-256 verify, pause/resume, `attempts` cap 3, `Preflight`
* `internal/transfer` + `internal/transfer/fsinfo` — `FileDisk` abstraction, per-OS `Probe` (`linux: /proc/mounts` + `Statfs`, `darwin: Statfs`, `windows: GetVolumeInformationW`), `Volumes()` removable-drive picker (`linux: /dev/sd|mmcblk + /media|/run/media|/mnt + by-label`, `darwin: /Volumes`, `windows: logical drives`)
* `internal/api` — `chi` REST + SSE (`/api/library`, `/api/destinations`, `/api/queue`, `/api/enrich/{art,cheats,riptopl}`, `/api/destinations/{id}/preflight`)
* `internal/{riptopl,art,cheats}` — loader/art/cheat fetchers (stdlib `net/http` + `image/png` only)
* `internal/{logging,config,invariants}` — rotating `slog` JSON sink (`~/.oplbm/logs/`, 10 MiB×5)
* `web/` — Svelte + Vite SPA, `go:embed web/dist` in `web/embed.go`, plain CSS custom properties (no Tailwind)

## Constraints — do not violate (enforced in CI/tests)
* **N1** Streaming I/O only, buffers ≤4 MiB; **N2** no cgo except `desktop` tag, no `os/exec`; **N3** sources read-only; **N4** validate `DISCS.TXT/VMCDIR.TXT/ul.cfg` before enqueue (422); **N5** one writer per destination; **N6** SQLite persistence for resume.
* `licenses/` gitignored, `build/bin/` Wails output gitignored, but both shipped in release bundles.

## Sources of truth
* `OPL-Backup-Manager-SPEC.md` — OPL/POPStarter/RiptOPL rules (§§2.1–2.11)
* `BUILD-PLAN.md` — milestones M1→M5, §1 resolved decisions (Q1–Q7), invariants N1–N6
* CI: `.github/workflows/ci.yml` (jobs `vet-test`, `web`, `cross-build`, `wails`)
