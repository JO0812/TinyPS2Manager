# Build output of `scripts/build-web.sh` (Svelte/Vite → static files served
# embedded by the Go binary). This placeholder keeps `go:embed web/dist`
# compiling on fresh clones (Go ignores dotfiles, so .gitkeep cannot work);
# build-web.sh rewrites it after every build since Vite empties this dir.
