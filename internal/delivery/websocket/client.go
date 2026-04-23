package websocket

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"time"

	"github.com/coder/websocket"

	"github.com/sean-phyx/syncjam-api/internal/application"
	"github.com/sean-phyx/syncjam-api/internal/domain"
)

// outboundBufferSize bounds queued broadcasts per client. A client
// whose buffer fills faster than it drains gets its messages dropped
// (see Registry.Notify).
const outboundBufferSize = 32

// helloTimeout is the deadline for a new connection's first message.
const helloTimeout = 10 * time.Second

type client struct {
	conn        *websocket.Conn
	broker      *application.Broker
	registry    *Registry
	subsonic    application.SubsonicVerifier
	subsonicURL string

	// Populated after hello_ack.
	userID   string
	outbound chan []byte
}

func (c *client) run(ctx context.Context) {
	defer func() {
		if c.userID != "" {
			c.registry.Unregister(c.userID)
			c.broker.Unregister(c.userID)
		}
		_ = c.conn.Close(websocket.StatusNormalClosure, "bye")
	}()

	if err := c.awaitHello(ctx); err != nil {
		log.Printf("[ws] hello failed: %v", err)
		return
	}

	writerCtx, cancelWriter := context.WithCancel(ctx)
	defer cancelWriter()
	go c.writer(writerCtx)
	go c.keepalive(writerCtx)

	for {
		_, data, err := c.conn.Read(ctx)
		if err != nil {
			return
		}
		c.handle(ctx, data)
	}
}

// keepalive sends WebSocket-level PING frames so middleboxes don't
// close the connection as idle. Application-level clock_pings are JSON,
// invisible to proxies as keepalive signals.
func (c *client) keepalive(ctx context.Context) {
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := c.conn.Ping(pingCtx)
			cancel()
			if err != nil {
				log.Printf("[ws] keepalive ping failed: %v", err)
				_ = c.conn.Close(websocket.StatusGoingAway, "ping timeout")
				return
			}
		}
	}
}

// awaitHello blocks for one message; rejects anything that isn't a
// valid hello within helloTimeout.
func (c *client) awaitHello(ctx context.Context) error {
	hctx, cancel := context.WithTimeout(ctx, helloTimeout)
	defer cancel()
	_, data, err := c.conn.Read(hctx)
	if err != nil {
		c.sendError("invalid_message", "expected hello")
		return err
	}
	var msg struct {
		Type string          `json:"type"`
		Auth json.RawMessage `json:"auth"`
	}
	if err := json.Unmarshal(data, &msg); err != nil || msg.Type != "hello" {
		c.sendError("invalid_message", "first message must be hello")
		return errors.New("not hello")
	}

	identity, err := c.verify(ctx, msg.Auth)
	if err != nil {
		code := mapErrorCode(err)
		c.sendError(code, err.Error())
		return err
	}

	c.userID = c.broker.Register(identity)
	c.outbound = make(chan []byte, outboundBufferSize)
	c.registry.Register(c.userID, c.outbound)

	return c.sendRaw(map[string]any{
		"type":        "hello_ack",
		"userId":      c.userID,
		"displayName": identity.DisplayName,
	})
}

func (c *client) verify(ctx context.Context, rawAuth json.RawMessage) (*domain.AuthedIdentity, error) {
	var creds application.SubsonicCreds
	if err := json.Unmarshal(rawAuth, &creds); err != nil {
		return nil, domain.ErrAuthFailed
	}
	return c.subsonic.Verify(ctx, c.subsonicURL, creds)
}

// writer serialises broadcasts — the WebSocket only supports one
// concurrent writer per connection.
func (c *client) writer(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case data := <-c.outbound:
			if err := c.conn.Write(ctx, websocket.MessageText, data); err != nil {
				return
			}
		}
	}
}

// sendRaw writes directly, bypassing the outbound channel. For use
// before the channel + writer goroutine exist (hello handshake).
func (c *client) sendRaw(msg any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return c.conn.Write(context.Background(), websocket.MessageText, data)
}

func (c *client) sendError(code, message string) {
	_ = c.sendRaw(map[string]any{
		"type":    "error",
		"code":    code,
		"message": message,
	})
}

// mapErrorCode renders a domain sentinel as a protocol error code.
func mapErrorCode(err error) string {
	switch {
	case errors.Is(err, domain.ErrAuthFailed):
		return "auth_failed"
	case errors.Is(err, domain.ErrBackendUnreachable):
		return "backend_unreachable"
	case errors.Is(err, domain.ErrSessionNotFound):
		return "session_not_found"
	case errors.Is(err, domain.ErrNotInSession):
		return "not_in_session"
	case errors.Is(err, domain.ErrNotAuthorised):
		return "not_authorised"
	default:
		return "backend_unreachable"
	}
}
