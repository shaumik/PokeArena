package mcpserver

import (
	"context"
	"net/url"
	"sync"

	"github.com/gorilla/websocket"

	"pokearena/internal/protocol"
)

// gwClient is a thin WebSocket client to the gateway's live_pvp slot
// endpoint. It owns exactly one connection and one read pump; the caller
// reads typed server frames from Updates and writes typed actions via
// Send. Close is idempotent and always unblocks Updates.
//
// Concurrency contract: Send may be called from one goroutine at a time
// (gorilla/websocket allows one writer); Updates / Closed / Close may be
// called from any goroutine.
type gwClient struct {
	conn *websocket.Conn

	updates chan protocol.MatchUpdate // server → caller; closed when read pump exits
	closed  chan error                // single value: terminal error from read pump (nil = clean)
	stop    chan struct{}             // signals read pump to exit cleanly
	once    sync.Once                 // guards Close

	// lastReadErr is set by readPump on its way out and read by the same
	// goroutine in the deferred terminalErr — no cross-goroutine access,
	// so no sync needed.
	lastReadErr error
}

// dialGateway opens a WS to baseURL + the play path for (battleID, slot,
// token), starts the read pump, and returns the client ready for use.
// The handshake itself respects ctx; the read pump runs in its own
// goroutine and outlives ctx.
func dialGateway(ctx context.Context, baseURL, battleID, slot, token string) (*gwClient, error) {
	u, err := joinURL(baseURL, protocol.PlayPath(battleID, slot, token))
	if err != nil {
		return nil, err
	}
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, u, nil)
	if err != nil {
		return nil, err
	}
	c := &gwClient{
		conn:    conn,
		updates: make(chan protocol.MatchUpdate, 8), // matches gateway's per-slot buffer
		closed:  make(chan error, 1),
		stop:    make(chan struct{}),
	}
	go c.readPump()
	return c, nil
}

// Updates is the stream of frames from the server. The channel is
// closed once the connection ends (either side); after that, drain
// Closed to learn whether it was clean or an error.
func (c *gwClient) Updates() <-chan protocol.MatchUpdate { return c.updates }

// Closed yields the terminal error exactly once when the read pump
// exits: nil on a Close-initiated shutdown, otherwise the underlying
// read error (typically a *websocket.CloseError).
func (c *gwClient) Closed() <-chan error { return c.closed }

// Send writes one client frame to the server. Errors here are typically
// terminal for the connection — the caller should Close and surface
// the error to the agent.
func (c *gwClient) Send(msg protocol.WsClientMsg) error {
	return c.conn.WriteJSON(msg)
}

// Close shuts the connection down. Idempotent; safe to call concurrently
// with reads. After Close returns, Updates will drain its buffer and
// close, and Closed will yield nil (not the network error caused by
// our own Close call).
func (c *gwClient) Close() error {
	c.once.Do(func() {
		close(c.stop)
		// The read pump will see either a read error from this close or
		// notice c.stop; either way it cleans up. We don't propagate the
		// Close error because there's nothing useful for the caller to
		// do with it (the connection is gone by definition).
		_ = c.conn.Close()
	})
	return nil
}

// readPump reads frames until the connection ends or Close is called.
// Each frame is forwarded to c.updates via a select against c.stop, so
// a slow consumer can't keep the pump alive past a Close call. On exit
// it reports the terminal state on c.closed and closes c.updates.
func (c *gwClient) readPump() {
	defer close(c.updates)
	defer func() {
		// c.closed is buffered cap 1 so this is always non-blocking,
		// even if the consumer never reads it.
		select {
		case c.closed <- c.terminalErr():
		default:
		}
	}()

	for {
		var u protocol.MatchUpdate
		if err := c.conn.ReadJSON(&u); err != nil {
			c.lastReadErr = err
			return
		}
		select {
		case c.updates <- u:
		case <-c.stop:
			return
		}
	}
}

// lastReadErr / terminalErr distinguish a Close-initiated shutdown
// (where ReadJSON returns a "use of closed connection" error that
// isn't interesting) from a genuine server-side or network error.
func (c *gwClient) terminalErr() error {
	select {
	case <-c.stop:
		return nil // we initiated; suppress the noise
	default:
		return c.lastReadErr
	}
}

// joinURL combines a base ("ws://host:8080") with a path that may
// include a query ("/api/.../play?slot=p1&token=…"). Done via net/url
// rather than string concatenation so a trailing slash on baseURL or a
// missing leading slash on path doesn't quietly produce a broken URL.
func joinURL(baseURL, path string) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	p, err := url.Parse(path)
	if err != nil {
		return "", err
	}
	u.Path = p.Path
	u.RawQuery = p.RawQuery
	return u.String(), nil
}
