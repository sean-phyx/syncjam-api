package application

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"sync"
	"time"

	"github.com/sean-phyx/syncjam-api/internal/domain"
)

// StatePatch is a partial session-state mutation. Nil fields are
// left untouched.
type StatePatch struct {
	TrackID    *string
	PositionMs *int64
	Playing    *bool
}

type client struct {
	userID    string
	identity  *domain.AuthedIdentity
	sessionID string
}

// Broker holds in-memory clients and sessions and fans state changes
// out through the Notifier. Public methods are safe for concurrent use.
type Broker struct {
	mu        sync.Mutex
	clients   map[string]*client        // userID → client
	sessions  map[string]*domain.Session // sessionID → session
	codeIndex map[string]string          // upper(inviteCode) → sessionID
	notifier  Notifier
	now       func() time.Time // overridable for tests
}

func NewBroker(notifier Notifier) *Broker {
	return &Broker{
		clients:   make(map[string]*client),
		sessions:  make(map[string]*domain.Session),
		codeIndex: make(map[string]string),
		notifier:  notifier,
		now:       time.Now,
	}
}

// Register adds an authenticated client and returns its userID.
func (b *Broker) Register(identity *domain.AuthedIdentity) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	uid := newID()
	b.clients[uid] = &client{userID: uid, identity: identity}
	return uid
}

// Unregister drops the client and leaves any session it was in.
func (b *Broker) Unregister(userID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	c, ok := b.clients[userID]
	if !ok {
		return
	}
	if c.sessionID != "" {
		b.leaveLocked(userID)
	}
	delete(b.clients, userID)
}

// Identity returns nil if userID isn't registered.
func (b *Broker) Identity(userID string) *domain.AuthedIdentity {
	b.mu.Lock()
	defer b.mu.Unlock()
	c, ok := b.clients[userID]
	if !ok {
		return nil
	}
	return c.identity
}

// CreateSession implicitly leaves any existing session first.
func (b *Broker) CreateSession(userID string) (*domain.Session, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	c, ok := b.clients[userID]
	if !ok {
		return nil, domain.ErrClientNotFound
	}
	if c.sessionID != "" {
		b.leaveLocked(userID)
	}

	session := &domain.Session{
		ID:         newID(),
		InviteCode: b.freshInviteCodeLocked(),
		State: domain.SessionState{
			AnchorServerTimeMs: b.now().UnixMilli(),
		},
		Members: map[string]struct{}{userID: {}},
	}
	b.sessions[session.ID] = session
	b.codeIndex[session.InviteCode] = session.ID
	c.sessionID = session.ID
	return session, nil
}

// JoinSession broadcasts member_joined on success.
func (b *Broker) JoinSession(userID, inviteCode string) (*domain.Session, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	c, ok := b.clients[userID]
	if !ok {
		return nil, domain.ErrClientNotFound
	}
	sid, ok := b.codeIndex[normalizeCode(inviteCode)]
	if !ok {
		return nil, domain.ErrSessionNotFound
	}
	session, ok := b.sessions[sid]
	if !ok {
		return nil, domain.ErrSessionNotFound
	}
	if c.sessionID != "" && c.sessionID != sid {
		b.leaveLocked(userID)
	}
	session.Members[userID] = struct{}{}
	c.sessionID = sid

	for mid := range session.Members {
		if mid == userID {
			continue
		}
		_ = b.notifier.Notify(mid, map[string]any{
			"type":        "member_joined",
			"userId":      userID,
			"displayName": c.identity.DisplayName,
		})
	}
	return session, nil
}

// LeaveSession is a no-op when userID isn't in a session.
func (b *Broker) LeaveSession(userID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.leaveLocked(userID)
}

// UpdateState applies the patch, re-anchors the clock, and broadcasts
// state_changed to every member (issuer included).
func (b *Broker) UpdateState(userID string, patch StatePatch) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	c, ok := b.clients[userID]
	if !ok || c.sessionID == "" {
		return domain.ErrNotInSession
	}
	session, ok := b.sessions[c.sessionID]
	if !ok {
		return domain.ErrNotInSession
	}

	if patch.TrackID != nil {
		session.State.TrackID = *patch.TrackID
	}
	if patch.PositionMs != nil {
		session.State.PositionMs = *patch.PositionMs
	}
	if patch.Playing != nil {
		session.State.Playing = *patch.Playing
	}
	session.State.AnchorServerTimeMs = b.now().UnixMilli()

	for mid := range session.Members {
		_ = b.notifier.Notify(mid, map[string]any{
			"type":     "state_changed",
			"issuedBy": userID,
			"state": map[string]any{
				"trackId":            session.State.TrackID,
				"positionMs":         session.State.PositionMs,
				"playing":            session.State.Playing,
				"anchorServerTimeMs": session.State.AnchorServerTimeMs,
			},
		})
	}
	return nil
}

// StateFor returns a copy; callers may not mutate broker state.
func (b *Broker) StateFor(userID string) *domain.SessionState {
	b.mu.Lock()
	defer b.mu.Unlock()
	c, ok := b.clients[userID]
	if !ok || c.sessionID == "" {
		return nil
	}
	session, ok := b.sessions[c.sessionID]
	if !ok {
		return nil
	}
	state := session.State
	return &state
}

// MemberList returns nil if userID isn't in a session.
func (b *Broker) MemberList(userID string) []domain.Member {
	b.mu.Lock()
	defer b.mu.Unlock()
	c, ok := b.clients[userID]
	if !ok || c.sessionID == "" {
		return nil
	}
	session, ok := b.sessions[c.sessionID]
	if !ok {
		return nil
	}
	members := make([]domain.Member, 0, len(session.Members))
	for mid := range session.Members {
		mc, ok := b.clients[mid]
		if !ok {
			continue
		}
		members = append(members, domain.Member{
			UserID:      mid,
			DisplayName: mc.identity.DisplayName,
		})
	}
	return members
}

// --- internal ---

func (b *Broker) leaveLocked(userID string) {
	c, ok := b.clients[userID]
	if !ok || c.sessionID == "" {
		return
	}
	sid := c.sessionID
	session, ok := b.sessions[sid]
	c.sessionID = ""
	if !ok {
		return
	}
	delete(session.Members, userID)

	for mid := range session.Members {
		_ = b.notifier.Notify(mid, map[string]any{
			"type":   "member_left",
			"userId": userID,
		})
	}
	if len(session.Members) == 0 {
		delete(b.sessions, sid)
		delete(b.codeIndex, session.InviteCode)
	}
}

// 32 chars, no visually ambiguous ones (0/O, 1/I).
const inviteAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

func (b *Broker) freshInviteCodeLocked() string {
	for attempt := 0; attempt < 8; attempt++ {
		buf := make([]byte, 6)
		_, _ = rand.Read(buf)
		code := make([]byte, 6)
		for i := range buf {
			code[i] = inviteAlphabet[int(buf[i])%len(inviteAlphabet)]
		}
		s := string(code)
		if _, exists := b.codeIndex[s]; !exists {
			return s
		}
	}
	// Fallback if the keyspace is saturated.
	return strings.ToUpper(newID()[:6])
}

func newID() string {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

func normalizeCode(c string) string {
	return strings.ToUpper(strings.TrimSpace(c))
}
