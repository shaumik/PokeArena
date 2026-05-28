package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"pokearena/internal/ai"
	"pokearena/internal/cache"
	"pokearena/internal/engine"
	"pokearena/internal/protocol"
)

// RoomDeadline is the picker-room budget per docs/team-picker-room.md §7.
// A single timer covers everything: abandoned URL, slow picker, idle
// attach. If the room is not ACTIVE by t+RoomDeadline, it dies.
const RoomDeadline = 300 * time.Second

// pvpMatch coordinates one live_pvp battle from creation through end. It
// owns the picker-room phase (collecting valid team submissions), the
// authoritative BattleState once ACTIVE, and the turn loop. The two WS
// handlers attach their slots and communicate purely via the per-slot
// channels here.
//
// Lifecycle:
//   POST /api/battles for live_pvp → startPvPRoom creates the match
//   (eagerly, so the 300s deadline starts at POST per the doc) →
//   slots attach via attachPvPSlot → both submit_team → engine state
//   built and the existing turn loop runs to completion. shutdown
//   detaches and closes update channels on any exit.
type pvpMatch struct {
	battleID  string
	createdAt time.Time
	seed      uint64

	// Trainer names captured at creation. Non-strategic; surfaced in
	// FrameRoom so the SPA can label "vs Red" without a separate fetch.
	trainerName [2]string

	// state is nil while in the OPEN phase (no picks yet). Set by
	// runOpenPhase once both submissions validate. Read after that by
	// every method that uses m.state.
	state *engine.BattleState

	// Per-slot channels (indexed 0=p1, 1=p2).
	//   actions:  WS reader → coordinator, ACTIVE-phase actions only.
	//   submits:  WS reader → coordinator, OPEN-phase team picks.
	//   updates:  coordinator → WS writer (every server frame).
	//   attached: closed by attachSlot when its slot is registered.
	actions  [2]chan engine.Action
	submits  [2]chan []engine.TeamPick
	updates  [2]chan protocol.MatchUpdate
	attached [2]chan struct{}

	// once[i] serializes slot-registration attempts; the first call
	// wins, subsequent ones return (zero, false). cache.ClaimSlot is
	// the network-side guarantee against double-attach; this is the
	// in-process defense.
	once [2]sync.Once
	won  [2]bool

	// Submitted picks per slot, written exclusively by the Room
	// goroutine after validating against engine.ValidateTeam.
	submitted [2][]engine.TeamPick
}

// slotAttach is the handle a WS handler gets after registering its
// slot. Send action / submit_team messages on their respective channels;
// receive frames from updates. Closing either client-side channel (the
// handler does this on disconnect) tells the coordinator the slot is gone.
type slotAttach struct {
	actions chan<- engine.Action
	submits chan<- []engine.TeamPick
	updates <-chan protocol.MatchUpdate
}

// newPvPMatch builds an empty (OPEN-phase) match. State is filled in
// later by runOpenPhase once both sides have submitted valid teams.
func newPvPMatch(battleID, p1Name, p2Name string, seed uint64) *pvpMatch {
	m := &pvpMatch{
		battleID:    battleID,
		createdAt:   time.Now(),
		seed:        seed,
		trainerName: [2]string{p1Name, p2Name},
	}
	for i := 0; i < 2; i++ {
		// actions/submits: capacity 1 — one outstanding per slot per
		// phase is the whole protocol; further backpressure happens
		// in the WS handler.
		m.actions[i] = make(chan engine.Action, 1)
		m.submits[i] = make(chan []engine.TeamPick, 1)
		// updates: 8 frames of slack absorbs the start-of-turn burst
		// (state → info → turn). A slow client stalls only itself; the
		// other slot's writer is independent.
		m.updates[i] = make(chan protocol.MatchUpdate, 8)
		m.attached[i] = make(chan struct{})
	}
	return m
}

// attachSlot registers a WS handler's slot. Returns (handle, true) on
// first call; (zero, false) on any subsequent call for the same slot.
// Two callers winning here would corrupt the coordinator — cache.ClaimSlot
// gates this at the network edge; this is the local belt.
func (m *pvpMatch) attachSlot(slot cache.PvPSlot) (slotAttach, bool) {
	i := slot.Index()
	m.once[i].Do(func() {
		m.won[i] = true
		close(m.attached[i])
	})
	if !m.won[i] {
		return slotAttach{}, false
	}
	return slotAttach{actions: m.actions[i], submits: m.submits[i], updates: m.updates[i]}, true
}

// run is the coordinator's main loop. It drives the picker phase to a
// successful close (engine state built, transition to ACTIVE), runs the
// existing turn loop until the battle ends, then cleans up via the
// deferred shutdown.
func (m *pvpMatch) run(s *Server) {
	defer m.shutdown(s)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := m.runOpenPhase(ctx, s); err != nil {
		// Surface the cause to whoever's still listening, then exit.
		// Room dies; battle row is left in its "open" status so an
		// operator can see it was never started.
		for i := 0; i < 2; i++ {
			if m.won[i] {
				m.send(i, protocol.MatchUpdate{Type: protocol.FrameError, Message: "room ended: " + err.Error()})
			}
		}
		return
	}

	// State is now populated; broadcast the initial fog-of-war view
	// and enter the existing turn loop unchanged.
	m.broadcast(protocol.FrameState, nil)

	for !m.state.Ended() {
		actions, err := m.collectActions(ctx)
		if err != nil {
			return
		}
		turnLog := engine.ResolveTurn(s.dex, m.state, actions)
		m.broadcast(protocol.FrameTurn, turnLog)

		if m.state.Phase == engine.PhaseReplace {
			sw, err := m.collectReplaceActions(ctx)
			if err != nil {
				return
			}
			replaceLog := engine.ResolveReplace(m.state, sw)
			m.broadcast(protocol.FrameTurn, replaceLog)
			turnLog = append(turnLog, replaceLog...)
		}

		s.persistLiveTurn(m.state, turnLog)
	}

	winner := m.state.Winner
	for i := 0; i < 2; i++ {
		view := ai.MakeView(m.state, i)
		m.send(i, protocol.MatchUpdate{Type: protocol.FrameEnd, View: &view, Winner: &winner, Turn: m.state.Turn})
	}
	s.finishLiveBattle(m.state)
	s.deletePvPTokensBest(m.battleID)
}

// runOpenPhase waits for both slots to attach AND both to submit a
// valid team, all within RoomDeadline. On success, it builds the
// engine state via NewBattleFromPicks, persists it to Redis, advances
// the battle row to "running", and returns nil — m.state is then live.
//
// Returns an error on disconnect, deadline, or engine-init failure.
func (m *pvpMatch) runOpenPhase(ctx context.Context, s *Server) error {
	deadline := time.NewTimer(time.Until(m.createdAt.Add(RoomDeadline)))
	defer deadline.Stop()

	// Local attachment tracker, owned by this goroutine. The channel
	// alias trick (set to nil after firing) prevents a closed channel
	// from spinning the select.
	var attached [2]bool
	a0, a1 := m.attached[0], m.attached[1]

	m.broadcastRoom(protocol.RoomPhaseOpen, attached)

	for m.submitted[0] == nil || m.submitted[1] == nil {
		select {
		case <-a0:
			a0 = nil
			attached[0] = true
			m.broadcastRoom(protocol.RoomPhaseOpen, attached)
		case <-a1:
			a1 = nil
			attached[1] = true
			m.broadcastRoom(protocol.RoomPhaseOpen, attached)
		case picks, ok := <-m.submits[0]:
			if !ok {
				return errors.New("slot p1 disconnected before submitting a team")
			}
			if err := m.acceptSubmission(0, picks, s); err != nil {
				m.sendErr(0, err.Error())
				continue
			}
			m.broadcastRoom(protocol.RoomPhaseOpen, attached)
		case picks, ok := <-m.submits[1]:
			if !ok {
				return errors.New("slot p2 disconnected before submitting a team")
			}
			if err := m.acceptSubmission(1, picks, s); err != nil {
				m.sendErr(1, err.Error())
				continue
			}
			m.broadcastRoom(protocol.RoomPhaseOpen, attached)
		case _, ok := <-m.actions[0]:
			if !ok {
				return errors.New("slot p1 disconnected before submitting a team")
			}
			m.sendErr(0, "submit a team before sending actions")
		case _, ok := <-m.actions[1]:
			if !ok {
				return errors.New("slot p2 disconnected before submitting a team")
			}
			m.sendErr(1, "submit a team before sending actions")
		case <-deadline.C:
			return fmt.Errorf("room expired after %s — both sides did not submit in time", RoomDeadline)
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	st, err := engine.NewBattleFromPicks(s.dex, m.battleID,
		m.trainerName[0], m.submitted[0],
		m.trainerName[1], m.submitted[1],
		m.seed)
	if err != nil {
		return fmt.Errorf("engine init: %w", err)
	}
	m.state = st

	// Persist initial state + flip the row to "running" before play
	// begins so a gateway crash mid-turn doesn't strand a battle in
	// "open" forever. Errors are non-fatal — the in-memory state is
	// still the source of truth for the duration of the WS lifetimes.
	bg, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.cache.SaveState(bg, st); err != nil {
		log.Printf("pvp open: cache.SaveState %s: %v", m.battleID, err)
	}
	if err := s.store.SetBattleStatus(bg, m.battleID, "running"); err != nil {
		log.Printf("pvp open: set status running %s: %v", m.battleID, err)
	}

	m.broadcastRoom(protocol.RoomPhaseStarting, attached)
	return nil
}

// acceptSubmission validates the picks against ValidateTeam and stores
// them. Idempotent rejection of re-submission keeps the contract
// honest: once submitted, locked.
func (m *pvpMatch) acceptSubmission(side int, picks []engine.TeamPick, s *Server) error {
	if m.submitted[side] != nil {
		return errors.New("team already submitted — picks are locked")
	}
	if err := engine.ValidateTeam(picks, s.dex); err != nil {
		return fmt.Errorf("invalid team: %s", err)
	}
	m.submitted[side] = picks
	return nil
}

// broadcastRoom sends a per-slot FrameRoom view of the current OPEN
// state. "You" is the receiving slot; "them" is the other. The
// remaining-deadline value resyncs the client's countdown on every
// state change.
func (m *pvpMatch) broadcastRoom(phase protocol.RoomPhase, attached [2]bool) {
	remaining := time.Until(m.createdAt.Add(RoomDeadline))
	if remaining < 0 {
		remaining = 0
	}
	for i := 0; i < 2; i++ {
		you := protocol.RoomSlot{
			Attached:  attached[i],
			Submitted: m.submitted[i] != nil,
			Trainer:   m.trainerName[i],
		}
		them := protocol.RoomSlot{
			Attached:  attached[1-i],
			Submitted: m.submitted[1-i] != nil,
			Trainer:   m.trainerName[1-i],
		}
		m.send(i, protocol.MatchUpdate{
			Type: protocol.FrameRoom,
			Room: &protocol.RoomUpdate{
				Phase:      phase,
				You:        you,
				Them:       them,
				DeadlineMS: remaining.Milliseconds(),
			},
		})
	}
}

// shutdown removes the match from the server registry and closes the
// update channels so WS writer goroutines exit cleanly. Called exactly
// once via deferred run.
func (m *pvpMatch) shutdown(s *Server) {
	s.detachPvPMatch(m.battleID)
	close(m.updates[0])
	close(m.updates[1])
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
// sides whose Replace flag is set need to submit; the other side's slot
// in the returned array is nil, which is what engine.ResolveReplace
// expects.
func (m *pvpMatch) collectReplaceActions(ctx context.Context) ([2]*engine.Action, error) {
	var sw [2]*engine.Action
	needs := m.state.Replace

	for i := 0; i < 2; i++ {
		if needs[i] {
			m.send(i, protocol.MatchUpdate{Type: protocol.FrameInfo, Message: "Your Pokémon fainted — choose a replacement."})
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
		m.send(i, protocol.MatchUpdate{
			Type: typ,
			View: &view,
			Log:  log,
			Turn: m.state.Turn,
		})
	}
}

// sendErr is a shortcut for a per-slot error frame.
func (m *pvpMatch) sendErr(i int, msg string) {
	m.send(i, protocol.MatchUpdate{Type: protocol.FrameError, Message: msg})
}

// send pushes an update to a slot. A slow writer that fills the 8-slot
// buffer blocks here; that's intentional — better to back-pressure one
// side than silently drop state-coherence frames.
func (m *pvpMatch) send(i int, u protocol.MatchUpdate) {
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

// startPvPRoom creates a Room eagerly at POST time. The 300s deadline
// starts ticking immediately so an unclaimed URL can't sit in memory
// past its budget. Idempotent: if a Room for battleID already exists,
// this is a no-op.
func (s *Server) startPvPRoom(battleID, p1Name, p2Name string, seed uint64) {
	s.matchesMu.Lock()
	defer s.matchesMu.Unlock()
	if _, exists := s.matches[battleID]; exists {
		return
	}
	m := newPvPMatch(battleID, p1Name, p2Name, seed)
	s.matches[battleID] = m
	go m.run(s)
}

// attachPvPSlot registers a WS handler's slot against an already-running
// Room. Returns (handle, true, nil) on first attach for the slot. If the
// Room doesn't exist (timed out, never created), returns an error. If
// the slot is already attached locally — shouldn't happen given the
// cache.ClaimSlot guard — returns (zero, false, nil).
func (s *Server) attachPvPSlot(battleID string, slot cache.PvPSlot) (slotAttach, bool, error) {
	s.matchesMu.Lock()
	m, exists := s.matches[battleID]
	s.matchesMu.Unlock()
	if !exists {
		return slotAttach{}, false, errors.New("room not found")
	}
	a, ok := m.attachSlot(slot)
	return a, ok, nil
}

// detachPvPMatch removes a match from the server registry. Called by
// the coordinator's deferred shutdown — never directly from the WS
// handler.
func (s *Server) detachPvPMatch(battleID string) {
	s.matchesMu.Lock()
	delete(s.matches, battleID)
	s.matchesMu.Unlock()
}

// deletePvPTokensBest deletes the slot-token hash on a short,
// independent context so end-of-battle cleanup isn't bound to whatever
// ctx is in scope. Errors are swallowed: the worst case is the hash
// sits there for its TTL, which is acceptable.
func (s *Server) deletePvPTokensBest(battleID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = s.cache.DeletePvPTokens(ctx, battleID)
}
