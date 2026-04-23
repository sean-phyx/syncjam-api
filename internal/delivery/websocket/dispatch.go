package websocket

import (
	"context"
	"encoding/json"
	"time"

	"github.com/sean-phyx/syncjam-api/internal/application"
)

// inbound is the flat union of client→server fields; Type selects
// which fields carry meaning. Shape matches PROTOCOL.md.
type inbound struct {
	Type             string `json:"type"`
	InviteCode       string `json:"inviteCode,omitempty"`
	TrackID          string `json:"trackId,omitempty"`
	PositionMs       int64  `json:"positionMs,omitempty"`
	ClientSendTimeMs int64  `json:"clientSendTimeMs,omitempty"`
	Enabled          bool   `json:"enabled,omitempty"`
}

// handle dispatches one authenticated inbound message to the broker.
func (c *client) handle(ctx context.Context, raw []byte) {
	var msg inbound
	if err := json.Unmarshal(raw, &msg); err != nil {
		c.sendError("invalid_message", "could not parse JSON")
		return
	}

	switch msg.Type {
	case "hello":
		c.sendError("invalid_message", "already authenticated")

	case "create_session":
		session, err := c.broker.CreateSession(c.userID)
		if err != nil {
			c.sendError(mapErrorCode(err), err.Error())
			return
		}
		c.enqueueSessionEnvelope("session_created", session.ID, session.InviteCode)

	case "join_session":
		session, err := c.broker.JoinSession(c.userID, msg.InviteCode)
		if err != nil {
			c.sendError(mapErrorCode(err), err.Error())
			return
		}
		c.enqueueSessionEnvelope("session_joined", session.ID, session.InviteCode)

	case "leave_session":
		c.broker.LeaveSession(c.userID)

	case "set_host_only":
		if err := c.broker.SetHostOnly(c.userID, msg.Enabled); err != nil {
			c.sendError(mapErrorCode(err), err.Error())
		}

	case "play":
		playing := true
		c.applyPatch(application.StatePatch{Playing: &playing, PositionMs: &msg.PositionMs})

	case "pause":
		playing := false
		c.applyPatch(application.StatePatch{Playing: &playing, PositionMs: &msg.PositionMs})

	case "seek":
		c.applyPatch(application.StatePatch{PositionMs: &msg.PositionMs})

	case "track_change":
		c.applyPatch(application.StatePatch{
			TrackID:    &msg.TrackID,
			PositionMs: &msg.PositionMs,
		})

	case "clock_ping":
		// Any work between these timestamps biases the client's
		// offset estimate. Keep them adjacent.
		recv := time.Now().UnixMilli()
		send := time.Now().UnixMilli()
		_ = c.registry.Notify(c.userID, map[string]any{
			"type":             "clock_pong",
			"clientSendTimeMs": msg.ClientSendTimeMs,
			"serverRecvTimeMs": recv,
			"serverSendTimeMs": send,
		})

	default:
		c.sendError("invalid_message", "unknown message type")
	}
}

func (c *client) applyPatch(patch application.StatePatch) {
	if err := c.broker.UpdateState(c.userID, patch); err != nil {
		c.sendError(mapErrorCode(err), err.Error())
	}
}

func (c *client) enqueueSessionEnvelope(kind, sessionID, inviteCode string) {
	members := c.broker.MemberList(c.userID)
	wireMembers := make([]map[string]any, 0, len(members))
	for _, m := range members {
		wireMembers = append(wireMembers, map[string]any{
			"userId":      m.UserID,
			"displayName": m.DisplayName,
		})
	}
	state := c.broker.StateFor(c.userID)
	if state == nil {
		c.sendError("session_not_found", "session state unavailable")
		return
	}
	hostUserID, hostOnly, _ := c.broker.SessionMeta(c.userID)
	_ = c.registry.Notify(c.userID, map[string]any{
		"type":       kind,
		"sessionId":  sessionID,
		"inviteCode": inviteCode,
		"hostUserId": hostUserID,
		"hostOnly":   hostOnly,
		"state": map[string]any{
			"trackId":            state.TrackID,
			"positionMs":         state.PositionMs,
			"playing":            state.Playing,
			"anchorServerTimeMs": state.AnchorServerTimeMs,
		},
		"members": wireMembers,
	})
}

