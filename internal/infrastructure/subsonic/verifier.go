// Package subsonic implements application.SubsonicVerifier against a
// live ping.view call. No credentials are retained.
package subsonic

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/sean-phyx/syncjam-api/internal/application"
	"github.com/sean-phyx/syncjam-api/internal/domain"
)

type Verifier struct {
	client *http.Client
}

func NewVerifier() *Verifier {
	return &Verifier{
		client: &http.Client{Timeout: 5 * time.Second},
	}
}

func (v *Verifier) Verify(ctx context.Context, serverURL string, creds application.SubsonicCreds) (*domain.AuthedIdentity, error) {
	base := strings.TrimRight(serverURL, "/")
	q := url.Values{}
	q.Set("f", "json")
	q.Set("c", "syncjam")
	q.Set("v", "1.16.1")

	var display string
	switch {
	case creds.APIKey != "":
		q.Set("apiKey", creds.APIKey)
		// apiKey ping response doesn't carry a username; use a prefix
		// as a pseudonym.
		display = "apikey:" + truncate(creds.APIKey, 8)
	case creds.Username != "" && creds.Token != "" && creds.Salt != "":
		q.Set("u", creds.Username)
		q.Set("t", creds.Token)
		q.Set("s", creds.Salt)
		display = creds.Username
	default:
		return nil, fmt.Errorf("%w: need username+token+salt or apiKey", domain.ErrAuthFailed)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s/rest/ping.view?%s", base, q.Encode()), nil)
	if err != nil {
		return nil, fmt.Errorf("%w: building request: %v", domain.ErrBackendUnreachable, err)
	}
	res, err := v.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", domain.ErrBackendUnreachable, err)
	}
	defer res.Body.Close()
	if res.StatusCode >= 500 {
		return nil, fmt.Errorf("%w: HTTP %d", domain.ErrBackendUnreachable, res.StatusCode)
	}

	var body struct {
		SubsonicResponse struct {
			Status string `json:"status"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		} `json:"subsonic-response"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("%w: decode response: %v", domain.ErrBackendUnreachable, err)
	}
	if body.SubsonicResponse.Status != "ok" {
		msg := "credentials rejected"
		if body.SubsonicResponse.Error != nil && body.SubsonicResponse.Error.Message != "" {
			msg = body.SubsonicResponse.Error.Message
		}
		return nil, fmt.Errorf("%w: %s", domain.ErrAuthFailed, msg)
	}

	return &domain.AuthedIdentity{
		IdentityKey: display,
		DisplayName: display,
	}, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
