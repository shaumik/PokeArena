package mcpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"pokearena/internal/protocol"
)

// fakeGateway spins up an httptest server with one WS endpoint. Each
// connection runs the provided handler — the test scripts the server
// side of the conversation by writing/reading frames directly.
func fakeGateway(t *testing.T, handler func(t *testing.T, conn *websocket.Conn)) (baseURL string, cleanup func()) {
	t.Helper()
	up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer c.Close()
		handler(t, c)
	}))
	// httptest gives http://; the WS dialer needs ws://.
	return "ws" + strings.TrimPrefix(srv.URL, "http"), srv.Close
}

// must is a tiny helper so test bodies stay readable when the same
// "fatal-on-err" pattern repeats. The shadow of `must` from cmd/pvp-smoke
// is deliberate — same idea, different scope.
func must(t *testing.T, label string, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", label, err)
	}
}

func TestDialAndReceive(t *testing.T) {
	// Server side: send one state frame, one turn frame, then sit idle
	// until the client closes. Both frames carry a turn number we can
	// assert on the client side.
	base, cleanup := fakeGateway(t, func(t *testing.T, conn *websocket.Conn) {
		must(t, "state", conn.WriteJSON(protocol.MatchUpdate{Type: protocol.FrameState, Turn: 0}))
		must(t, "turn", conn.WriteJSON(protocol.MatchUpdate{Type: protocol.FrameTurn, Turn: 1}))
		// Block until the client closes the conn so the deferred Close()
		// in fakeGateway doesn't race the client's reads.
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	gc, err := dialGateway(ctx, base, "battle-x", "p1", "tok")
	must(t, "dial", err)
	defer gc.Close()

	got := drain(t, gc, 2)
	if got[0].Type != protocol.FrameState || got[0].Turn != 0 {
		t.Errorf("frame 0: %+v", got[0])
	}
	if got[1].Type != protocol.FrameTurn || got[1].Turn != 1 {
		t.Errorf("frame 1: %+v", got[1])
	}
}

func TestSendAction(t *testing.T) {
	// Server side: read one action frame, echo it back as an info frame
	// with the kind embedded in Message so we can assert the round-trip
	// preserved field values.
	base, cleanup := fakeGateway(t, func(t *testing.T, conn *websocket.Conn) {
		var msg protocol.WsClientMsg
		must(t, "read action", conn.ReadJSON(&msg))
		must(t, "ack", conn.WriteJSON(protocol.MatchUpdate{
			Type: protocol.FrameInfo, Message: msg.Kind, Turn: msg.Index,
		}))
	})
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	gc, err := dialGateway(ctx, base, "b", "p2", "t")
	must(t, "dial", err)
	defer gc.Close()

	must(t, "send", gc.Send(protocol.WsClientMsg{
		Type: "action", Kind: protocol.ActionKindSwitch, Index: 3,
	}))
	ack := drain(t, gc, 1)[0]
	if ack.Message != protocol.ActionKindSwitch || ack.Turn != 3 {
		t.Errorf("ack lost fields: %+v", ack)
	}
}

func TestCloseIsCleanAndIdempotent(t *testing.T) {
	// Server side: send nothing, just sit on the connection so the
	// client's readPump is blocked in ReadJSON when we Close.
	base, cleanup := fakeGateway(t, func(t *testing.T, conn *websocket.Conn) {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	gc, err := dialGateway(ctx, base, "b", "p1", "t")
	must(t, "dial", err)

	// Close twice — must be idempotent.
	must(t, "close 1", gc.Close())
	must(t, "close 2", gc.Close())

	// After our Close, Closed must yield nil — we initiated the shutdown,
	// so the underlying "use of closed network connection" error must be
	// suppressed.
	select {
	case err := <-gc.Closed():
		if err != nil {
			t.Errorf("Closed after Close()=nil, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("readPump did not signal Closed within 1s of Close()")
	}

	// Updates must be closed (drains and yields the zero value).
	if _, ok := <-gc.Updates(); ok {
		t.Error("Updates not closed after Close")
	}
}

func TestServerCloseReportsError(t *testing.T) {
	// Server side: send one frame then abort the connection without a
	// close handshake. The client's readPump should see a non-nil error
	// and surface it via Closed.
	base, cleanup := fakeGateway(t, func(t *testing.T, conn *websocket.Conn) {
		must(t, "state", conn.WriteJSON(protocol.MatchUpdate{Type: protocol.FrameState}))
		conn.Close() // abrupt; no close frame
	})
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	gc, err := dialGateway(ctx, base, "b", "p1", "t")
	must(t, "dial", err)
	defer gc.Close()

	// Drain the one expected frame, then wait for the error.
	drain(t, gc, 1)
	select {
	case err := <-gc.Closed():
		if err == nil {
			t.Error("server abort produced nil Closed error; expected non-nil")
		}
	case <-time.After(time.Second):
		t.Fatal("readPump never reported terminal error")
	}
}

func TestJoinURL(t *testing.T) {
	// Smoke: the joiner must survive trailing slashes and the path's
	// own query string, since that's what the gateway-issued playURL
	// looks like.
	cases := []struct {
		base, path, want string
	}{
		{"ws://h:1", "/a?x=1", "ws://h:1/a?x=1"},
		{"ws://h:1/", "/a?x=1", "ws://h:1/a?x=1"},
		{"wss://h", "/api/battles/X/play?slot=p1&token=t", "wss://h/api/battles/X/play?slot=p1&token=t"},
	}
	for _, c := range cases {
		got, err := joinURL(c.base, c.path)
		if err != nil {
			t.Errorf("joinURL(%q,%q): %v", c.base, c.path, err)
			continue
		}
		if got != c.want {
			t.Errorf("joinURL(%q,%q) = %q; want %q", c.base, c.path, got, c.want)
		}
	}
}

// drain reads the next n frames from gc.Updates with a short test
// timeout. Common to several tests so its existence is earned.
func drain(t *testing.T, gc *gwClient, n int) []protocol.MatchUpdate {
	t.Helper()
	out := make([]protocol.MatchUpdate, 0, n)
	for i := 0; i < n; i++ {
		select {
		case u, ok := <-gc.Updates():
			if !ok {
				t.Fatalf("Updates closed before frame %d/%d", i, n)
			}
			out = append(out, u)
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for frame %d/%d", i, n)
		}
	}
	return out
}
