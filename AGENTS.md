# AGENTS.md

Common commands for this repository. Run these before considering work done.

## Build (web + Go)

```
scripts/build-web.sh   # installs web deps, runs vite build -> web/dist
scripts/build.sh       # builds the Wails app for the host OS
```

## Lint / vet / typecheck

```
go vet ./...
go test ./...
cd web && npm run check   # svelte-check (once scaffolded)
```

## Cross-build matrix (CI)

```
GOOS=windows GOARCH=amd64 go build -o dist/oplbm.exe ./cmd/oplbm
GOOS=linux   GOARCH=amd64 go build -o dist/oplbm-linux-amd64 ./cmd/oplbm
GOOS=linux   GOARCH=arm64 go build -o dist/oplbm-linux-arm64 ./cmd/oplbm
GOOS=darwin  GOARCH=amd64 go build -o dist/oplbm-darwin-amd64 ./cmd/oplbm
GOOS=darwin  GOARCH=arm64 go build -o dist/oplbm-darwin-arm64 ./cmd/oplbm
```

Wails builds (once wired): `wails build -platform windows/amd64` etc.

## Tests

```
go test ./...                       # unit + integration
go test -tags=genfixtures ./testdata # regenerate synthetic fixtures
scripts/e2e.sh                      # full bootup: CLI + API + browser UI flows
                                    # (UI flows skip gracefully without node/Chromium)
```

## Key constraints (do not violate)

- Pure Go only; no cgo, no shelling to external binaries.
- Streaming I/O; never load a full ISO/BIN into memory.
- Source files are read-only; never mutate.
- One destination writer per destination volume at any time.
- Validate DISCS.TXT / VMCDIR.TXT / USBExtreme output before enqueue.

Sources of truth:
- `OPL-Backup-Manager-SPEC.md` — requirements
- `BUILD-PLAN.md` — implementation plan + resolved decisions (§1)