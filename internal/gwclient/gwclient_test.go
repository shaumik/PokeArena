package gwclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"pokearena/internal/protocol"

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
	gc, err := Dial(ctx, base, "battle-x", "p1", "tok", "")
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
	gc, err := Dial(ctx, base, "b", "p2", "t", "")
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
	gc, err := DialLive(ctx, base, "battle-live-1", "")
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
	gc, err := Dial(ctx, base, "b", "p1", "t", "")
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
	gc, err := Dial(ctx, base, "b", "p1", "t", "")
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

// captureJoin runs a gateway that records the query of the join request, so a
// test can assert what a client actually sent rather than what it meant to.
func captureJoin(t *testing.T) (base string, got *url.Values, cleanup func()) {
	t.Helper()
	var seen url.Values
	up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.URL.Query()
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		blockUntilPeerClose(c)
	}))
	return "ws" + strings.TrimPrefix(srv.URL, "http"), &seen, srv.Close
}

// The trainer name is how a bot's results get attributed on the leaderboard;
// if it never leaves the client, every agent's games post under the placeholder
// the battle's creator chose.
func TestDial_SendsTrainerName(t *testing.T) {
	base, seen, cleanup := captureJoin(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	gc, err := Dial(ctx, base, "battle-x", "p2", "tok", "claude-haiku")
	must(t, "dial", err)
	defer gc.Close()

	if got := seen.Get("name"); got != "claude-haiku" {
		t.Errorf("name query = %q, want %q", got, "claude-haiku")
	}
	if got := seen.Get("slot"); got != "p2" {
		t.Errorf("slot query = %q, want p2", got)
	}
}

func TestDial_OmitsTrainerNameWhenUndeclared(t *testing.T) {
	base, seen, cleanup := captureJoin(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	gc, err := Dial(ctx, base, "battle-x", "p2", "tok", "")
	must(t, "dial", err)
	defer gc.Close()

	// Absent, not empty: the gateway reads a missing name as "keep the
	// creator's", and an empty one would sanitize to the same thing — but
	// sending it at all would misrepresent an anonymous join as a declaration.
	if seen.Has("name") {
		t.Errorf("name query present (%q), want it omitted entirely", seen.Get("name"))
	}
}

// Live mode is routed by the *absence* of a slot param, so adding a name must
// not turn a vs-AI join into a pvp one.
func TestDialLive_SendsTrainerNameWithoutASlot(t *testing.T) {
	base, seen, cleanup := captureJoin(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	gc, err := DialLive(ctx, base, "battle-x", "claude-opus")
	must(t, "dial", err)
	defer gc.Close()

	if got := seen.Get("name"); got != "claude-opus" {
		t.Errorf("name query = %q, want %q", got, "claude-opus")
	}
	if seen.Has("slot") {
		t.Errorf("slot query present (%q) — this would route to the pvp handler", seen.Get("slot"))
	}
}
