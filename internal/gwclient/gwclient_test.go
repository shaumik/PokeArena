package gwclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/shaumik/PokeArena/internal/protocol"

	"github.com/gorilla/websocket"
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

func must(t *testing.T, label string, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", label, err)
	}
}

// blockUntilPeerClose is the standard server-side body for "stay alive
// until the client hangs up".
func blockUntilPeerClose(conn *websocket.Conn) {
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
	}
}

func TestDialAndReceive(t *testing.T) {
	base, cleanup := fakeGateway(t, func(t *testing.T, conn *websocket.Conn) {
		must(t, "state", conn.WriteJSON(protocol.MatchUpdate{Type: protocol.FrameState, Turn: 0}))
		must(t, "turn", conn.WriteJSON(protocol.MatchUpdate{Type: protocol.FrameTurn, Turn: 1}))
		blockUntilPeerClose(conn)
	})
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	gc, err := Dial(ctx, base, "battle-x", "p1", "tok")
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
	gc, err := Dial(ctx, base, "b", "p2", "t")
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

// TestDialLive_UsesTokenlessPath pins the routing-critical property of live
// mode: DialLive must connect to /api/battles/{id}/play with NO slot or token
// query. The gateway dispatches to the pvp handler the instant a slot param is
// present, so an accidental query here would silently route a vs-AI join to the
// pvp path and it would be rejected as "not joinable as a pvp slot".
func TestDialLive_UsesTokenlessPath(t *testing.T) {
	gotPath := make(chan string, 1)
	up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath <- r.URL.RequestURI() // path + raw query, exactly as the gateway routes on
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer c.Close()
		_ = c.WriteJSON(protocol.MatchUpdate{Type: protocol.FrameState, Turn: 0})
		blockUntilPeerClose(c)
	}))
	defer srv.Close()
	base := "ws" + strings.TrimPrefix(srv.URL, "http")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	gc, err := DialLive(ctx, base, "battle-live-1")
	must(t, "dial live", err)
	defer gc.Close()

	select {
	case p := <-gotPath:
		if want := "/api/battles/battle-live-1/play"; p != want {
			t.Fatalf("live join hit %q, want %q (any slot/token query would misroute to pvp)", p, want)
		}
	case <-time.After(time.Second):
		t.Fatal("server never received the live join request")
	}
}

func TestCloseIsCleanAndIdempotent(t *testing.T) {
	base, cleanup := fakeGateway(t, func(t *testing.T, conn *websocket.Conn) {
		blockUntilPeerClose(conn)
	})
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	gc, err := Dial(ctx, base, "b", "p1", "t")
	must(t, "dial", err)

	must(t, "close 1", gc.Close())
	must(t, "close 2", gc.Close())

	select {
	case err := <-gc.Closed():
		if err != nil {
			t.Errorf("Closed after Close()=nil, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("readPump did not signal Closed within 1s of Close()")
	}

	if _, ok := <-gc.Updates(); ok {
		t.Error("Updates not closed after Close")
	}
}

func TestServerCloseReportsError(t *testing.T) {
	base, cleanup := fakeGateway(t, func(t *testing.T, conn *websocket.Conn) {
		must(t, "state", conn.WriteJSON(protocol.MatchUpdate{Type: protocol.FrameState}))
		conn.Close()
	})
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	gc, err := Dial(ctx, base, "b", "p1", "t")
	must(t, "dial", err)
	defer gc.Close()

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

func drain(t *testing.T, gc *Client, n int) []protocol.MatchUpdate {
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

// A refused dial is the single most likely first experience of anyone who
// installed pokearena-mcp from the MCP registry: the default gateway is
// ws://localhost:8080 and they have no arena running. The error has to say
// what to do about it, because "connection refused" does not.
func TestDial_UnreachableGatewayExplainsItself(t *testing.T) {
	// Port 1 is reserved and nothing listens on it, so this dial is refused
	// rather than hanging or resolving somewhere unexpected.
	const base = "ws://127.0.0.1:1"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := Dial(ctx, base, "battle-x", "p1", "tok")
	if err == nil {
		t.Fatal("dial to a dead port returned no error")
	}

	msg := err.Error()
	for _, want := range []string{
		base,                    // which gateway we actually tried
		"docker compose up -d",  // how to get one locally
		"POKEARENA_GATEWAY_URL", // how to point somewhere else
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("error should mention %q, got:\n%s", want, msg)
		}
	}
}
