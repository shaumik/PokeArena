// Package gwclient is a thin WebSocket client to the gateway's live_pvp
// slot endpoint. It owns exactly one connection and one read pump; the
// caller reads typed server frames from Updates and writes typed actions
// via Send. Close is idempotent and always unblocks Updates.
//
// This package is shared by anything that needs to drive a battle as a
// trainer client: pokearena-mcp (MCP adapter), pokearena-agent (the
// reference agent harness), and the integration test drivers.
package gwclient

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"sync"
	"syscall"

	"github.com/shaumik/PokeArena/internal/protocol"

	"github.com/gorilla/websocket"
)

// Client is a single gateway WebSocket connection.
//
// Concurrency contract: Send may be called from one goroutine at a time
// (gorilla/websocket allows one writer); Updates / Closed / Close may be
// called from any goroutine.
type Client struct {
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

// Dial opens a WS to baseURL + the play path for (battleID, slot, token),
// starts the read pump, and returns the client ready for use. The
// handshake itself respects ctx; the read pump runs in its own goroutine
// and outlives ctx.
func Dial(ctx context.Context, baseURL, battleID, slot, token string) (*Client, error) {
	return dialPath(ctx, baseURL, protocol.PlayPath(battleID, slot, token))
}

// DialLive opens a WS to a single-player live-mode battle, where the opponent
// is the programmatic AI rather than another client. Live mode is tokenless and
// slotless — the human is hardcoded to p1 — so this is the join path an MCP or
// agent client uses to face the Heuristic/Expectimax opponent, the same one the
// SPA's single-player mode plays against.
func DialLive(ctx context.Context, baseURL, battleID string) (*Client, error) {
	return dialPath(ctx, baseURL, protocol.LivePlayPath(battleID))
}

// dialPath is the shared connect: resolve the URL, open the socket, start the
// read pump. Both the pvp (Dial) and live (DialLive) joins differ only in the
// path they connect to.
func dialPath(ctx context.Context, baseURL, path string) (*Client, error) {
	u, err := joinURL(baseURL, path)
	if err != nil {
		return nil, err
	}
	conn, resp, err := websocket.DefaultDialer.DialContext(ctx, u, nil)
	if err != nil {
		return nil, unreachableErr(baseURL, err)
	}
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close() // handshake response body; close it for hygiene
	}
	c := &Client{
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
func (c *Client) Updates() <-chan protocol.MatchUpdate { return c.updates }

// Closed yields the terminal error exactly once when the read pump
// exits: nil on a Close-initiated shutdown, otherwise the underlying
// read error (typically a *websocket.CloseError).
func (c *Client) Closed() <-chan error { return c.closed }

// Send writes one client frame to the server. Errors here are typically
// terminal for the connection — the caller should Close and surface
// the error to the agent.
func (c *Client) Send(msg protocol.WsClientMsg) error {
	return c.conn.WriteJSON(msg)
}

// Close shuts the connection down. Idempotent; safe to call concurrently
// with reads. After Close returns, Updates will drain its buffer and
// close, and Closed will yield nil (not the network error caused by
// our own Close call).
func (c *Client) Close() error {
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
func (c *Client) readPump() {
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

// terminalErr distinguishes a Close-initiated shutdown (where ReadJSON
// returns a "use of closed connection" error that isn't interesting)
// from a genuine server-side or network error.
func (c *Client) terminalErr() error {
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

// unreachableErr turns a refused dial into instructions.
//
// The common case is not a bug: someone installed pokearena-mcp from the MCP
// registry, where the default gateway is ws://localhost:8080, and has no arena
// running. Bare "connection refused" tells them nothing about what an arena is
// or how to get one, and an agent relaying that message cannot help either. So
// name the URL we tried and both ways forward.
//
// Only a dial that could not reach anything gets this treatment; a gateway
// that answered and rejected us is a different problem and keeps its own
// error.
func unreachableErr(baseURL string, err error) error {
	var netErr net.Error
	if !errors.As(err, &netErr) && !errors.Is(err, syscall.ECONNREFUSED) {
		return err
	}
	return fmt.Errorf(
		"no PokéArena gateway at %s: %w\n"+
			"  Start one locally:  docker compose up -d   (then it is at ws://localhost:8080)\n"+
			"  Or point at another arena: set POKEARENA_GATEWAY_URL=wss://your.host",
		baseURL, err)
}
