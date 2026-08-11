package livebattle

import (
	"context"
	"testing"
	"time"

	"pokearena/internal/messages"
	"pokearena/internal/protocol"
)

// A live_pvp battle is created before its players arrive, so both slots carry
// names chosen by whoever pressed "Start" — an agent joining p2 was recorded as
// "Opponent". A slot that declares a name on attach must replace it, because
// that name is the leaderboard key the battle's result posts to.
func TestAttach_DeclaredTrainerNameReplacesCreatorPlaceholder(t *testing.T) {
	dex := loadDex(t)
	t1, t2 := twoTeams(t, dex)

	sink := newChanSink()
	m := NewMatch(Config{
		BattleID: "B-name", P1Name: "Red", P2Name: "Opponent", Seed: 7,
		Kinds: [2]SideKind{SideWS, SideWS},
		Sink:  sink,
		Deps: Deps{
			Dex: dex, Cache: &fakeCache{}, Store: &fakeStore{}, Publish: (&eventRecorder{}).publish,
		},
	})
	pump := NewPump(m, [2]SideKind{SideWS, SideWS})

	done := make(chan struct{})
	go func() { m.Run(context.Background()); close(done) }()
	defer func() {
		go func() {
			for range sink.ch[0] {
			}
		}()
		go func() {
			for range sink.ch[1] {
			}
		}()
		pump.Route(messages.LiveAction{BattleID: "B-name", Slot: "p1", Phase: messages.LivePhaseDisconnect})
		<-done
	}()

	// p1 attaches anonymously (keeps "Red"); p2 declares itself.
	pump.Route(messages.LiveAction{BattleID: "B-name", Slot: "p1", Phase: messages.LivePhaseAttach})
	pump.Route(messages.LiveAction{
		BattleID: "B-name", Slot: "p2", Phase: messages.LivePhaseAttach, Trainer: "claude-haiku",
	})

	// The room frame is the first place the opponent sees who they're facing.
	// p1 gets a room frame at its own attach too, before p2 exists — wait for
	// the broadcast that actually reflects p2 being in the room.
	room := readRoomUntilThemAttached(t, sink.ch[0], 5*time.Second)
	if got := room.You.Trainer; got != "Red" {
		t.Errorf("p1 (anonymous attach) = %q, want the creator's name %q", got, "Red")
	}
	if got := room.Them.Trainer; got != "claude-haiku" {
		t.Errorf("p2 as seen by p1 = %q, want the declared name %q", got, "claude-haiku")
	}

	// And it must reach the engine state, which is what gets persisted and
	// replayed — a name that only lived in a room frame would be cosmetic.
	pump.Route(messages.LiveAction{BattleID: "B-name", Slot: "p1", Phase: messages.LivePhaseSubmit, Picks: t1})
	pump.Route(messages.LiveAction{BattleID: "B-name", Slot: "p2", Phase: messages.LivePhaseSubmit, Picks: t2})

	// Each side sees its own Side in full; the fog projection reduces the foe
	// to their active Pokémon, so p2's name is asserted from p2's own view.
	s0 := readUntil(t, sink.ch[0], protocol.FrameState, 5*time.Second)
	s1 := readUntil(t, sink.ch[1], protocol.FrameState, 5*time.Second)
	if s0.View == nil || s1.View == nil {
		t.Fatal("FrameState carried no view")
	}
	if got := s0.View.Self.Trainer; got != "Red" {
		t.Errorf("engine state p1 trainer = %q, want %q", got, "Red")
	}
	if got := s1.View.Self.Trainer; got != "claude-haiku" {
		t.Errorf("engine state p2 trainer = %q, want the declared name %q", got, "claude-haiku")
	}
}

// readRoomUntilThemAttached returns the first FrameRoom whose opponent slot is
// attached. A slot receives a room broadcast on its own attach as well, and
// that one necessarily predates the other side joining.
func readRoomUntilThemAttached(t *testing.T, ch <-chan protocol.MatchUpdate, timeout time.Duration) *protocol.RoomUpdate {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case u, ok := <-ch:
			if !ok {
				t.Fatal("frame channel closed before the opponent attached")
			}
			if u.Type == protocol.FrameRoom && u.Room != nil && u.Room.Them.Attached {
				return u.Room
			}
		case <-deadline:
			t.Fatal("timed out waiting for a room frame with the opponent attached")
		}
	}
}

// set is the write half of the concurrency unit; an empty declaration must be
// a no-op rather than a store, or every anonymous attach would blank the label.
func TestTrainerNames_EmptyDeclarationKeepsExisting(t *testing.T) {
	names := trainerNames{name: [2]string{"Red", "Blue"}}

	names.set(1, "")
	if got := names.get(1); got != "Blue" {
		t.Errorf("after empty set, name = %q, want the existing %q", got, "Blue")
	}

	names.set(1, "claude-opus")
	if got := names.get(1); got != "claude-opus" {
		t.Errorf("after set, name = %q, want %q", got, "claude-opus")
	}

	// A re-attach that declares nothing must not undo the name from the first.
	names.set(1, "")
	if got := names.get(1); got != "claude-opus" {
		t.Errorf("re-attach blanked the name: got %q, want %q", got, "claude-opus")
	}
}
