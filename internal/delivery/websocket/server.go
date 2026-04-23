package websocket

import (
	"log"
	"net/http"

	"github.com/coder/websocket"

	"github.com/sean-phyx/syncjam-api/internal/application"
)

// Server exposes the SyncJam WebSocket and health endpoints over HTTP.
type Server struct {
	broker   *application.Broker
	registry *Registry
	subsonic application.SubsonicVerifier

	// SubsonicURL is set at deploy time; clients never supply their own.
	SubsonicURL string

	// AllowedOrigins gates WS upgrades. Empty disables origin checking.
	AllowedOrigins []string
}

func NewServer(
	broker *application.Broker,
	registry *Registry,
	subsonic application.SubsonicVerifier,
) *Server {
	return &Server{
		broker:   broker,
		registry: registry,
		subsonic: subsonic,
	}
}

// Mount registers /ws and /healthz on the given mux.
func (s *Server) Mount(mux *http.ServeMux) {
	mux.HandleFunc("/ws", s.handleWS)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	opts := &websocket.AcceptOptions{
		OriginPatterns: s.AllowedOrigins,
	}
	if len(s.AllowedOrigins) == 0 {
		opts.InsecureSkipVerify = true
	}
	conn, err := websocket.Accept(w, r, opts)
	if err != nil {
		log.Printf("[ws] accept failed: %v", err)
		return
	}
	c := &client{
		conn:        conn,
		broker:      s.broker,
		registry:    s.registry,
		subsonic:    s.subsonic,
		subsonicURL: s.SubsonicURL,
	}
	c.run(r.Context())
}
