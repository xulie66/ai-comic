package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"comic.local/go-service/internal/auth"
	"comic.local/go-service/internal/config"
	"comic.local/go-service/internal/generation"
	"comic.local/go-service/internal/httpapi"
	"comic.local/go-service/internal/operations"
	"comic.local/go-service/internal/store"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Service failed; inspect the private server log.")
		os.Exit(1)
	}
}
func run() error {
	bootstrap := flag.Bool("bootstrap-admin", false, "create administrator using GO_SERVICE_ADMIN_USERNAME and GO_SERVICE_ADMIN_PASSWORD, then exit")
	flag.Parse()
	c, err := config.Load()
	if err != nil {
		return err
	}
	logDir := filepath.Join(c.DataRoot, "logs")
	if err = os.MkdirAll(logDir, 0700); err != nil {
		return err
	}
	logFile, err := os.OpenFile(filepath.Join(logDir, "server.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer logFile.Close()
	logger := slog.New(slog.NewJSONHandler(logFile, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)
	defer func() {
		if err != nil {
			logger.Error("service stopped with error", "error", err)
		}
	}()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	initCtx, initCancel := context.WithTimeout(ctx, 30*time.Second)
	defer initCancel()
	s, err := store.Open(initCtx, store.Options{DataRoot: c.DataRoot, DatabaseURL: c.DatabaseURL})
	if err != nil {
		logger.Error("store initialization failed", "error", err)
		return err
	}
	defer s.Close()
	a := auth.New(s, auth.Options{CookieName: c.CookieName, SecureCookies: c.SecureCookies, SessionTTL: c.SessionTTL})
	if *bootstrap {
		u, e := a.BootstrapAdmin(initCtx, os.Getenv("GO_SERVICE_ADMIN_USERNAME"), os.Getenv("GO_SERVICE_ADMIN_PASSWORD"))
		if e != nil {
			logger.Error("administrator bootstrap failed", "error", e)
			return e
		}
		logger.Info("administrator created", "user_id", u.ID)
		fmt.Println("Administrator created.")
		return nil
	}
	ops := operations.New(s.Pool(), logger)
	ops.DataRoot = c.DataRoot
	if err = ops.Init(initCtx); err != nil {
		logger.Error("operations initialization failed", "error", err)
		return err
	}
	api := &httpapi.Server{Store: s, Auth: a, Ops: ops, Client: &generation.Client{Logger: logger}, Logger: logger, PublicDir: c.PublicDir, MaxBody: c.MaxBodyBytes}
	if err = api.InitJobs(filepath.Join(c.DataRoot, "queue")); err != nil {
		logger.Error("job initialization failed", "error", err)
		return err
	}
	defer api.Jobs.Close()
	ops.QueueSnapshot = func(ctx context.Context) (map[string]any, error) {
		return map[string]any{"items": api.Jobs.Snapshot()}, nil
	}
	ops.RenderJobs = func(ctx context.Context, query map[string]string) (map[string]any, error) {
		return map[string]any{"items": api.Jobs.Snapshot()}, nil
	}
	handler := recoverRequests(logger, api)
	srv := &http.Server{Addr: c.Addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 1 << 20, ErrorLog: slog.NewLogLogger(logger.Handler(), slog.LevelError)}
	done := make(chan error, 1)
	go func() {
		logger.Info("service listening", "addr", c.Addr, "postgres", s.Pool() != nil)
		done <- srv.ListenAndServe()
	}()
	select {
	case err = <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
	}
	shutdownCtx, stop := context.WithTimeout(context.Background(), 30*time.Second)
	defer stop()
	err = srv.Shutdown(shutdownCtx)
	if err != nil {
		_ = srv.Close()
		return err
	}
	logger.Info("service stopped")
	return nil
}
func recoverRequests(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if p := recover(); p != nil {
				logger.Error("request panic", "method", r.Method, "path", r.URL.Path, "panic", fmt.Sprint(p))
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(500)
				_, _ = w.Write([]byte(`{"success":false,"error":"服务暂时不可用"}`))
			}
		}()
		next.ServeHTTP(w, r)
	})
}
