package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jo/TinyPS2Manager/internal/api"
	"github.com/jo/TinyPS2Manager/internal/config"
	"github.com/jo/TinyPS2Manager/internal/library"
	"github.com/jo/TinyPS2Manager/internal/queue"
	"github.com/jo/TinyPS2Manager/internal/transfer"
	webui "github.com/jo/TinyPS2Manager/web"
)

// defaultBind is localhost-only. Binding 0.0.0.0 exposes the API (which has
// no auth) to the LAN: anyone there could read job state and write to your
// destinations. Use --bind 0.0.0.0 only on networks you fully trust.
const defaultBind = "127.0.0.1:41337"

func cmdServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	bind := fs.String("bind", defaultBind, "listen address (default localhost-only)")
	dbPath := fs.String("db", "", "SQLite path (default $CONFIG/oplbm/oplbm.db)")
	settingsPath := fs.String("settings", "", "settings JSON path (default $CONFIG/oplbm/settings.json)")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: oplbm serve [--bind addr] [--db path] [--settings path]\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		fs.Usage()
		return fmt.Errorf("serve takes no positional args")
	}
	db, err := resolveDB(*dbPath)
	if err != nil {
		return err
	}
	settings := *settingsPath
	if settings == "" {
		settings, err = config.DefaultPath()
		if err != nil {
			return err
		}
	}
	qstore, err := queue.Open(db)
	if err != nil {
		return err
	}
	defer qstore.Close()
	lib, err := library.Open(db)
	if err != nil {
		return err
	}
	defer lib.Close()
	cfg, err := config.Load(settings)
	if err != nil {
		return err
	}
	staging := cfg.StagingDir
	if staging == "" {
		staging = os.TempDir()
	}
	exec := queue.New(qstore, lib, transfer.FileDisk{}, staging)
	srv := api.New(qstore, lib, settings, exec)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	srv.StartExecutor(ctx)

	host := *bind
	if host == "0.0.0.0" || host == ":41337" || len(host) > 0 && host[0] == ':' {
		fmt.Fprintln(os.Stderr, "WARNING: listening beyond localhost exposes the unauthenticated API to the network")
	}
	// /api/* hits the REST+SSE surface; everything else serves the UI.
	mux := http.NewServeMux()
	mux.Handle("/api/", srv.Handler())
	mux.Handle("/", webui.Handler())
	httpSrv := &http.Server{Addr: host, Handler: mux}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutdown)
	}()
	fmt.Printf("oplbm serving on http://%s (db %s)\n", host, db)
	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}
