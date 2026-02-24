package health

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

type SnapshotFunc func() map[string]any
type ReadyFunc func() bool

type Server struct {
	addr     string
	logger   *slog.Logger
	snapshot SnapshotFunc
	ready    ReadyFunc
}

func NewServer(host string, port int, snapshot SnapshotFunc, ready ReadyFunc, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{
		addr:     fmt.Sprintf("%s:%d", host, port),
		logger:   logger,
		snapshot: snapshot,
		ready:    ready,
	}
}

func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/ready", s.handleReady)
	mux.HandleFunc("/status", s.handleStatus)

	server := &http.Server{
		Addr:              s.addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	s.logger.Info("health server started", "addr", s.addr)
	err := server.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("health server listen failed: %w", err)
	}

	s.logger.Info("health server stopped", "addr", s.addr)
	return nil
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
	})
}

func (s *Server) handleReady(w http.ResponseWriter, _ *http.Request) {
	if s.ready == nil || s.ready() {
		writeJSON(w, http.StatusOK, map[string]any{
			"status": "ready",
		})
		return
	}

	writeJSON(w, http.StatusServiceUnavailable, map[string]any{
		"status": "not_ready",
	})
}

func (s *Server) handleStatus(w http.ResponseWriter, _ *http.Request) {
	payload := map[string]any{
		"status": "ok",
	}
	if s.snapshot != nil {
		payload["snapshot"] = s.snapshot()
	}
	writeJSON(w, http.StatusOK, payload)
}

func writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}
