package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"sync"
	"time"

	"github.com/google/uuid"

	"pokearena/internal/ai"
	"pokearena/internal/cache"
	"pokearena/internal/engine"
	"pokearena/internal/messages"
	"pokearena/internal/protocol"
)

// sideKind tags whether a match slot is driven by a remote WebSocket
// client or by an in-process AI driver. From the coordinator's POV
// the two are interchangeable: both produce actions on m.actions[i]
// and (for WS sides) consume frames from m.updates[i].
type sideKind int

const (
	sideWS sideKind = iota
	sideAI
)

// aiSideSpec carries everything the AI driver needs to pick a team
// and decide actions. Populated only for AI sides at match
// construction; the WS side's matching slot is the zero value.
type aiSideSpec struct {
	difficulty string
	team       []engine.TeamPick // pre-picked at room creation, replayable from the seed
}

// turnDecisionBudget is the AI driver's per-turn budget — if the
// ai-service hasn't replied with EventAIDecided by then, the driver
// falls back to the local heuristic so the turn never stalls.
const turnDecisionBudget = 90 * time.Second

// RoomDeadline is the picker-room budget per docs/team-picker-room.md §7.
// A single timer covers everything: abandoned URL, slow picker, idle
// attach. If the room is not ACTIVE by t+RoomDeadline, it dies.
// Set to 10 minutes — long enough for a deliberate human draft against
// an LLM agent that's reasoning out its picks (Claude Code agents
// sometimes take 60-120s to settle on a team).
const RoomDeadline = 10 * time.Minute

// pvpMatch coordinates one live battle from creation through end —
// whether that's "live" (one WS + one AI) or "live_pvp" (two WS). It
// owns the picker-room phase, the authoritative BattleState once
// ACTIVE, and the turn loop. WS handlers attach their slots and talk
// via per-slot channels; AI slots are driven by in-process goroutines
// that write to the same channels.
//
// Lifecycle:
//   POST /api/battles → startLiveRoom (live) or startPvPRoom (live_pvp)
//   creates the match → slots attach (WS) or are pre-attached (AI) →
//   teams submitted → engine state built → turn loop runs to completion.
//   shutdown detaches and closes update channels on any exit.
type pvpMatch struct {
	battleID  string
	createdAt time.Time
	seed      uint64

	// Trainer names captured at creation. Non-strategic; surfaced in
	// FrameRoom so the SPA can label "vs Red" without a separate fetch.
	trainerName [2]string

	// kind[i] tags slot i as a remote WS or an in-process AI driver.
	// aiSpec[i] is populated iff kind[i] == sideAI.
	kind   [2]sideKind
	aiSpec [2]aiSideSpec

	// state is nil while in the OPEN phase (no picks yet). Set by
	// runOpenPhase once both submissions validate. Read after that by
	// every method that uses m.state.
	state *engine.BattleState

	// Per-slot channels (indexed 0=p1, 1=p2).
	//   actions:  WS reader OR AI driver → coordinator (ACTIVE-phase actions).
	//   submits:  WS reader OR AI auto-submit → coordinator (OPEN-phase picks).
	//   updates:  coordinator → WS writer (every server frame). AI slots ignore.
	//   attached: closed when the slot is registered (immediately for AI sides).
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

	// aiPending maps jobID → channel for AI drivers awaiting an
	// EventAIDecided fan-out. Touched by the event pump goroutine
	// and the driver goroutines; guarded by aiPendingMu.
	aiPendingMu sync.Mutex
	aiPending   map[string]chan messages.AIDecided
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

// newMatch builds an empty (OPEN-phase) match. kind[i] selects how
// each slot is driven; aiSpecs[i] is consulted only when kind[i] is
// sideAI. State is filled in later by runOpenPhase once both sides
// have submitted valid teams. AI sides are pre-attached (their
// attached channel is closed and their won flag is set), so the
// open-phase coordinator never has to wait for them to register.
func newMatch(battleID, p1Name, p2Name string, seed uint64, kinds [2]sideKind, aiSpecs [2]aiSideSpec) *pvpMatch {
	m := &pvpMatch{
		battleID:    battleID,
		createdAt:   time.Now(),
		seed:        seed,
		trainerName: [2]string{p1Name, p2Name},
		kind:        kinds,
		aiSpec:      aiSpecs,
		aiPending:   map[string]chan messages.AIDecided{},
	}
	for i := 0; i < 2; i++ {
		// actions/submits: capacity 1 — one outstanding per slot per
		// phase is the whole protocol; further backpressure happens
		// in the WS handler (or in the AI driver).
		m.actions[i] = make(chan engine.Action, 1)
		m.submits[i] = make(chan []engine.TeamPick, 1)
		// updates: 8 frames of slack absorbs the start-of-turn burst
		// (state → info → turn). A slow client stalls only itself; the
		// other slot's writer is independent. AI slots never read this.
		m.updates[i] = make(chan protocol.MatchUpdate, 8)
		m.attached[i] = make(chan struct{})

		if m.kind[i] == sideAI {
			// AI sides are "always attached" — no remote handler will
			// ever call attachSlot for them. Pre-claim so the same
			// once-guard semantics hold.
			m.won[i] = true
			close(m.attached[i])
		}
	}
	return m
}

// newPvPMatch builds a two-WS match (the original live_pvp shape).
// Thin wrapper around newMatch — kept for clarity at call sites.
func newPvPMatch(battleID, p1Name, p2Name string, seed uint64) *pvpMatch {
	return newMatch(battleID, p1Name, p2Name, seed, [2]sideKind{sideWS, sideWS}, [2]aiSideSpec{})
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
// turn loop until the battle ends, then cleans up via the deferred
// shutdown. If any side is AI, an event-pump goroutine subscribes to
// the hub for the lifetime of the match so AI drivers can correlate
// EventAIDecided fan-outs to outstanding jobs.
func (m *pvpMatch) run(s *Server) {
	defer m.shutdown(s)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if m.hasAISide() {
		m.startEventPump(ctx, s)
	}

	if err := m.runOpenPhase(ctx, s); err != nil {
		// Surface the cause to whoever's still listening, then exit.
		// Room dies; battle row is left in its "open" status so an
		// operator can see it was never started.
		msg := "room ended: " + err.Error()
		if m.kind[0] == sideWS && m.won[0] {
			m.send(0, protocol.MatchUpdate{Type: protocol.FrameError, Message: msg})
		}
		if m.kind[1] == sideWS && m.won[1] {
			m.send(1, protocol.MatchUpdate{Type: protocol.FrameError, Message: msg})
		}
		return
	}

	// State is now populated; announce battle-started for spectators
	// (parity with quicksim's event sequence), broadcast the initial
	// fog-of-war view to the WS slots, and enter the turn loop.
	bg, cancelBG := context.WithTimeout(context.Background(), 5*time.Second)
	_ = s.broker.PublishEvent(bg, messages.EventBattleStarted, m.battleID, messages.BattleStarted{BattleID: m.battleID})
	cancelBG()

	m.broadcast(protocol.FrameState, nil)

	for !m.state.Ended() {
		// AI sides need a per-turn driver: it publishes the job to
		// the ai-service and writes the chosen action to m.actions[i],
		// making it indistinguishable from a WS slot at the select below.
		if m.kind[0] == sideAI {
			go m.driveAITurn(ctx, s, 0)
		}
		if m.kind[1] == sideAI {
			go m.driveAITurn(ctx, s, 1)
		}
		actions, err := m.collectActions(ctx)
		if err != nil {
			return
		}
		turnLog := engine.ResolveTurn(s.dex, m.state, actions)
		m.broadcast(protocol.FrameTurn, turnLog)

		if m.state.Phase == engine.PhaseReplace {
			// Replace decisions are cheap; AI sides resolve locally
			// (no queue round-trip) and push to m.actions[i].
			if m.kind[0] == sideAI && m.state.Replace[0] {
				go m.driveAIReplace(s, 0)
			}
			if m.kind[1] == sideAI && m.state.Replace[1] {
				go m.driveAIReplace(s, 1)
			}
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
	m.sendEnd(0, &winner)
	m.sendEnd(1, &winner)
	s.finishLiveBattle(m.state)
	s.deletePvPTokensBest(m.battleID)
}

func (m *pvpMatch) sendEnd(side int, winner *int) {
	if m.kind[side] == sideAI {
		return
	}
	view := ai.MakeView(m.state, side)
	m.send(side, protocol.MatchUpdate{Type: protocol.FrameEnd, View: &view, Winner: winner, Turn: m.state.Turn})
}

// hasAISide reports whether any slot is driven by the in-process AI.
// Used to gate the hub subscription (no AI → no AIDecided to consume).
func (m *pvpMatch) hasAISide() bool {
	return m.kind[0] == sideAI || m.kind[1] == sideAI
}

// runOpenPhase waits for both slots to attach AND both to submit a
// valid team, all within RoomDeadline. AI sides are pre-attached at
// construction and auto-submit their pre-picked team immediately; the
// WS sides arrive over the wire. On success, it builds the engine
// state via NewBattleFromPicks, persists it to Redis, advances the
// battle row to "running", and returns nil — m.state is then live.
//
// Returns an error on disconnect, deadline, or engine-init failure.
func (m *pvpMatch) runOpenPhase(ctx context.Context, s *Server) error {
	deadline := time.NewTimer(time.Until(m.createdAt.Add(RoomDeadline)))
	defer deadline.Stop()

	// AI sides drop their pre-picked team into the submits channel
	// immediately. The select loop below picks it up via the normal
	// submission path, so WS and AI sides follow the same code.
	if m.kind[0] == sideAI {
		m.submits[0] <- m.aiSpec[0].team
	}
	if m.kind[1] == sideAI {
		m.submits[1] <- m.aiSpec[1].team
	}

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

// broadcast sends the same logical update to both WS slots with their
// respective fog-of-war views. AI slots are skipped — the AI driver
// reads state directly from m.state when deciding, so per-frame views
// would be allocated and discarded.
func (m *pvpMatch) broadcast(typ string, log []engine.LogLine) {
	m.broadcastOne(0, typ, log)
	m.broadcastOne(1, typ, log)
}

func (m *pvpMatch) broadcastOne(side int, typ string, log []engine.LogLine) {
	if m.kind[side] == sideAI {
		return
	}
	view := ai.MakeView(m.state, side)
	m.send(side, protocol.MatchUpdate{Type: typ, View: &view, Log: log, Turn: m.state.Turn})
}

// sendErr is a shortcut for a per-slot error frame.
func (m *pvpMatch) sendErr(i int, msg string) {
	m.send(i, protocol.MatchUpdate{Type: protocol.FrameError, Message: msg})
}

// send pushes an update to a slot. A slow writer that fills the 8-slot
// buffer blocks here; that's intentional — better to back-pressure one
// side than silently drop state-coherence frames.
//
// AI slots have no WS writer reading m.updates[i]; sending into that
// channel would fill the buffer and then deadlock the coordinator at
// the start of turn 9. Skip them — the AI driver gets the state it
// needs from m.state at action-request time, not from frames.
func (m *pvpMatch) send(i int, u protocol.MatchUpdate) {
	if m.kind[i] == sideAI {
		return
	}
	m.updates[i] <- u
}

// driveAITurn is one turn's worth of AI work for slot i. It publishes
// an AI job to the ai-service, waits up to turnDecisionBudget for the
// matching EventAIDecided to arrive (routed through the match's event
// pump), and falls back to the local heuristic if the service is
// silent. The chosen action goes onto m.actions[i] so the coordinator's
// collectActions select treats the AI side exactly like a WS slot.
//
// Started once per turn by run(); exits when it has produced an
// action (or when ctx is cancelled because the match is shutting down).
func (m *pvpMatch) driveAITurn(ctx context.Context, s *Server, side int) {
	jobID := uuid.NewString()
	ch := make(chan messages.AIDecided, 1)
	m.registerAIPending(jobID, ch)
	defer m.unregisterAIPending(jobID)

	if err := s.broker.PublishJob(ctx, messages.QueueAI, messages.AIJob{
		JobID: jobID, BattleID: m.battleID, Turn: m.state.Turn,
		Side: side, Difficulty: m.aiSpec[side].difficulty,
	}); err != nil {
		log.Printf("ai job publish %s side=%d: %v", m.battleID, side, err)
		// Fall through — the deadline below will trigger the local
		// fallback so the turn still resolves.
	}

	timer := time.NewTimer(turnDecisionBudget)
	defer timer.Stop()

	var act engine.Action
	select {
	case d := <-ch:
		act = d.Action
		if d.Reasoning != "" {
			// Surface AI reasoning to any spectator/opponent WS — same
			// shape the old live-mode loop emitted.
			m.broadcastInfo("ai", d.Reasoning)
		}
	case <-timer.C:
		act = s.localAIDecision(m.state, side)
	case <-ctx.Done():
		return
	}

	select {
	case m.actions[side] <- act:
	case <-ctx.Done():
	}
}

// driveAIReplace produces an AI side's forced-switch choice locally.
// The replace decision is shallow enough that a queue round-trip would
// be pure overhead; the fallback heuristic was already what live-mode
// used for replaces before this refactor.
func (m *pvpMatch) driveAIReplace(s *Server, side int) {
	act := s.localAIDecision(m.state, side)
	m.actions[side] <- act
}

// broadcastInfo emits an "ai" reasoning frame to every WS slot — the
// other player and any spectator subscribed at this layer see the
// AI's thinking just as they did with the pre-refactor live mode.
func (m *pvpMatch) broadcastInfo(kind, message string) {
	if m.kind[0] == sideWS {
		m.send(0, protocol.MatchUpdate{Type: kind, Message: message})
	}
	if m.kind[1] == sideWS {
		m.send(1, protocol.MatchUpdate{Type: kind, Message: message})
	}
}

// startEventPump subscribes to the per-battle event stream and routes
// EventAIDecided fan-outs to the matching driver's pending channel by
// jobID. Runs for the lifetime of the match; stopped via ctx.
//
// Only called when at least one slot is AI — otherwise there's nothing
// to consume and the subscription would be pure waste.
func (m *pvpMatch) startEventPump(ctx context.Context, s *Server) {
	subID, events, err := s.hub.Subscribe(m.battleID)
	if err != nil {
		log.Printf("ai event pump subscribe %s: %v", m.battleID, err)
		return
	}
	go func() {
		defer s.hub.Unsubscribe(m.battleID, subID)
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
				m.aiPendingMu.Lock()
				ch, ok := m.aiPending[d.JobID]
				m.aiPendingMu.Unlock()
				if !ok {
					continue
				}
				select {
				case ch <- d:
				default:
					// Driver already gave up (timer fired); harmless.
				}
			}
		}
	}()
}

func (m *pvpMatch) registerAIPending(jobID string, ch chan messages.AIDecided) {
	m.aiPendingMu.Lock()
	m.aiPending[jobID] = ch
	m.aiPendingMu.Unlock()
}

func (m *pvpMatch) unregisterAIPending(jobID string) {
	m.aiPendingMu.Lock()
	delete(m.aiPending, jobID)
	m.aiPendingMu.Unlock()
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

// startPvPRoom creates a two-WS Room eagerly at POST time. The picker
// deadline starts ticking immediately so an unclaimed URL can't sit in
// memory past its budget. Idempotent: if a Room for battleID already
// exists, this is a no-op.
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

// startLiveRoom creates a "live" (one human WS + one in-process AI)
// match eagerly at POST time. The AI team is drawn here from the
// curated pool, seeded deterministically by the battle's seed — the
// same seed always produces the same opponent team, so battles stay
// replayable. Returns an error if no AI team is available for the
// requested difficulty; the caller is responsible for surfacing it.
//
// Idempotent: if a Room for battleID already exists, this is a no-op.
func (s *Server) startLiveRoom(battleID, p1Name, p2Name string, seed uint64, difficulty string) error {
	s.matchesMu.Lock()
	defer s.matchesMu.Unlock()
	if _, exists := s.matches[battleID]; exists {
		return nil
	}
	aiTeam, err := s.aiTeams.Pick(difficulty, rand.New(rand.NewSource(int64(seed))))
	if err != nil {
		return fmt.Errorf("no AI team for difficulty %q: %w", difficulty, err)
	}
	m := newMatch(battleID, p1Name, p2Name, seed,
		[2]sideKind{sideWS, sideAI},
		[2]aiSideSpec{{}, {difficulty: difficulty, team: aiTeam}},
	)
	s.matches[battleID] = m
	go m.run(s)
	return nil
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
