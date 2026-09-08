// Command server starts the Contacts HTTP API.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/scottfridlund/contacts/backend/internal/auth"
	"github.com/scottfridlund/contacts/backend/internal/config"
	"github.com/scottfridlund/contacts/backend/internal/contactsync"
	googlesync "github.com/scottfridlund/contacts/backend/internal/contactsync/google"
	"github.com/scottfridlund/contacts/backend/internal/db"
	"github.com/scottfridlund/contacts/backend/internal/httpapi"
	"github.com/scottfridlund/contacts/backend/internal/logging"
	"github.com/scottfridlund/contacts/backend/internal/person"
	"github.com/scottfridlund/contacts/backend/internal/user"
)

func main() {
	healthcheck := flag.Bool("healthcheck", false, "probe the local health endpoint and exit")
	flag.Parse()

	if *healthcheck {
		os.Exit(runHealthcheck())
	}

	if err := run(); err != nil {
		// Logger may not be available yet; write plainly to stderr.
		os.Stderr.WriteString("fatal: " + err.Error() + "\n")
		os.Exit(1)
	}
}

// runHealthcheck performs an in-process HTTP probe used by the container
// HEALTHCHECK instruction (distroless images have no shell or curl).
func runHealthcheck() int {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	//nolint:gosec // local loopback probe for container healthcheck only
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:"+port+"/healthz", nil)
	if err != nil {
		return 1
	}
	resp, err := http.DefaultClient.Do(req) //nolint:gosec // local loopback probe for container healthcheck only
	if err != nil {
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger := logging.New(cfg.LogLevel, cfg.IsProduction())
	logger.Info("starting contacts server", "env", cfg.Env, "port", cfg.Port)

	if err := db.Migrate(cfg.DatabaseURL()); err != nil {
		return err
	}
	logger.Info("migrations applied")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := db.Connect(ctx, cfg.DatabaseURL())
	if err != nil {
		return err
	}
	defer pool.Close()

	repo := person.NewRepository(pool)
	syncAccountRepo := contactsync.NewRepository(pool)
	syncJobRepo := contactsync.NewJobRepository(pool)
	syncSvc := contactsync.NewService(syncJobRepo)
	svc := person.NewService(repo, syncSvc)
	accountRepo := user.NewRepository(pool)

	processor := contactsync.Processor(contactsync.NoopProcessor{})
	var googleAdapter contactsync.Adapter
	if cfg.GoogleClientID != "" && cfg.GoogleClientSecret != "" && cfg.GoogleRedirectURL != "" {
		adapter := googlesync.NewAdapter(googlesync.Config{
			ClientID:     cfg.GoogleClientID,
			ClientSecret: cfg.GoogleClientSecret,
			RedirectURL:  cfg.GoogleRedirectURL,
		}, syncAccountRepo, svc, nil)
		registry := contactsync.AdapterRegistry{}
		registry.Register(adapter)
		processor = contactsync.NewDispatchProcessor(registry)
		googleAdapter = adapter
		logger.Info("google sync adapter configured")
	}
	runner := contactsync.NewRunner(syncAccountRepo, syncJobRepo, processor)

	purgeWindow := time.Duration(cfg.PurgeAfterDays) * 24 * time.Hour
	go runPurgeLoop(ctx, logger, svc, purgeWindow)
	go runSyncLoop(ctx, logger, runner, 30*time.Second)

	var provider *auth.Provider
	if cfg.AuthentikIssuer != "" {
		provider, err = auth.New(ctx, auth.Config{
			Issuer:       cfg.AuthentikIssuer,
			ClientID:     cfg.AuthentikClientID,
			ClientSecret: cfg.AuthentikSecret,
			RedirectURL:  cfg.AuthentikRedirect,
			SessionKey:   cfg.SessionSecret,
		}, accountRepo)
		if err != nil {
			return err
		}
	}
	handler := httpapi.NewRouter(logger, svc, repo, syncAccountRepo, googleAdapter, cfg.CORSAllowedOrigins, provider)
	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("http server listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	select {
	case err := <-serverErr:
		return err
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	logger.Info("server stopped cleanly")
	return nil
}

func runSyncLoop(ctx context.Context, logger *slog.Logger, runner *contactsync.Runner, interval time.Duration) {
	if runner == nil {
		return
	}
	if err := runner.RunLoop(ctx, interval); err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("sync loop failed", "error", err)
	}
}

// runPurgeLoop periodically purges soft-deleted records past the retention
// window (recycle bin behavior).
func runPurgeLoop(ctx context.Context, logger *slog.Logger, svc *person.Service, window time.Duration) {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	purge := func() {
		count, err := svc.PurgeExpired(ctx, window)
		if err != nil {
			logger.Error("purge failed", "error", err)
			return
		}
		if count > 0 {
			logger.Info("purged expired persons", "count", count)
		}
	}

	purge() // run once at startup
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			purge()
		}
	}
}
