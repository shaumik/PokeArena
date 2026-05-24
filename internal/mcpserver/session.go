package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"pokearena/internal/ai"
	"pokearena/internal/protocol"
)

// Session-level errors. Tool handlers translate these into the
// agent-facing MCP error responses.
var (
	errNotJoined     = errors.New("pokearena-mcp: not joined to a battle — call join_battle first")
	errAlreadyJoined = errors.New("pokearena-mcp: already joined to a battle — call leave_battle first")
	errBattleEnded   = errors.New("pokearena-mcp: battle has ended")
	errNotYourTurn   = errors.New("pokearena-mcp: not your turn — call wait first")
)

// session is the per-process battle state. Exactly one session per MCP
// server process per the design (one process, many sequential battles,
// never concurrent). All mutable state is behind s.mu; the dispatcher
// goroutine and the tool-handler goroutines share access through it.
type session struct {
	cfg Config

	mu sync.Mutex

	// client is non-nil iff a battle is bound. Set under mu in Join,
	// cleared in Leave.
	client *gwClient

	// latest is the most recent BattleView seen. Set on every
	// state/turn/end frame. Nil until the first state frame arrives.
	latest *ai.View

	// needsAction is true when a state/turn frame has arrived since
	// the agent last submitted an action. Cleared by Act; set by the
	// dispatcher on incoming frames. This is the "your turn" signal.
	needsAction bool

	// terminal goes true on FrameEnd or when the dispatcher exits
	// because the connection closed. Wait returns ready+terminal once
	// this flips; subsequent Act calls return errBattleEnded.
	terminal bool

	// winner is set from FrameEnd.Winner. Nil if the battle didn't
	// end naturally (connection drop, leave_battle).
	winner *int

	// tick is closed and replaced under mu whenever any of the above
	// changes. Waiters snapshot the current pointer, then select on it
	// against their timeout. This is the standard Go broadcast idiom
	// that composes with select (which sync.Cond does not).
	tick chan struct{}

	// dispatcherDone is closed when the dispatcher goroutine exits.
	// Mostly for Leave to wait on a clean shutdown.
	dispatcherDone chan struct{}

	// Bookkeeping returned in JoinResult / surfaced in logs.
	battleID string
	slot     string
}

func newSession(cfg Config) *session {
	return &session{cfg: cfg, tick: make(chan struct{})}
}

// Join opens the gateway WS, starts the dispatcher, and blocks until
// the first state frame arrives. Returns the initial view + identity
// info the agent needs to play.
func (s *session) Join(ctx context.Context, battleID, slot, token string) (joinBattleOut, error) {
	s.mu.Lock()
	if s.client != nil {
		s.mu.Unlock()
		return joinBattleOut{}, errAlreadyJoined
	}
	s.mu.Unlock()

	gc, err := dialGateway(ctx, s.cfg.GatewayURL, battleID, slot, token)
	if err != nil {
		return joinBattleOut{}, fmt.Errorf("connect to gateway: %w", err)
	}

	s.mu.Lock()
	s.client = gc
	s.battleID = battleID
	s.slot = slot
	s.latest = nil
	s.needsAction = false
	s.terminal = false
	s.winner = nil
	s.dispatcherDone = make(chan struct{})
	s.mu.Unlock()

	go s.dispatch(gc)

	// Block until either a state frame arrives or the dispatcher gives
	// up (gateway rejected us, e.g. SLOT_TAKEN). The gateway sends
	// "info" while waiting for the other slot but won't send "state"
	// until both sides attach, so this can block for the whole pairing
	// window — that's by design.
	if err := s.awaitFirstView(ctx); err != nil {
		// Clean up: closing the client triggers dispatcher exit; reset
		// session state so a retry is possible.
		gc.Close()
		<-s.dispatcherDone
		s.reset()
		return joinBattleOut{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	return joinBattleOut{
		BattleID:    battleID,
		Slot:        slot,
		YourTrainer: s.latest.Self.Trainer,
		// OpponentTrainer left empty in v0 — BattleView doesn't carry it;
		// the SPA shows "Opponent" for the same reason. Tracked in
		// backlog/gateway-second-slot.md.
		View: *s.latest,
	}, nil
}

// View returns the latest BattleView. Errors if not joined.
func (s *session) View() (ai.View, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client == nil {
		return ai.View{}, errNotJoined
	}
	if s.latest == nil {
		// Joined but no state frame yet — shouldn't happen because Join
		// blocks until the first frame, but handle for safety.
		return ai.View{}, errors.New("pokearena-mcp: no view available yet")
	}
	return *s.latest, nil
}

// Wait blocks until it's the agent's turn, the battle ends, or the
// timeout elapses. timeoutSeconds is clamped per the spec (1..120).
func (s *session) Wait(ctx context.Context, timeoutSeconds int) (waitOut, error) {
	if timeoutSeconds < 1 {
		timeoutSeconds = 60
	}
	if timeoutSeconds > 120 {
		timeoutSeconds = 120
	}
	deadline := time.NewTimer(time.Duration(timeoutSeconds) * time.Second)
	defer deadline.Stop()

	for {
		s.mu.Lock()
		if s.client == nil {
			s.mu.Unlock()
			return waitOut{}, errNotJoined
		}
		// Terminal takes precedence over needsAction — once the battle
		// is over there's nothing to choose anymore.
		if s.terminal {
			out := waitOut{Ready: true, Terminal: true}
			if s.latest != nil {
				v := *s.latest
				out.View = &v
			}
			s.mu.Unlock()
			return out, nil
		}
		if s.needsAction && s.latest != nil {
			v := *s.latest
			s.mu.Unlock()
			return waitOut{Ready: true, View: &v}, nil
		}
		tick := s.tick
		s.mu.Unlock()

		select {
		case <-tick:
			// State changed; loop and re-evaluate.
		case <-deadline.C:
			return waitOut{Ready: false}, nil
		case <-ctx.Done():
			return waitOut{}, ctx.Err()
		}
	}
}

// Act submits one action. Optimistic: the gateway acknowledges by
// resolving the turn (visible on the next Wait) or rejects via a
// FrameError that the dispatcher will surface on the next Wait.
func (s *session) Act(kind string, index int) (actOut, error) {
	s.mu.Lock()
	if s.client == nil {
		s.mu.Unlock()
		return actOut{}, errNotJoined
	}
	if s.terminal {
		s.mu.Unlock()
		return actOut{}, errBattleEnded
	}
	if !s.needsAction {
		s.mu.Unlock()
		return actOut{}, errNotYourTurn
	}
	turn := 0
	if s.latest != nil {
		turn = s.latest.Turn
	}
	client := s.client
	// Clear needsAction before we Send: prevents a double-fire if the
	// agent calls Act twice quickly. If Send fails we'll learn via the
	// dispatcher closing the connection.
	s.needsAction = false
	s.mu.Unlock()

	if err := client.Send(protocol.WsClientMsg{
		Type: "action", Kind: kind, Index: index,
	}); err != nil {
		return actOut{}, fmt.Errorf("send action: %w", err)
	}
	return actOut{Accepted: true, Turn: turn}, nil
}

// Leave closes the gateway connection and resets session state so a
// subsequent Join is possible. No-op if not joined.
func (s *session) Leave() error {
	s.mu.Lock()
	client := s.client
	done := s.dispatcherDone
	s.mu.Unlock()

	if client == nil {
		return nil // no-op per spec
	}
	client.Close()
	if done != nil {
		<-done
	}
	s.reset()
	return nil
}

// reset clears all per-battle state. Called after a clean leave or a
// failed join. Holds s.mu briefly; safe to call from any goroutine.
func (s *session) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.client = nil
	s.latest = nil
	s.needsAction = false
	s.terminal = false
	s.winner = nil
	s.battleID = ""
	s.slot = ""
	// Don't touch s.tick — leave it for the next Join to inherit; any
	// stale waiter on the old tick will get woken when this goroutine
	// closes it (which happens at the next state change).
}

// awaitFirstView blocks until the first state frame arrives or the
// dispatcher exits without setting one (gateway rejected us).
func (s *session) awaitFirstView(ctx context.Context) error {
	for {
		s.mu.Lock()
		if s.latest != nil {
			s.mu.Unlock()
			return nil
		}
		tick := s.tick
		s.mu.Unlock()

		select {
		case <-tick:
		case <-s.dispatcherDone:
			// Dispatcher exited without a frame → connection died
			// before pairing completed. Most common cause: token
			// rejected by gateway (which sends a close, not a state).
			return errors.New("gateway closed connection before initial state")
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// dispatch consumes frames from gc, updates session state, and ticks
// waiters. Exits when gc.Updates closes (Close or connection death).
func (s *session) dispatch(gc *gwClient) {
	defer close(s.dispatcherDone)

	for u := range gc.Updates() {
		s.mu.Lock()
		switch u.Type {
		case protocol.FrameState, protocol.FrameTurn:
			if u.View != nil {
				s.latest = u.View
			}
			// Every state or turn frame means it's our turn to choose
			// next — unless the gateway also marked us terminal (which
			// it wouldn't on these frame types, but defensive).
			if !s.terminal {
				s.needsAction = true
			}
		case protocol.FrameEnd:
			if u.View != nil {
				s.latest = u.View
			}
			s.terminal = true
			s.winner = u.Winner
			s.needsAction = false
		case protocol.FrameInfo:
			// No state change; don't tick waiters. The "waiting for
			// opponent" info isn't actionable.
			s.mu.Unlock()
			continue
		case protocol.FrameError:
			// Surface as terminal-ish for now: clear needsAction so the
			// next Wait re-evaluates and the agent gets unstuck. Proper
			// error propagation is a future enhancement.
			s.needsAction = false
		}
		s.tickLocked()
		s.mu.Unlock()
	}

	// Updates closed → connection ended. If we weren't already terminal,
	// mark so; either way wake any waiter so they don't block forever.
	s.mu.Lock()
	if !s.terminal {
		s.terminal = true
	}
	s.tickLocked()
	s.mu.Unlock()
}

// tickLocked closes the current tick channel and creates a fresh one.
// Caller must hold s.mu.
func (s *session) tickLocked() {
	old := s.tick
	s.tick = make(chan struct{})
	close(old)
}
