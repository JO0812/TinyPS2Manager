// Package main is the entrypoint for the oplbm Wails application.
//
// It embeds the compiled frontend (web/dist) via go:embed and serves the
// REST + SSE API surface on localhost. The native window/tray shell is
// provided by Wails (spec §10 Q4 — wrapped desktop app).
package main

func main() {
	// Phase 0: stub entrypoint. Wails wiring lands in M2 alongside the API.
}