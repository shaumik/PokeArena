package livebattle

import (
	"context"
	"math/rand"
	"sync"
	"testing"
	"time"

	"pokearena/internal/ai"
	"pokearena/internal/domain"
	"pokearena/internal/engine"
	"pokearena/internal/messages"
	"pokearena/internal/protocol"
)

// --- in-memory host fakes ---
//
// The whole point of extracting the coordinator behind interfaces is that a
// full battle can be exercised with no broker, no Redis, and no WebSockets.
// These fakes are the proof.

type recordSink struct {
	mu     sync.Mutex
	frames [2][]protocol.MatchUpdate
	closed bool
}

func (s *recordSink) SendFrame(slot int, u protocol.MatchUpdate) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.frames[slot] = append(s.frames[slot], u)
}
func (s *recordSink) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
}
func (s *recordSink) frameCount(slot int) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.frames[slot])
}

// chanSink forwards frames to per-slot channels so a test "client" can react to
// them in real time. Buffered deeply enough that the coordinator never blocks.
type chanSink struct {
	ch [2]chan protocol.MatchUpdate
}

func newChanSink() *chanSink {
	return &chanSink{ch: [2]chan protocol.MatchUpdate{
		make(chan protocol.MatchUpdate, 256),
		make(chan protocol.MatchUpdate, 256),
	}}
}
func (s *chanSink) SendFrame(slot int, u protocol.MatchUpdate) { s.ch[slot] <- u }
func (s *chanSink) Close() {
	close(s.ch[0])
	close(s.ch[1])
}

type fakeStore struct {
	mu        sync.Mutex
	status    string
	completed bool
	winner    int
	turns     int
}

func (f *fakeStore) SetBattleStatus(_ context.Context, _, status string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status = status
	return nil
}
func (f *fakeStore) AppendTurn(context.Context, string, int, []byte, []byte) error { return nil }
func (f *fakeStore) CompleteBattle(_ context.Context, _ string, winner, turns int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.completed, f.winner, f.turns = true, winner, turns
	return nil
}

type fakeCache struct {
	mu           sync.Mutex
	stateDeleted bool
	tokensGone   bool
}

func (f *fakeCache) SaveState(context.Context, *engine.BattleState) error { return nil }
func (f *fakeCache) DeleteState(context.Context, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stateDeleted = true
	return nil
}
func (f *fakeCache) DeletePvPTokens(context.Context, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tokensGone = true
	return nil
}

type eventRecorder struct {
	mu    sync.Mutex
	types []string
}

func (e *eventRecorder) publish(_ context.Context, eventType, _ string, _ any) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.types = append(e.types, eventType)
}
func (e *eventRecorder) saw(eventType string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, t := range e.types {
		if t == eventType {
			return true
		}
	}
	return false
}

// legalAI always returns the engine's first legal action. With full-state
// access (which the coordinator hands it) this is enough to drive a battle to
// completion deterministically — exactly what we want for a turn-loop test.
type legalAI struct{}

func (legalAI) Start(context.Context) {}
func (legalAI) Decide(_ context.Context, st *engine.BattleState, side int) (engine.Action, string) {
	return engine.LegalActions(st, side)[0], ""
}
func (legalAI) DecideReplace(_ context.Context, st *engine.BattleState, side int) engine.Action {
	return engine.LegalActions(st, side)[0]
}

func loadDex(t *testing.T) *domain.Dex {
	t.Helper()
	d, err := domain.LoadDex("../../data", "test")
	if err != nil {
		t.Fatalf("load dex: %v", err)
	}
	return d
}

func twoTeams(t *testing.T, dex *domain.Dex) ([]engine.TeamPick, []engine.TeamPick) {
	t.Helper()
	pool, err := ai.LoadTeamPool(dex, "../../data/ai-teams.json")
	if err != nil {
		t.Fatalf("load team pool: %v", err)
	}
	t1, err := pool.Pick(rand.New(rand.NewSource(1)))
	if err != nil {
		t.Fatalf("pick team 1: %v", err)
	}
	t2, err := pool.Pick(rand.New(rand.NewSource(2)))
	if err != nil {
		t.Fatalf("pick team 2: %v", err)
	}
	return t1, t2
}

// TestMatch_FullBattleTwoAI runs a complete battle with both slots AI-driven
// over the in-memory host. It exercises the open phase (auto-submit), the turn
// loop, persistence, and the finish/cleanup path — end to end, no infra.
func TestMatch_FullBattleTwoAI(t *testing.T) {
	dex := loadDex(t)
	t1, t2 := twoTeams(t, dex)

	store := &fakeStore{}
	cache := &fakeCache{}
	events := &eventRecorder{}
	sink := &recordSink{}

	m := NewMatch(Config{
		BattleID: "B-ai", P1Name: "Red", P2Name: "Blue", Seed: 42,
		Kinds:   [2]SideKind{SideAI, SideAI},
		AITeams: [2][]engine.TeamPick{t1, t2},
		Sink:    sink,
		Deps: Deps{
			Dex: dex, Cache: cache, Store: store, Publish: events.publish,
			AI: legalAI{},
		},
	})

	done := make(chan struct{})
	go func() { m.Run(context.Background()); close(done) }()

	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("battle did not finish within 30s")
	}

	if !store.completed {
		t.Fatal("CompleteBattle was never called — battle did not finish")
	}
	if store.winner != 0 && store.winner != 1 {
		t.Fatalf("winner = %d, want 0 or 1", store.winner)
	}
	if !cache.stateDeleted {
		t.Fatal("final state was not deleted from cache")
	}
	for _, want := range []string{messages.EventBattleStarted, messages.EventTurnResolved, messages.EventBattleCompleted} {
		if !events.saw(want) {
			t.Fatalf("expected event %q to be published", want)
		}
	}
	// AI slots must never receive frames (no drainer); a non-zero count would
	// mean the send-skip for AI slots regressed.
	if n := sink.frameCount(0); n != 0 {
		t.Fatalf("AI slot 0 received %d frames, want 0", n)
	}
	if !sink.closed {
		t.Fatal("sink was not closed on shutdown")
	}
}

// TestMatch_PvPPickerThenDisconnect drives the two-WS path through the picker
// room to ACTIVE, then disconnects one slot and asserts the coordinator winds
// down. This is the live_pvp shape with no AI and no broker.
func TestMatch_PvPPickerThenDisconnect(t *testing.T) {
	dex := loadDex(t)
	t1, t2 := twoTeams(t, dex)

	sink := newChanSink()
	m := NewMatch(Config{
		BattleID: "B-pvp", P1Name: "Red", P2Name: "Blue", Seed: 7,
		Kinds: [2]SideKind{SideWS, SideWS},
		Sink:  sink,
		Deps: Deps{
			Dex: dex, Cache: &fakeCache{}, Store: &fakeStore{}, Publish: (&eventRecorder{}).publish,
		},
	})

	done := make(chan struct{})
	go func() { m.Run(context.Background()); close(done) }()

	p0, ok := m.Attach(0)
	if !ok {
		t.Fatal("attach slot 0 failed")
	}
	p1, ok := m.Attach(1)
	if !ok {
		t.Fatal("attach slot 1 failed")
	}
	// Second attach of the same slot must lose.
	if _, ok := m.Attach(0); ok {
		t.Fatal("double-attach of slot 0 unexpectedly won")
	}

	p0.Submits <- t1
	p1.Submits <- t2

	// Drain slot 1's frames so the coordinator never blocks; watch slot 0 for
	// the FrameState that signals the battle went ACTIVE.
	go func() {
		for range sink.ch[1] {
		}
	}()
	if !waitForFrame(t, sink.ch[0], protocol.FrameState, 5*time.Second) {
		t.Fatal("never observed FrameState — battle did not reach ACTIVE")
	}

	// Drop slot 0; the coordinator should detect the disconnect and shut down.
	m.Disconnect(0)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("coordinator did not shut down after disconnect")
	}
}

// TestMatch_RoomDeadlineExpires asserts an unclaimed room dies on its deadline.
func TestMatch_RoomDeadlineExpires(t *testing.T) {
	dex := loadDex(t)
	sink := newChanSink()
	m := NewMatch(Config{
		BattleID: "B-dead", P1Name: "Red", P2Name: "Blue", Seed: 1,
		Kinds:        [2]SideKind{SideWS, SideWS},
		Sink:         sink,
		RoomDeadline: 100 * time.Millisecond,
		Deps: Deps{
			Dex: dex, Cache: &fakeCache{}, Store: &fakeStore{}, Publish: (&eventRecorder{}).publish,
		},
	})
	go func() {
		for range sink.ch[0] {
		}
	}()
	go func() {
		for range sink.ch[1] {
		}
	}()

	done := make(chan struct{})
	go func() { m.Run(context.Background()); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("room did not expire on its deadline")
	}
}

func waitForFrame(t *testing.T, ch <-chan protocol.MatchUpdate, frameType string, timeout time.Duration) bool {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case u, ok := <-ch:
			if !ok {
				return false
			}
			if u.Type == frameType {
				return true
			}
		case <-deadline:
			return false
		}
	}
}
