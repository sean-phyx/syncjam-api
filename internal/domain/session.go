package domain

// SessionState is the canonical playback state. Every mutation
// re-anchors AnchorServerTimeMs.
type SessionState struct {
	TrackID            string
	PositionMs         int64
	Playing            bool
	AnchorServerTimeMs int64
}

// Member is the public projection of a session participant. UserID is
// a broker-local opaque id, not the backend user id.
type Member struct {
	UserID      string
	DisplayName string
}

type Session struct {
	ID         string
	InviteCode string
	State      SessionState
	Members    map[string]struct{}
}
