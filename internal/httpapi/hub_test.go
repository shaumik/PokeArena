package httpapi

import (
	"testing"
	"time"

	"pokearena/internal/messages"
)

// newBareHub builds a Hub with no event queue. Safe as long as the test only
// exercises dispatch (which never touches the queue) and pre-populates the
// subscriber maps directly instead of calling Subscribe (which binds).
func newBareHub() *Hub {
	return &Hub{
		subs:      map[string]map[int]chan Event{},
		frameSubs: map[string]map[int]chan []byte{},
	}
}

func TestHub_DispatchFrame_RoutesToSlot(t *testing.T) {
	h := newBareHub()
	const battleID = "11111111-2222-3333-4444-555555555555"

	p1 := make(chan []byte, 1)
	p2 := make(chan []byte, 1)
	h.frameSubs[frameKey(battleID, "p1")] = map[int]chan []byte{0: p1}
	h.frameSubs[frameKey(battleID, "p2")] = map[int]chan []byte{1: p2}

	h.dispatchFrame(messages.LiveFrameKey(battleID, "p1"), []byte(`{"type":"turn"}`))

	select {
	case got := <-p1:
		if string(got) != `{"type":"turn"}` {
			t.Fatalf("p1 got %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("p1 bridge never received its frame")
	}
	// The other slot must not see p1's frame.
	select {
	case got := <-p2:
		t.Fatalf("p2 bridge wrongly received %q", got)
	default:
	}
}

func TestHub_DispatchFrame_IgnoresUnknownSlot(t *testing.T) {
	h := newBareHub()
	// No panic, no delivery when nobody is bound.
	h.dispatchFrame(messages.LiveFrameKey("nobody", "p1"), []byte(`{}`))
}

func TestHub_Dispatch_DomainEventByBattle(t *testing.T) {
	h := newBareHub()
	const battleID = "abc"
	ch := make(chan Event, 1)
	h.subs[battleID] = map[int]chan Event{0: ch}

	h.dispatch(Event{Type: messages.EventTurnResolved, BattleID: battleID, Body: []byte("x")})
	select {
	case ev := <-ch:
		if ev.Type != messages.EventTurnResolved || ev.BattleID != battleID {
			t.Fatalf("unexpected event %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("watcher never received the domain event")
	}
}
