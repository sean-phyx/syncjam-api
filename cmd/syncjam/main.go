// Command syncjam is the SyncJam sync-broker server entry point.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/sean-phyx/syncjam-api/internal/application"
	"github.com/sean-phyx/syncjam-api/internal/delivery/websocket"
	"github.com/sean-phyx/syncjam-api/internal/infrastructure/subsonic"
)

func main() {
	addr := env("PORT", "8787")
	allowedOrigins := splitNonEmpty(env("ALLOWED_ORIGINS", ""))
	subsonicURL := strings.TrimRight(env("SUBSONIC_URL", ""), "/")
	if subsonicURL == "" {
		log.Fatal("[syncjam] SUBSONIC_URL must be set (e.g. http://navidrome:4533)")
	}

	verifier := subsonic.NewVerifier()
	registry := websocket.NewRegistry()
	broker := application.NewBroker(registry)
	server := websocket.NewServer(broker, registry, verifier)
	server.SubsonicURL = subsonicURL
	server.AllowedOrigins = allowedOrigins

	mux := http.NewServeMux()
	server.Mount(mux)

	httpSrv := &http.Server{
		Addr:              ":" + addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("[syncjam] listening on :%s (ws /ws, health /healthz) subsonic=%s", addr, subsonicURL)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("[syncjam] server error: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("[syncjam] shutdown signal received")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		log.Printf("[syncjam] graceful shutdown error: %v", err)
	}
}

func env(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func splitNonEmpty(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := parts[:0]
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
