package httpapi

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"pokearena/internal/ai"
	"pokearena/internal/cache"
	"pokearena/internal/engine"
)

// pvpMatch coordinates one live_pvp battle. It owns the authoritative
// BattleState in memory and runs the turn loop; the two WS handlers attach
// their slots and communicate purely via the per-slot channels here.
//
// The match is created lazily by the first WS handler to claim a slot
// (via Server.attachPvPSlot), starts its run goroutine immediately, and
// removes itself from the Server's registry when run exits. There is at
// most one match per battle per gateway instance.
type pvpMatch struct {
	battleID string
	state    *engine.BattleState

	// Per-slot channels (indexed 0=p1, 1=p2).
	//   actions:  WS reader → coordinator (raw, unvalidated actions).
	//   updates:  coordinator → WS writer (state/turn/end/error frames).
	//   attached: closed by attachSlot when its slot is registered.
	actions  [2]chan engine.Action
	updates  [2]chan matchUpdate
	attached [2]chan struct{}

	// once[i] serializes attempts to register slot i. The first call wins;
	// subsequent calls return (zero, false) so the WS handler can report
	// "slot is already attached to its match."
	once [2]sync.Once
	won  [2]bool
}

// matchUpdate is what the coordinator sends to a WS handler. The struct's
// JSON tags define the on-the-wire shape: receivers should switch on Type.
type matchUpdate struct {
	Type    string           `json:"type"` // "state" | "turn" | "end" | "error" | "info"
	View    *ai.View         `json:"view,omitempty"`
	Log     []engine.LogLine `json:"log,omitempty"`
	Winner  *int             `json:"winner,omitempty"`
	Turn    int              `json:"turn,omitempty"`
	Message string           `json:"message,omitempty"`
}

// slotAttach is the handle a WS handler gets after registering its slot.
// Send actions into `actions`; receive frames from `updates`. Closing
// `actions` (the handler does this on disconnect) tells the coordinator
// to abort the match.
type slotAttach struct {
	actions chan<- engine.Action
	updates <-chan matchUpdate
}

func newPvPMatch(battleID string, st *engine.BattleState) *pvpMatch {
	m := &pvpMatch{
		battleID: battleID,
		state:    st,
	}
	for i := 0; i < 2; i++ {
		// actions: capacity 1 so the WS handler isn't blocked by a turn
		// that's still gathering the other side's input. Beyond that the
		// handler does its own backpressure (one outstanding action per
		// slot per turn is the whole protocol).
		m.actions[i] = make(chan engine.Action, 1)
		// updates: 8 frames of slack absorbs the burst at start-of-turn
		// (state → turn-info → turn frame). If a client falls more than
		// that behind, sending blocks and the slow client stalls only
		// itself — the other slot's writer is independent.
		m.updates[i] = make(chan matchUpdate, 8)
		m.attached[i] = make(chan struct{})
	}
	return m
}

// attachSlot registers a WS handler's slot. Returns (handle, true) on
// first call; (zero, false) on any subsequent call for the same slot
// — that case shouldn't happen in practice because cache.ClaimSlot is
// atomic, but defending against it here means a buggy caller can't
// double-attach and corrupt the match.
func (m *pvpMatch) attachSlot(slot cache.PvPSlot) (slotAttach, bool) {
	i := slot.Index()
	m.once[i].Do(func() {
		m.won[i] = true
		close(m.attached[i])
	})
	if !m.won[i] {
		return slotAttach{}, false
	}
	return slotAttach{actions: m.actions[i], updates: m.updates[i]}, true
}

// run is the coordinator's main loop. It waits for both slots to attach,
// broadcasts the initial state, then drives engine.ResolveTurn / Replace
// until the battle ends or a slot disconnects. On any exit it cleans up
// after itself via shutdown.
func (m *pvpMatch) run(s *Server) {
	defer m.shutdown(s)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := m.waitForBothAttached(ctx); err != nil {
		return
	}

	m.broadcast("state", nil)

	for !m.state.Ended() {
		actions, err := m.collectActions(ctx)
		if err != nil {
			return
		}
		turnLog := engine.ResolveTurn(s.dex, m.state, actions)
		m.broadcast("turn", turnLog)

		if m.state.Phase == engine.PhaseReplace {
			sw, err := m.collectReplaceActions(ctx)
			if err != nil {
				return
			}
			replaceLog := engine.ResolveReplace(m.state, sw)
			m.broadcast("turn", replaceLog)
			turnLog = append(turnLog, replaceLog...)
		}

		s.persistLiveTurn(m.state, turnLog)
	}

	// Battle ended naturally — broadcast the result and finalize.
	winner := m.state.Winner
	for i := 0; i < 2; i++ {
		view := ai.MakeView(m.state, i)
		m.send(i, matchUpdate{Type: "end", View: &view, Winner: &winner, Turn: m.state.Turn})
	}
	s.finishLiveBattle(m.state)
	s.deletePvPTokensBest(m.state.ID)
}

// shutdown removes the match from the server registry and closes the
// update channels so the WS writer goroutines exit cleanly. Called
// exactly once via deferred run.
func (m *pvpMatch) shutdown(s *Server) {
	s.detachPvPMatch(m.battleID)
	close(m.updates[0])
	close(m.updates[1])
}

// waitForBothAttached blocks until both slots have registered. A bounded
// timeout caps how long a half-joined match can sit idle, and a closed
// actions channel here means the only attached slot disconnected before
// the opponent ever arrived — abort.
func (m *pvpMatch) waitForBothAttached(ctx context.Context) error {
	const attachDeadline = 5 * time.Minute
	timer := time.NewTimer(attachDeadline)
	defer timer.Stop()

	var attached [2]bool
	for !(attached[0] && attached[1]) {
		select {
		case <-m.attached[0]:
			attached[0] = true
			if !attached[1] {
				m.send(0, matchUpdate{Type: "info", Message: "Waiting for opponent to join…"})
			}
		case <-m.attached[1]:
			attached[1] = true
			if !attached[0] {
				m.send(1, matchUpdate{Type: "info", Message: "Waiting for opponent to join…"})
			}
		case _, ok := <-m.actions[0]:
			if !ok {
				return errors.New("slot p1 disconnected before opponent joined")
			}
			// Action arrived before opponent — silently drop; we'll start
			// fresh once both are in.
		case _, ok := <-m.actions[1]:
			if !ok {
				return errors.New("slot p2 disconnected before opponent joined")
			}
		case <-timer.C:
			return fmt.Errorf("attach timeout: opponent never joined within %s", attachDeadline)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// collectActions gathers one legal action from each side for a choosing
// turn. Illegal actions are reported back to the offending slot and the
// coordinator keeps waiting; a closed actions channel is a disconnect
// and aborts the match.
func (m *pvpMatch) collectActions(ctx context.Context) ([2]engine.Action, error) {
	var actions [2]engine.Action
	var got [2]bool

	for !(got[0] && got[1]) {
		select {
		case act, ok := <-m.actions[0]:
			if !ok {
				return actions, fmt.Errorf("slot p1 disconnected")
			}
			if got[0] {
				m.sendErr(0, "your action for this turn was already submitted")
				continue
			}
			if !isLegalAction(m.state, 0, act) {
				m.sendErr(0, "that action is not legal right now")
				continue
			}
			actions[0], got[0] = act, true
		case act, ok := <-m.actions[1]:
			if !ok {
				return actions, fmt.Errorf("slot p2 disconnected")
			}
			if got[1] {
				m.sendErr(1, "your action for this turn was already submitted")
				continue
			}
			if !isLegalAction(m.state, 1, act) {
				m.sendErr(1, "that action is not legal right now")
				continue
			}
			actions[1], got[1] = act, true
		case <-ctx.Done():
			return actions, ctx.Err()
		}
	}
	return actions, nil
}

// collectReplaceActions gathers forced-switch choices after faints. Only
// sides whose Replace flag is set need to submit; the other side's slot in
// the returned array is nil, which is what engine.ResolveReplace expects.
func (m *pvpMatch) collectReplaceActions(ctx context.Context) ([2]*engine.Action, error) {
	var sw [2]*engine.Action
	needs := m.state.Replace

	for i := 0; i < 2; i++ {
		if needs[i] {
			m.send(i, matchUpdate{Type: "info", Message: "Your Pokémon fainted — choose a replacement."})
		}
	}

	done := func() bool {
		return (!needs[0] || sw[0] != nil) && (!needs[1] || sw[1] != nil)
	}
	for !done() {
		select {
		case act, ok := <-m.actions[0]:
			if !ok {
				return sw, fmt.Errorf("slot p1 disconnected during replace")
			}
			if !needs[0] || sw[0] != nil {
				m.sendErr(0, "not waiting for an action right now")
				continue
			}
			if !isLegalAction(m.state, 0, act) {
				m.sendErr(0, "that action is not legal right now")
				continue
			}
			a := act
			sw[0] = &a
		case act, ok := <-m.actions[1]:
			if !ok {
				return sw, fmt.Errorf("slot p2 disconnected during replace")
			}
			if !needs[1] || sw[1] != nil {
				m.sendErr(1, "not waiting for an action right now")
				continue
			}
			if !isLegalAction(m.state, 1, act) {
				m.sendErr(1, "that action is not legal right now")
				continue
			}
			a := act
			sw[1] = &a
		case <-ctx.Done():
			return sw, ctx.Err()
		}
	}
	return sw, nil
}

// broadcast sends the same logical update to both slots with their
// respective fog-of-war views. Used for "state" and "turn" frames where
// both sides need to learn the new state simultaneously.
func (m *pvpMatch) broadcast(typ string, log []engine.LogLine) {
	for i := 0; i < 2; i++ {
		view := ai.MakeView(m.state, i)
		m.send(i, matchUpdate{
			Type: typ,
			View: &view,
			Log:  log,
			Turn: m.state.Turn,
		})
	}
}

// sendErr is a shortcut for a per-slot error frame.
func (m *pvpMatch) sendErr(i int, msg string) {
	m.send(i, matchUpdate{Type: "error", Message: msg})
}

// send pushes an update to a slot. A slow writer that fills the 8-slot
// buffer blocks here; that's intentional — better to back-pressure one
// side than silently drop state-coherence frames.
func (m *pvpMatch) send(i int, u matchUpdate) {
	m.updates[i] <- u
}

// isLegalAction reports whether act is in the legal set for side. The
// coordinator owns this check because it owns the authoritative state;
// the WS handler shuttles raw actions through without judgment.
func isLegalAction(st *engine.BattleState, side int, act engine.Action) bool {
	for _, legal := range engine.LegalActions(st, side) {
		if legal == act {
			return true
		}
	}
	return false
}

// --- Server-side glue ---

// attachPvPSlot is the WS handler's entry point into the coordinator. It
// creates the match lazily, starts its goroutine, and registers the
// caller's slot. If the slot is already attached (shouldn't happen, but
// defended against), returns (zero, false) and the handler should refuse.
func (s *Server) attachPvPSlot(battleID string, slot cache.PvPSlot, st *engine.BattleState) (slotAttach, bool) {
	s.matchesMu.Lock()
	m, ok := s.matches[battleID]
	if !ok {
		m = newPvPMatch(battleID, st)
		s.matches[battleID] = m
		go m.run(s)
	}
	s.matchesMu.Unlock()
	return m.attachSlot(slot)
}

// detachPvPMatch removes a match from the server registry. Called by the
// coordinator's deferred shutdown — never directly from the WS handler.
func (s *Server) detachPvPMatch(battleID string) {
	s.matchesMu.Lock()
	delete(s.matches, battleID)
	s.matchesMu.Unlock()
}

// deletePvPTokensBest deletes the slot-token hash on a short, independent
// context so end-of-battle cleanup isn't bound to whatever ctx is in
// scope. Errors are swallowed: the worst case is the hash sits there for
// its TTL, which is acceptable.
func (s *Server) deletePvPTokensBest(battleID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = s.cache.DeletePvPTokens(ctx, battleID)
}
