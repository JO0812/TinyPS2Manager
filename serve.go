package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jo/TinyPS2Manager/internal/api"
	"github.com/jo/TinyPS2Manager/internal/config"
	"github.com/jo/TinyPS2Manager/internal/library"
	applog "github.com/jo/TinyPS2Manager/internal/logging"
	"github.com/jo/TinyPS2Manager/internal/queue"
	"github.com/jo/TinyPS2Manager/internal/transfer"
	webui "github.com/jo/TinyPS2Manager/web"
)

// defaultBind is localhost-only. Binding 0.0.0.0 exposes the API (which has
// no auth) to the LAN: anyone there could read job state and write to your
// destinations. Use --bind 0.0.0.0 only on networks you fully trust.
const defaultBind = "127.0.0.1:41337"

// stack bundles everything opened for one run: log sink, SQLite stores,
// executor and the API server. openStack builds it; Close releases it.
type stack struct {
	db      string
	logSink *applog.Sink
	qstore  *queue.Store
	lib     *library.Store
	srv     *api.Server
}

// openStack is shared by headless serve and the desktop shell (build tag
// `desktop`): it opens logging, the SQLite stores, config and the API server
// without binding any listener. Close releases everything in reverse order.
func openStack(dbFlag, settingsFlag string) (*stack, error) {
	logDir, err := applog.DefaultDir()
	if err != nil {
		return nil, err
	}
	logSink, err := applog.Open(logDir)
	if err != nil {
		return nil, err
	}
	slog.SetDefault(logSink.Logger())
	db, err := resolveDB(dbFlag)
	if err != nil {
		logSink.Close()
		return nil, err
	}
	settings := settingsFlag
	if settings == "" {
		settings, err = config.DefaultPath()
		if err != nil {
			logSink.Close()
			return nil, err
		}
	}
	qstore, err := queue.Open(db)
	if err != nil {
		logSink.Close()
		return nil, err
	}
	lib, err := library.Open(db)
	if err != nil {
		qstore.Close()
		logSink.Close()
		return nil, err
	}
	cfg, err := config.Load(settings)
	if err != nil {
		lib.Close()
		qstore.Close()
		logSink.Close()
		return nil, err
	}
	staging := cfg.StagingDir
	if staging == "" {
		staging = os.TempDir()
	}
	exec := queue.New(qstore, lib, transfer.FileDisk{}, staging)
	srv := api.New(qstore, lib, settings, exec)
	return &stack{db: db, logSink: logSink, qstore: qstore, lib: lib, srv: srv}, nil
}

func (s *stack) Close() {
	s.lib.Close()
	s.qstore.Close()
	s.logSink.Close()
}

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
	st, err := openStack(*dbPath, *settingsPath)
	if err != nil {
		return err
	}
	defer st.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	st.srv.StartExecutor(ctx)

	host := *bind
	if host == "0.0.0.0" || host == ":41337" || len(host) > 0 && host[0] == ':' {
		fmt.Fprintln(os.Stderr, "WARNING: listening beyond localhost exposes the unauthenticated API to the network")
	}
	// /api/* hits the REST+SSE surface; everything else serves the UI.
	mux := http.NewServeMux()
	mux.Handle("/api/", st.srv.Handler())
	mux.Handle("/", webui.Handler())
	httpSrv := &http.Server{Addr: host, Handler: mux}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutdown)
	}()
	fmt.Printf("oplbm serving on http://%s (db %s)\n", host, st.db)
	slog.Info("server listening", "bind", host, "db", st.db)
	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("server stopped with error", "error", err)
		return err
	}
	slog.Info("server stopped")
	return nil
}
