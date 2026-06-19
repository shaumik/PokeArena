package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"math/rand"
	"sync"
	"time"

	"github.com/google/uuid"

	"pokearena/internal/cache"
	"pokearena/internal/engine"
	"pokearena/internal/livebattle"
	"pokearena/internal/messages"
	"pokearena/internal/protocol"
)

// turnDecisionBudget is the AI decider's per-turn budget — if the ai-service
// hasn't replied with EventAIDecided by then, the gateway falls back to the
// local heuristic so the turn never stalls.
const turnDecisionBudget = 90 * time.Second

// gwMatch bundles a live coordinator with the per-slot frame channels the WS
// writers drain. The coordinator (in internal/livebattle) is transport-agnostic;
// wsSink is the gateway's in-process backing for its FrameSink.
type gwMatch struct {
	match *livebattle.Match
	sink  *wsSink
}

// wsSink implements livebattle.FrameSink with one buffered channel per slot. A
// slow writer that fills the 8-frame buffer back-pressures the coordinator —
// better than silently dropping state-coherence frames. The coordinator never
// calls SendFrame for an AI slot, so the unread channel can't deadlock.
type wsSink struct {
	ch [2]chan protocol.MatchUpdate
}

func newWSSink() *wsSink {
	return &wsSink{ch: [2]chan protocol.MatchUpdate{
		make(chan protocol.MatchUpdate, 8),
		make(chan protocol.MatchUpdate, 8),
	}}
}

func (s *wsSink) SendFrame(slot int, u protocol.MatchUpdate)  { s.ch[slot] <- u }
func (s *wsSink) frames(slot int) <-chan protocol.MatchUpdate { return s.ch[slot] }
func (s *wsSink) Close() {
	close(s.ch[0])
	close(s.ch[1])
}

// gwAttach is the handle a WS handler gets after registering its slot: a
// Producer to push actions/submissions, the frame channel to drain, and a
// disconnect signal to fire on socket close.
type gwAttach struct {
	producer   livebattle.Producer
	frames     <-chan protocol.MatchUpdate
	disconnect func()
}

// liveDeps builds the host dependencies the coordinator needs. ai may be nil
// for an all-WS match (live_pvp).
func (s *Server) liveDeps(battleID string, ai livebattle.AIDecider) livebattle.Deps {
	return livebattle.Deps{
		Dex:     s.dex,
		Cache:   s.cache,
		Store:   s.store,
		Publish: s.publishLiveEvent,
		AI:      ai,
		OnDone:  s.detachPvPMatch,
	}
}

// startPvPRoom creates a two-WS Room eagerly at POST time. The picker deadline
// starts ticking immediately so an unclaimed URL can't sit in memory past its
// budget. Idempotent: a no-op if a Room for battleID already exists.
func (s *Server) startPvPRoom(battleID, p1Name, p2Name string, seed uint64) {
	s.matchesMu.Lock()
	defer s.matchesMu.Unlock()
	if _, exists := s.matches[battleID]; exists {
		return
	}
	sink := newWSSink()
	m := livebattle.NewMatch(livebattle.Config{
		BattleID: battleID, P1Name: p1Name, P2Name: p2Name, Seed: seed,
		Kinds: [2]livebattle.SideKind{livebattle.SideWS, livebattle.SideWS},
		Sink:  sink,
		Deps:  s.liveDeps(battleID, nil),
	})
	s.matches[battleID] = &gwMatch{match: m, sink: sink}
	go m.Run()
}

// startLiveRoom creates a "live" (one human WS + one in-process AI) match
// eagerly at POST time. The AI team is drawn from the curated pool, seeded
// deterministically by the battle's seed so the same battle always faces the
// same opponent. Idempotent.
func (s *Server) startLiveRoom(battleID, p1Name, p2Name string, seed uint64) error {
	s.matchesMu.Lock()
	defer s.matchesMu.Unlock()
	if _, exists := s.matches[battleID]; exists {
		return nil
	}
	aiTeam, err := s.aiTeams.Pick(rand.New(rand.NewSource(int64(seed))))
	if err != nil {
		return err
	}
	sink := newWSSink()
	m := livebattle.NewMatch(livebattle.Config{
		BattleID: battleID, P1Name: p1Name, P2Name: p2Name, Seed: seed,
		Kinds:   [2]livebattle.SideKind{livebattle.SideWS, livebattle.SideAI},
		AITeams: [2][]engine.TeamPick{nil, aiTeam},
		Sink:    sink,
		Deps:    s.liveDeps(battleID, s.newGatewayAI(battleID)),
	})
	s.matches[battleID] = &gwMatch{match: m, sink: sink}
	go m.Run()
	return nil
}

// attachPvPSlot registers a WS handler's slot against an already-running Room.
// Returns (handle, true, nil) on first attach for the slot. If the Room doesn't
// exist (timed out, never created) returns an error. If the slot is already
// attached locally — shouldn't happen given the cache.ClaimSlot guard —
// returns (zero, false, nil).
func (s *Server) attachPvPSlot(battleID string, slot cache.PvPSlot) (gwAttach, bool, error) {
	s.matchesMu.Lock()
	gm, exists := s.matches[battleID]
	s.matchesMu.Unlock()
	if !exists {
		return gwAttach{}, false, errors.New("room not found")
	}
	i := slot.Index()
	p, ok := gm.match.Attach(i)
	if !ok {
		return gwAttach{}, false, nil
	}
	return gwAttach{
		producer:   p,
		frames:     gm.sink.frames(i),
		disconnect: func() { gm.match.Disconnect(i) },
	}, true, nil
}

// detachPvPMatch removes a match from the server registry. Called by the
// coordinator's shutdown hook (livebattle.Deps.OnDone) — never directly from
// the WS handler.
func (s *Server) detachPvPMatch(battleID string) {
	s.matchesMu.Lock()
	delete(s.matches, battleID)
	s.matchesMu.Unlock()
}

// --- gateway AI decider ---

// gatewayAI implements livebattle.AIDecider over the ai-service. It publishes an
// ai.job and awaits the matching ai.decided event (routed through the gateway's
// Hub), falling back to the local heuristic if the service is silent past the
// per-turn budget. Forced-switch (replace) decisions resolve locally — a queue
// round-trip would be pure overhead. One instance per match: the event pump is
// scoped to a single battle.
type gatewayAI struct {
	s        *Server
	battleID string
	budget   time.Duration

	mu      sync.Mutex
	pending map[string]chan messages.AIDecided
}

func (s *Server) newGatewayAI(battleID string) *gatewayAI {
	return &gatewayAI{
		s: s, battleID: battleID, budget: turnDecisionBudget,
		pending: map[string]chan messages.AIDecided{},
	}
}

// Start subscribes to the per-battle event stream and routes EventAIDecided
// fan-outs to the matching Decide call's pending channel by jobID. Runs for the
// lifetime of the match; stopped via ctx.
func (g *gatewayAI) Start(ctx context.Context) {
	subID, events, err := g.s.hub.Subscribe(g.battleID)
	if err != nil {
		log.Printf("ai event pump subscribe %s: %v", g.battleID, err)
		return
	}
	go func() {
		defer g.s.hub.Unsubscribe(g.battleID, subID)
		for {
			select {
			case <-ctx.Done():
				return
			case ev := <-events:
				if ev.Type != messages.EventAIDecided {
					continue
				}
				var d messages.AIDecided
				if err := json.Unmarshal(ev.Body, &d); err != nil {
					continue
				}
				g.mu.Lock()
				ch, ok := g.pending[d.JobID]
				g.mu.Unlock()
				if !ok {
					continue
				}
				select {
				case ch <- d:
				default: // Decide already gave up (timer fired); harmless.
				}
			}
		}
	}()
}

// Decide publishes an AI job and waits up to the budget for the reply, falling
// back to the local heuristic so the turn always resolves.
func (g *gatewayAI) Decide(ctx context.Context, st *engine.BattleState, side int) (engine.Action, string) {
	jobID := uuid.NewString()
	ch := make(chan messages.AIDecided, 1)
	g.mu.Lock()
	g.pending[jobID] = ch
	g.mu.Unlock()
	defer func() {
		g.mu.Lock()
		delete(g.pending, jobID)
		g.mu.Unlock()
	}()

	if err := g.s.broker.PublishJob(ctx, messages.QueueAI, messages.AIJob{
		JobID: jobID, BattleID: g.battleID, Turn: st.Turn, Side: side,
	}); err != nil {
		log.Printf("ai job publish %s side=%d: %v", g.battleID, side, err)
		// Fall through — the deadline below triggers the local fallback.
	}

	timer := time.NewTimer(g.budget)
	defer timer.Stop()

	select {
	case d := <-ch:
		return d.Action, d.Reasoning
	case <-timer.C:
		return g.s.localAIDecision(st, side), ""
	case <-ctx.Done():
		return engine.Action{}, ""
	}
}

// DecideReplace resolves a forced switch locally.
func (g *gatewayAI) DecideReplace(_ context.Context, st *engine.BattleState, side int) engine.Action {
	return g.s.localAIDecision(st, side)
}
