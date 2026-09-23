//go:build tools

// Package tools pins Phase 0 dependencies that no non-test code imports yet.
// The tools build tag keeps these out of normal builds; `go mod tidy`
// respects the imports and retains the pins in go.mod.
package tools

import (
	_ "github.com/go-chi/chi/v5"
	_ "golang.org/x/sys/unix"
	_ "modernc.org/sqlite"
)
