package websocket

import (
	"encoding/json"
	"errors"
	"sync"
)

// Registry maps userID → outbound channel. Implements
// application.Notifier. Marshals each broadcast once regardless of
// fan-out size.
type Registry struct {
	mu      sync.RWMutex
	clients map[string]chan<- []byte
}

func NewRegistry() *Registry {
	return &Registry{clients: make(map[string]chan<- []byte)}
}

// Register overwrites any prior entry for userID.
func (r *Registry) Register(userID string, out chan<- []byte) {
	r.mu.Lock()
	r.clients[userID] = out
	r.mu.Unlock()
}

// Unregister is idempotent.
func (r *Registry) Unregister(userID string) {
	r.mu.Lock()
	delete(r.clients, userID)
	r.mu.Unlock()
}

// Notify enqueues msg on userID's outbound channel. Returns
// outbound_full if the client can't keep up; the message is dropped.
func (r *Registry) Notify(userID string, msg any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	r.mu.RLock()
	out, ok := r.clients[userID]
	r.mu.RUnlock()
	if !ok {
		return errors.New("client_not_registered")
	}
	select {
	case out <- data:
		return nil
	default:
		return errors.New("outbound_full")
	}
}
