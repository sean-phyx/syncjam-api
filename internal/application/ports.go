package application

import (
	"context"

	"github.com/sean-phyx/syncjam-api/internal/domain"
)

// SubsonicCreds carries either password-derived auth
// (Username + Token + Salt) or Navidrome's API key form. The verifier
// dispatches on which is populated.
type SubsonicCreds struct {
	Username string
	Token    string
	Salt     string
	APIKey   string
}

// SubsonicVerifier is the application port implemented by
// infrastructure adapters.
type SubsonicVerifier interface {
	Verify(ctx context.Context, serverURL string, creds SubsonicCreds) (*domain.AuthedIdentity, error)
}

// Notifier delivers outbound messages to a connected client. Returns
// an error when the message couldn't be enqueued.
type Notifier interface {
	Notify(userID string, msg any) error
}
