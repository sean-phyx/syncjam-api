package domain

import "errors"

// Sentinel errors. Delivery layers translate these into protocol
// error codes (see PROTOCOL.md).
var (
	ErrAuthFailed         = errors.New("auth_failed")
	ErrBackendUnreachable = errors.New("backend_unreachable")
	ErrSessionNotFound    = errors.New("session_not_found")
	ErrNotInSession       = errors.New("not_in_session")
	ErrClientNotFound     = errors.New("client_not_found")
	ErrNotAuthorised      = errors.New("not_authorised")
)
