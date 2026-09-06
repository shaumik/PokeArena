package livebattle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/shaumik/PokeArena/internal/ai"
	"github.com/shaumik/PokeArena/internal/engine"
	"github.com/shaumik/PokeArena/internal/messages"
	"github.com/shaumik/PokeArena/internal/protocol"
)

// Run drives the match from the picker phase through a successful close (engine
// state built, transition to ACTIVE), runs the turn loop until the battle ends,
// then cleans up via the deferred shutdown. Blocks until the match finishes; the
// host runs it in its own goroutine. The turn loop is bound to parent: when the
// host cancels it (lost ownership lease, or shutdown) Run returns ReasonYielded
// promptly instead of blocking on its inbound channels. The returned Reason tells
// the host how to clean up — see the Reason docs.
func (m *Match) Run(parent context.Context) Reason {
	defer m.shutdown()

	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	if m.hasAISide() && m.deps.AI != nil {
		m.deps.AI.Start(ctx)
	}

	if !m.resumed {
		if err := m.runOpenPhase(ctx); err != nil {
			// Surface the cause to whoever's still listening, then exit. The host's
			// cleanup marks the row "abandoned" (it never reached "running"), so the
			// failover scan won't try to reclaim a room that no one is in.
			return m.exitOpenPhase(parent, err)
		}

		// State is now populated; announce battle-started for spectators
		// (parity with quicksim's event sequence).
		bg, cancelBG := context.WithTimeout(context.Background(), 5*time.Second)
		m.deps.Publish(bg, messages.EventBattleStarted, m.battleID, messages.BattleStarted{BattleID: m.battleID})
		cancelBG()
	}

	// Broadcast the current fog-of-war view to the WS slots — the initial frame
	// for a fresh battle, or a resync frame for a resumed one — then enter the
	// turn loop.
	m.broadcast(protocol.FrameState, nil)

	for !m.state.Ended() {
		// AI sides need a per-turn driver: it asks the AIDecider for a choice
		// and writes it to m.actions[i], making it indistinguishable from a WS
		// slot at the collectActions select below.
		if m.kind[0] == SideAI {
			go m.driveAITurn(ctx, 0)
		}
		if m.kind[1] == SideAI {
			go m.driveAITurn(ctx, 1)
		}
		actions, err := m.collectActions(ctx)
		if err != nil {
			return m.exitTurnLoop(parent, err)
		}
		turnLog := engine.ResolveTurn(m.deps.Dex, m.state, actions)
		m.turn.Store(int64(m.state.Turn))
		m.broadcast(protocol.FrameTurn, turnLog)

		// A loop, not an if: ResolveReplace can leave the battle *still* in
		// PhaseReplace when a replacement dies to entry hazards on the way in
		// and the side has more Pokémon to send. Falling through to the top of
		// the turn loop in that state asks both sides for a choosing action
		// while the engine is still resolving switches — which restricts the
		// healthy side to switches it never owed, and hands an agent an empty
		// legal-action set when the bench is gone.
		for m.state.Phase == engine.PhaseReplace {
			if m.kind[0] == SideAI && m.state.Replace[0] {
				go m.driveAIReplace(ctx, 0)
			}
			if m.kind[1] == SideAI && m.state.Replace[1] {
				go m.driveAIReplace(ctx, 1)
			}
			sw, err := m.collectReplaceActions(ctx)
			if err != nil {
				return m.exitTurnLoop(parent, err)
			}
			replaceLog := engine.ResolveReplace(m.state, sw)
			m.broadcast(protocol.FrameTurn, replaceLog)
			turnLog = append(turnLog, replaceLog...)
		}

		m.persistTurn(m.state, turnLog)

		// The first post-takeover turn has now fully resolved — both the choosing
		// and any forced-switch phase ran at the fast resume cadence. Drop to the
		// slow steady-state cadence so subsequent turns never re-prompt a player
		// who is simply thinking.
		m.awaitingResume = false
	}

	winner := m.state.Winner
	m.sendEnd(0, &winner)
	m.sendEnd(1, &winner)
	m.finishBattle(m.state)
	m.deleteTokensBest()
	return ReasonCompleted
}

// errRoomExpired is the sentinel runOpenPhase returns when the picker deadline
// lapses before both sides submit, so Run can classify the exit as a deadline
// (vs a disconnect) without string-matching.
var errRoomExpired = errors.New("room expired before both sides submitted")

// errTurnTimeout is the sentinel the per-turn deadline returns when a slot goes
// silent without ever sending a disconnect — typically a crashed gateway. Run
// classifies it like a disconnect (the battle is abandoned), but reports it to
// the survivor neutrally: on a timeout the silence can't be attributed to one
// side, so "your opponent disconnected" would be a guess.
var errTurnTimeout = errors.New("turn deadline expired waiting for an action")

// classifyExit maps a turn-loop/open-phase error to a shutdown Reason. A
// canceled parent always means the host pulled the plug (lost lease / shutdown)
// — that takes precedence over whatever inbound-channel error raced with it.
func classifyExit(parent context.Context, err error) Reason {
	if parent.Err() != nil {
		return ReasonYielded
	}
	if errors.Is(err, errRoomExpired) {
		return ReasonDeadlineExpired
	}
	return ReasonDisconnected
}

// exitTurnLoop classifies a turn-loop error and, when it means a slot's feeder
// went away (not a host-driven yield), tells the surviving slot the battle is
// over before returning. Without that the survivor's client would hang with no
// terminal frame, since the loop returns straight to the host's cleanup. The
// wording differs by cause: a clean disconnect names the opponent leaving; a
// turn-deadline timeout is reported neutrally, since the silence can't be pinned
// on one side.
func (m *Match) exitTurnLoop(parent context.Context, err error) Reason {
	reason := classifyExit(parent, err)
	if reason != ReasonDisconnected {
		return reason
	}
	msg := "Your opponent disconnected — the battle was abandoned."
	if errors.Is(err, errTurnTimeout) {
		msg = "The battle timed out waiting for a move and was abandoned."
	}
	m.notifyBattleAbandoned(msg)
	return reason
}

// exitOpenPhase classifies a picker-room failure and, unless the host pulled the
// plug (yield → another owner takes over), tells every attached, still-connected
// WS slot the room is over. Without the terminal frame the survivor of an
// expired or half-empty room would sit on the picker screen forever — the same
// gap exitTurnLoop closes for in-play abandonment.
func (m *Match) exitOpenPhase(parent context.Context, err error) Reason {
	reason := classifyExit(parent, err)
	if reason == ReasonYielded {
		return reason
	}
	m.notifyBattleAbandoned("room ended: " + err.Error())
	return reason
}

// notifyBattleAbandoned sends every attached, still-connected WS slot a terminal
// frame because the battle was abandoned — before it started (a dead picker
// room) or mid-play. It is recorded "abandoned", not won, so we send no winner —
// just the explanatory message plus an end frame so the client leaves the battle
// view rather than waiting forever. Slots that never attached (won false) or have
// already disconnected (closed) are skipped: there is no live client to tell.
func (m *Match) notifyBattleAbandoned(msg string) {
	for i := 0; i < 2; i++ {
		if m.kind[i] != SideWS || !m.won[i] || m.slotClosed(i) {
			continue
		}
		m.sendErr(i, msg)
		m.sendEnd(i, nil)
	}
}

// slotClosed reports whether slot i's feeder has signaled disconnect, without
// blocking. Safe from the coordinator goroutine: closed[i] is only ever closed.
func (m *Match) slotClosed(i int) bool {
	select {
	case <-m.closed[i]:
		return true
	default:
		return false
	}
}

// hasAISide reports whether any slot is driven by the in-process AI.
func (m *Match) hasAISide() bool {
	return m.kind[0] == SideAI || m.kind[1] == SideAI
}

// shutdown closes done (so blocked producers exit) and the sink (so frame
// drainers exit), then runs the host cleanup hook. Called exactly once via the
// deferred Run.
func (m *Match) shutdown() {
	close(m.done)
	if m.deps.OnDone != nil {
		m.deps.OnDone(m.battleID)
	}
	m.sink.Close()
	m.conns.stopAll()
}

// runOpenPhase waits for both slots to attach AND both to submit a valid team,
// all within the room deadline. AI sides are pre-attached and auto-submit their
// pre-picked team immediately; WS sides arrive over the wire. On success it
// builds the engine state, persists it, advances the battle row to "running",
// and returns nil — m.state is then live.
func (m *Match) runOpenPhase(ctx context.Context) error {
	deadline := time.NewTimer(time.Until(m.createdAt.Add(m.roomDeadline)))
	defer deadline.Stop()

	// AI sides drop their pre-picked team into the submits channel immediately;
	// the select loop picks it up via the normal submission path so WS and AI
	// sides follow identical code.
	if m.kind[0] == SideAI {
		m.submits[0] <- m.aiTeam[0]
	}
	if m.kind[1] == SideAI {
		m.submits[1] <- m.aiTeam[1]
	}

	// Local attachment tracker, owned by this goroutine. Aliasing each attach
	// channel to nil after it fires prevents a closed channel from spinning the
	// select.
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
		case picks := <-m.submits[0]:
			if err := m.acceptSubmission(0, picks); err != nil {
				m.sendErr(0, err.Error())
				continue
			}
			m.broadcastRoom(protocol.RoomPhaseOpen, attached)
		case picks := <-m.submits[1]:
			if err := m.acceptSubmission(1, picks); err != nil {
				m.sendErr(1, err.Error())
				continue
			}
			m.broadcastRoom(protocol.RoomPhaseOpen, attached)
		case <-m.actions[0]:
			m.sendErr(0, "submit a team before sending actions")
		case <-m.actions[1]:
			m.sendErr(1, "submit a team before sending actions")
		case <-m.closed[0]:
			return errors.New("slot p1 disconnected before submitting a team")
		case <-m.closed[1]:
			return errors.New("slot p2 disconnected before submitting a team")
		case <-deadline.C:
			return fmt.Errorf("room expired after %s — both sides did not submit in time: %w", m.roomDeadline, errRoomExpired)
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	st, err := engine.NewBattleFromPicks(m.deps.Dex, m.battleID,
		m.trainerName[0], m.submitted[0],
		m.trainerName[1], m.submitted[1],
		m.seed)
	if err != nil {
		return fmt.Errorf("engine init: %w", err)
	}
	m.state = st
	m.turn.Store(int64(st.Turn))

	// Persist initial state + flip the row to "running" before play begins so a
	// crash mid-turn doesn't strand a battle in "open" forever. Errors are
	// non-fatal — the in-memory state is the source of truth for the duration.
	bg, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.deps.Cache.SaveState(bg, st); err != nil {
		log.Printf("livebattle open: SaveState %s: %v", m.battleID, err)
	}
	if err := m.deps.Store.SetBattleStatus(bg, m.battleID, "running"); err != nil {
		log.Printf("livebattle open: set status running %s: %v", m.battleID, err)
	}

	m.broadcastRoom(protocol.RoomPhaseStarting, attached)
	return nil
}

// acceptSubmission validates the picks against ValidateTeam and stores them.
// Once submitted, locked.
func (m *Match) acceptSubmission(side int, picks []engine.TeamPick) error {
	if m.submitted[side] != nil {
		return errors.New("team already submitted — picks are locked")
	}
	if err := engine.ValidateTeam(picks, m.deps.Dex); err != nil {
		return fmt.Errorf("invalid team: %w", err)
	}
	m.submitted[side] = picks
	return nil
}

// collectActions gathers one legal action from each side for a choosing turn —
// both sides are required. See collect for the re-prompt / abort behavior.
func (m *Match) collectActions(ctx context.Context) ([2]engine.Action, error) {
	actions, _, err := m.collect(ctx, [2]bool{true, true})
	return actions, err
}

// collect waits until every required side has produced a legal action for the
// current phase, then returns the per-side actions plus got[i]=true for each
// side that answered. needs[i] marks a side that must answer: both for a
// choosing turn, only the fainted sides for a forced switch. A non-required
// side is left zero with got[i]=false.
//
// Laggards are re-prompted on a ticker. The action a WS slot sends in reply to
// a frame can be lost — most acutely right after a failover takeover, when the
// new owner's resync frame (or the client's reply to it) can race the per-battle
// action queue being (re)bound (the same window in which a dying old owner may
// still be draining the shared queue). The resync is otherwise one-shot, so a
// single lost round would deadlock the turn loop forever. Re-broadcasting the
// current view until every awaited slot has answered makes that self-healing;
// the per-turn dedup drops any duplicate a re-prompt elicits. Runs in the
// coordinator goroutine, so reading m.state is race-free.
//
// The cadence is fast only for the first turn after a takeover (where the lost
// frame is likely) and slow in steady state, so an ordinary player who is just
// thinking is never re-prompted. See resyncInterval / resumeResyncInterval.
//
// A disconnect, the per-turn deadline, or a canceled ctx aborts.
func (m *Match) collect(ctx context.Context, needs [2]bool) (actions [2]engine.Action, got [2]bool, err error) {
	resync := time.NewTicker(m.resyncInterval())
	defer resync.Stop()

	deadline, stopDeadline := m.turnDeadlineChan()
	defer stopDeadline()

	done := func() bool { return (!needs[0] || got[0]) && (!needs[1] || got[1]) }

	for !done() {
		select {
		case act := <-m.actions[0]:
			if err := m.acceptAction(0, act, needs, &actions, &got); err != nil {
				return actions, got, err
			}
		case act := <-m.actions[1]:
			if err := m.acceptAction(1, act, needs, &actions, &got); err != nil {
				return actions, got, err
			}
		case <-m.closed[0]:
			return actions, got, fmt.Errorf("slot p1 disconnected")
		case <-m.closed[1]:
			return actions, got, fmt.Errorf("slot p2 disconnected")
		case <-deadline:
			return actions, got, fmt.Errorf("turn %d timed out after %s — a slot went silent without disconnecting: %w", m.state.Turn, m.turnDeadline, errTurnTimeout)
		case <-resync.C:
			for s := 0; s < 2; s++ {
				if needs[s] && !got[s] {
					m.broadcastOne(s, protocol.FrameState, nil)
				}
			}
		case <-ctx.Done():
			return actions, got, ctx.Err()
		}
	}
	return actions, got, nil
}

// resyncInterval returns the cadence at which the turn loop re-prompts a slot it
// is still waiting on. It is the fast resumeResyncInterval for the first turn
// after a failover takeover (where a one-shot frame is most likely lost to the
// queue-rebind race) and the slow steady-state resyncInterval thereafter. Read
// only from the coordinator goroutine, so m.awaitingResume needs no lock.
func (m *Match) resyncInterval() time.Duration {
	if m.awaitingResume {
		return resumeResyncInterval
	}
	return resyncInterval
}

// turnDeadlineChan returns a channel that fires once the per-turn deadline
// elapses, plus a stop func to release the timer. When no deadline is configured
// it returns a nil channel — which blocks forever in a select, so the backstop
// is simply absent — and a no-op stop.
//
// The deadline is armed per phase, not per turn: collectActions and
// collectReplaceActions each call this with a fresh timer. A turn therefore
// tolerates (1 + R) × turnDeadline of total silence, where R is the number of
// replace rounds — and R is no longer capped at 1, because a replacement that
// dies to entry hazards sends the coordinator round again. With a six-Pokémon
// team and hazards on the field, R can reach 5.
//
// That is intentional per round — each is an independent "waiting on a human"
// window — but it does mean the worst-case turn latency scales with team size
// rather than being the flat 2× this comment used to claim. Callers reasoning
// about the backstop should use (1 + team size), not 2.
func (m *Match) turnDeadlineChan() (<-chan time.Time, func()) {
	if m.turnDeadline <= 0 {
		return nil, func() {}
	}
	t := time.NewTimer(m.turnDeadline)
	return t.C, func() { t.Stop() }
}

// acceptAction validates one side's submitted action for the current phase and,
// if good, records it. A submission is refused when the side isn't being waited
// on, has already answered this phase, or chose an illegal action. A WS slot can
// retry any refusal (toast + keep waiting); an AI slot cannot — a refused AI
// action means the agent's LegalActions and the engine's disagree, a contract
// violation that aborts. This single path is shared by the choosing and
// forced-switch phases, so the AI-contract check applies to both.
func (m *Match) acceptAction(side int, act engine.Action, needs [2]bool, actions *[2]engine.Action, got *[2]bool) error {
	switch {
	case !needs[side]:
		return m.refuseAction(side, "not waiting for an action right now",
			fmt.Sprintf("ai side %d submitted for turn %d while not required", side, m.state.Turn))
	case got[side]:
		return m.refuseAction(side, "your action for this turn was already submitted",
			fmt.Sprintf("ai side %d submitted twice for turn %d", side, m.state.Turn))
	case !isLegalAction(m.state, side, act):
		return m.refuseAction(side, "that action is not legal right now",
			fmt.Sprintf("ai side %d returned an illegal action %+v — contract violation", side, act))
	}
	actions[side], got[side] = act, true
	return nil
}

// refuseAction handles a rejected submission: a WS slot gets a toast and the
// loop keeps waiting (returns nil); an AI slot's rejection is a contract
// violation that aborts the battle (returns an error after logging the detail).
func (m *Match) refuseAction(side int, wsToast, aiReason string) error {
	if m.kind[side] == SideWS {
		m.sendErr(side, wsToast)
		return nil
	}
	log.Printf("AI CONTRACT VIOLATION: battle=%s turn=%d side=%d: %s",
		m.battleID, m.state.Turn, side, aiReason)
	return errors.New(aiReason)
}

// collectReplaceActions gathers forced-switch choices after faints. Only sides
// whose Replace flag is set are waited on; the rest stay nil, which is what
// engine.ResolveReplace expects. It shares collect with the choosing phase, so
// the re-prompt cadence is identical and an illegal AI replacement aborts as a
// contract violation rather than spinning until the turn deadline.
func (m *Match) collectReplaceActions(ctx context.Context) ([2]*engine.Action, error) {
	needs := m.state.Replace
	for i := 0; i < 2; i++ {
		if needs[i] {
			m.send(i, protocol.MatchUpdate{Type: protocol.FrameInfo, Message: "Your Pokémon fainted — choose a replacement."})
		}
	}

	actions, got, err := m.collect(ctx, needs)
	if err != nil {
		return [2]*engine.Action{}, err
	}
	var sw [2]*engine.Action
	for i := 0; i < 2; i++ {
		if got[i] {
			a := actions[i]
			sw[i] = &a
		}
	}
	return sw, nil
}

// driveAITurn produces one turn's AI action for slot i via the AIDecider and
// writes it onto m.actions[i] so collectActions treats the AI side exactly like
// a WS slot.
func (m *Match) driveAITurn(ctx context.Context, side int) {
	act, reasoning := m.deps.AI.Decide(ctx, m.state, side)
	if reasoning != "" {
		m.broadcastInfo(protocol.FrameAI, reasoning)
	}
	select {
	case m.actions[side] <- act:
	case <-ctx.Done():
	}
}

// driveAIReplace produces an AI side's forced-switch choice. Replace decisions
// are shallow enough that a remote round-trip would be pure overhead, so the
// decider resolves them locally.
func (m *Match) driveAIReplace(ctx context.Context, side int) {
	act := m.deps.AI.DecideReplace(ctx, m.state, side)
	select {
	case m.actions[side] <- act:
	case <-ctx.Done():
	}
}

// --- frames ---

// broadcast sends the same logical update to both WS slots with their
// respective fog-of-war views. AI slots are skipped — the decider reads state
// directly, so per-frame views would be allocated and discarded.
func (m *Match) broadcast(typ string, logLines []engine.LogLine) {
	m.broadcastOne(0, typ, logLines)
	m.broadcastOne(1, typ, logLines)
}

func (m *Match) broadcastOne(side int, typ string, logLines []engine.LogLine) {
	if m.kind[side] == SideAI {
		return
	}
	view := ai.MakeViewDex(m.deps.Dex, m.state, side)
	m.send(side, protocol.MatchUpdate{Type: typ, View: &view, Log: logLines, Turn: m.state.Turn})
}

// sendEnd delivers a terminal frame to a WS slot. With a live state it carries
// the final fog-of-war view and turn; before the battle ever became ACTIVE
// (m.state nil — a picker room abandoned in the OPEN phase) it carries neither,
// just the terminal signal so the client leaves the room. A nil winner means the
// battle was abandoned rather than won.
func (m *Match) sendEnd(side int, winner *int) {
	if m.kind[side] == SideAI {
		return
	}
	u := protocol.MatchUpdate{Type: protocol.FrameEnd, Winner: winner}
	if m.state != nil {
		view := ai.MakeViewDex(m.deps.Dex, m.state, side)
		u.View = &view
		u.Turn = m.state.Turn
	}
	m.send(side, u)
}

// broadcastInfo emits a status frame (e.g. AI reasoning) to every WS slot.
func (m *Match) broadcastInfo(kind, message string) {
	if m.kind[0] == SideWS {
		m.send(0, protocol.MatchUpdate{Type: kind, Message: message})
	}
	if m.kind[1] == SideWS {
		m.send(1, protocol.MatchUpdate{Type: kind, Message: message})
	}
}

// broadcastRoom sends a per-slot FrameRoom view of the current OPEN state.
func (m *Match) broadcastRoom(phase protocol.RoomPhase, attached [2]bool) {
	remaining := time.Until(m.createdAt.Add(m.roomDeadline))
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

func (m *Match) sendErr(i int, msg string) {
	m.send(i, protocol.MatchUpdate{Type: protocol.FrameError, Message: msg})
}

// send pushes an update to a slot's sink. AI slots have no drainer; skip them so
// a full buffer can't deadlock the coordinator.
func (m *Match) send(i int, u protocol.MatchUpdate) {
	if m.kind[i] == SideAI {
		return
	}
	m.sink.SendFrame(i, u)
}

// --- persistence ---

// persistTurn fans the post-turn state out: SaveState (Redis) before the
// publish so a late SSE attacher sees a turn Redis already knows about, then the
// domain event, then AppendTurn (Postgres) for replay history. Runs on its own
// context so a client disconnect can't cancel the writes.
func (m *Match) persistTurn(st *engine.BattleState, logLines []engine.LogLine) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.deps.Cache.SaveState(ctx, st); err != nil {
		return
	}
	logJSON, _ := json.Marshal(logLines)
	stateJSON, _ := json.Marshal(st)
	m.deps.Publish(ctx, messages.EventTurnResolved, st.ID, messages.TurnResolved{
		BattleID: st.ID, Turn: st.Turn, Log: logLines, State: st,
	})
	_ = m.deps.Store.AppendTurn(ctx, st.ID, st.Turn, logJSON, stateJSON)
}

// finishBattle records the result and announces it for the leaderboard.
// CompleteBattle (Postgres) must run before publishing BattleCompleted — the
// leaderboard worker reads the battle row to score it. Independent context so a
// client disconnect at the instant of completion can't prevent recording.
func (m *Match) finishBattle(st *engine.BattleState) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = m.deps.Store.CompleteBattle(ctx, st.ID, st.Winner, st.Turn)
	m.deps.Publish(ctx, messages.EventBattleCompleted, st.ID, messages.BattleCompleted{
		BattleID: st.ID, Winner: st.Winner, TurnCount: st.Turn,
	})
	_ = m.deps.Cache.DeleteState(ctx, st.ID)
}

// deleteTokensBest clears the slot-token hash on a short independent context so
// end-of-battle cleanup isn't bound to whatever ctx is in scope.
func (m *Match) deleteTokensBest() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = m.deps.Cache.DeletePvPTokens(ctx, m.battleID)
}

// isLegalAction reports whether act is in the legal set for side. The
// coordinator owns this because it owns the authoritative state.
func isLegalAction(st *engine.BattleState, side int, act engine.Action) bool {
	return engine.ActionAllowed(nil, st, side, act)
}
