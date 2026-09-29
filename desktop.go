//go:build desktop

package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/jo/TinyPS2Manager/web"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App is the Wails binding surface for the SPA (window.go.main.App).
// The same web build also runs in plain browsers via `oplbm serve`, where
// window.go is absent — the frontend guards for that and keeps the typed
// path field as the fallback.
type App struct {
	ctx context.Context
}

// SelectFolder opens the OS-native directory chooser. Empty string means
// the user cancelled.
func (a *App) SelectFolder(title string) (string, error) {
	if title == "" {
		title = "Choose folder"
	}
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: title})
}

// runDesktop hosts the REST+SSE API and the embedded SPA inside a native
// Wails window: /api/* falls through to the API handler, everything else to
// the SPA handler (index fallback included). No TCP listener is bound — the
// SPA talks to the API same-origin through the Wails asset server.
func runDesktop(st *stack) error {
	ui, ok := web.FS()
	if !ok {
		return fmt.Errorf("UI not built: run scripts/build-web.sh, then rebuild")
	}
	apiHandler := st.srv.Handler()
	uiHandler := web.Handler()
	app := &App{}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			apiHandler.ServeHTTP(w, r)
			return
		}
		uiHandler.ServeHTTP(w, r)
	})
	return wails.Run(&options.App{
		Title:  "OPL Backup Manager",
		Width:  1080,
		Height: 760,
		Bind: []interface{}{
			app,
		},
		OnStartup: func(ctx context.Context) {
			app.ctx = ctx
		},
		AssetServer: &assetserver.Options{
			Assets:  ui,
			Handler: handler,
		},
	})
}

// runNoArgs opens the desktop window; Wails launches the binary with no
// arguments on double-click. `oplbm serve` keeps working headless in the
// same binary.
func runNoArgs() int {
	st, err := openStack("", "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	defer st.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	st.srv.StartExecutor(ctx)
	if err := runDesktop(st); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	return 0
}
