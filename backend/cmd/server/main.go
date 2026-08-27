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

	"github.com/scottfridlund/contacts/backend/internal/config"
	"github.com/scottfridlund/contacts/backend/internal/db"
	"github.com/scottfridlund/contacts/backend/internal/httpapi"
	"github.com/scottfridlund/contacts/backend/internal/logging"
	"github.com/scottfridlund/contacts/backend/internal/person"
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

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:"+port+"/healthz", nil)
	if err != nil {
		return 1
	}
	resp, err := http.DefaultClient.Do(req)
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
	svc := person.NewService(repo)
	relRepo := person.NewRelationshipRepository(pool)
	relSvc := person.NewRelationshipService(relRepo, repo)

	purgeWindow := time.Duration(cfg.PurgeAfterDays) * 24 * time.Hour
	go runPurgeLoop(ctx, logger, svc, relSvc, purgeWindow)

	handler := httpapi.NewRouter(logger, svc, relSvc, repo, cfg.CORSAllowedOrigins)
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

// runPurgeLoop periodically purges soft-deleted records past the retention
// window (recycle bin behavior).
func runPurgeLoop(ctx context.Context, logger *slog.Logger, svc *person.Service, relSvc *person.RelationshipService, window time.Duration) {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	purge := func() {
		count, err := svc.PurgeExpired(ctx, window)
		if err != nil {
			logger.Error("purge persons failed", "error", err)
		} else if count > 0 {
			logger.Info("purged expired persons", "count", count)
		}

		relCount, err := relSvc.PurgeExpired(ctx, window)
		if err != nil {
			logger.Error("purge relationships failed", "error", err)
		} else if relCount > 0 {
			logger.Info("purged expired relationships", "count", relCount)
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
