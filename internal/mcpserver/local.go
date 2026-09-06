package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"sync"

	"github.com/shaumik/PokeArena/internal/ai"
	"github.com/shaumik/PokeArena/internal/domain"
	"github.com/shaumik/PokeArena/internal/engine"
	"github.com/shaumik/PokeArena/internal/protocol"
)

// local.go: the in-process opponent. A localConn is a whole battle — picker
// room, engine, and a programmatic opponent — behind the same three-method
// conn the gateway WebSocket satisfies, so start_battle needs no gateway, no
// Docker and nobody in a browser.
//
// This is a relocation, not a new feature. The vs-AI battle already existed
// behind the gateway's tokenless `live` mode; everything here (the picker
// handshake, the fog-of-war projection, the legality gate, the AI opponent)
// is the same engine and the same ai package the gateway drives, reached
// without the network in between.
//
// Frame fidelity is the contract: the session dispatcher, Wait's tick loop and
// every tool handler are shared with the gateway path and cannot tell the two
// apart. In particular a state-bearing frame (state / turn) means "your move",
// so the run loop resolves any phase the agent is not part of — an opponent-only
// faint replacement — before emitting anything, rather than waking the agent
// for a decision that is not theirs.

// localOpponent names a built-in opponent policy. These are the two the
// benchmark treats as baselines; expectimax is the stronger of the two, but
// see docs/deeper-search-played-worse.md before reading depth as strength.
const (
	opponentHeuristic  = "heuristic"
	opponentExpectimax = "expectimax"
)

// localTrainerName / localOpponentName are the display names on each side.
// They land in the view's `self.trainer` and in the room frames.
const (
	localTrainerName  = "Agent"
	localOpponentName = "AI"
)

// localInboxDepth is the client → driver queue. Depth 1 is enough: the tool
// surface is strictly one call at a time, and Send is only ever reached from
// a tool handler.
const localInboxDepth = 1

// localConn drives one offline battle. It satisfies conn.
//
// Ownership: the run goroutine owns the engine state exclusively — nothing
// else touches *engine.BattleState — so the battle needs no lock of its own.
// The mutex here guards only shutdown, which any goroutine may call.
type localConn struct {
	dex      *domain.Dex
	opp      ai.Agent
	oppPicks []engine.TeamPick
	seed     uint64

	// me is the side index the MCP agent controls. Fixed at 0, mirroring the
	// gateway's live mode, which hardcodes the human to p1.
	me int

	updates chan protocol.MatchUpdate
	inbox   chan protocol.WsClientMsg
	stop    chan struct{}
	once    sync.Once

	// sentState records whether the opening FrameState has gone out, so
	// later frames are FrameTurn — matching what a gateway client sees.
	sentState bool
}

// newLocalConn builds an offline battle and starts its run loop. The battle
// opens in the picker phase: the caller's next move is submit_team, exactly
// as it would be against the gateway.
//
// seed pins the whole game — the engine's RNG stream and the opponent's team
// draw — so the same seed and the same submitted team replay bit-for-bit.
func newLocalConn(dex *domain.Dex, pool *ai.TeamPool, opponent string, seed uint64) (*localConn, error) {
	if dex == nil {
		return nil, errors.New("pokearena-mcp: offline mode has no dataset loaded")
	}
	if pool == nil {
		return nil, errors.New("pokearena-mcp: offline mode has no opponent teams loaded")
	}
	// The opponent's roster is drawn from the seed too, so "same seed, same
	// team" is a complete description of the game — not "same seed, and
	// whatever roster the process happened to pick".
	oppPicks, err := pool.Pick(rand.New(rand.NewSource(int64(seed)))) //nolint:gosec // deterministic replay, not security
	if err != nil {
		return nil, fmt.Errorf("draw opponent team: %w", err)
	}

	var agent ai.Agent
	switch opponent {
	case "", opponentHeuristic:
		agent = ai.NewHeuristicAgent(dex)
	case opponentExpectimax:
		agent = ai.NewExpectimaxAgent(dex)
	default:
		return nil, fmt.Errorf("unknown opponent %q (want %q or %q)",
			opponent, opponentHeuristic, opponentExpectimax)
	}

	c := &localConn{
		dex:      dex,
		opp:      agent,
		oppPicks: oppPicks,
		seed:     seed,
		me:       0,
		updates:  make(chan protocol.MatchUpdate, 8), // matches gwclient's buffer
		inbox:    make(chan protocol.WsClientMsg, localInboxDepth),
		stop:     make(chan struct{}),
	}
	go c.run()
	return c, nil
}

// Updates implements conn.
func (c *localConn) Updates() <-chan protocol.MatchUpdate { return c.updates }

// Send implements conn. It hands the frame to the run goroutine. A send after
// the battle has ended is dropped rather than erroring: the gateway path
// behaves the same way (the socket is gone and the write is lost), and the
// session learns the battle is over from the terminal frame, not from Send.
func (c *localConn) Send(msg protocol.WsClientMsg) error {
	select {
	case c.inbox <- msg:
		return nil
	case <-c.stop:
		return errors.New("pokearena-mcp: battle is no longer running")
	}
}

// Close implements conn. Idempotent, and always unblocks Updates — the run
// goroutine closes c.updates on its way out.
func (c *localConn) Close() error {
	c.once.Do(func() { close(c.stop) })
	return nil
}

// run is the whole battle: picker room, then turns, then the end frame. It
// owns c.updates and closes it on exit, which is what makes Close unblock a
// waiting dispatcher.
func (c *localConn) run() {
	defer close(c.updates)

	picks, ok := c.runPicker()
	if !ok {
		return
	}

	// Side 0 is the agent, side 1 the built-in opponent — c.me is 0, so the
	// pick lists go in that order.
	s, err := engine.NewBattleFromPicks(c.dex,
		fmt.Sprintf("local-%d", c.seed),
		localTrainerName, picks,
		localOpponentName, c.oppPicks,
		c.seed)
	if err != nil {
		// ValidateTeam passed but the engine still refused to build the side.
		// That is a bug rather than a user mistake, so say so plainly instead
		// of dressing it up as a rejected team.
		c.emitError(fmt.Sprintf("could not start the battle: %v", err))
		return
	}
	c.play(s)
}

// runPicker is the OPEN phase. It emits the room frame, waits for a team,
// validates it, and re-emits the room with you.submitted set — which is the
// exact acknowledgement session.SubmitTeam blocks on. A rejected team gets a
// FrameError and the room stays open, so the agent can fix it and resubmit
// without rejoining.
func (c *localConn) runPicker() ([]engine.TeamPick, bool) {
	if !c.emit(protocol.MatchUpdate{
		Type: protocol.FrameRoom,
		Room: c.room(false),
	}) {
		return nil, false
	}

	for {
		msg, ok := c.recv()
		if !ok {
			return nil, false
		}
		switch msg.Type {
		case protocol.MsgSubmitTeam:
			if err := engine.ValidateTeam(msg.Picks, c.dex); err != nil {
				if !c.emitError(err.Error()) {
					return nil, false
				}
				continue
			}
			// Clone before handing to the engine: the caller's slice came off
			// a tool call and must not alias battle state.
			picks := engine.ClonePicks(msg.Picks)
			if !c.emit(protocol.MatchUpdate{
				Type: protocol.FrameRoom,
				Room: c.room(true),
			}) {
				return nil, false
			}
			return picks, true

		case protocol.MsgLeaveRoom:
			return nil, false

		default:
			// An action before the battle exists. Say what is actually
			// expected rather than ignoring it — a silently dropped frame
			// leaves the agent waiting on a turn that will never come.
			if !c.emitError("the battle has not started yet — call submit_team first") {
				return nil, false
			}
		}
	}
}

// play runs the battle to its end. The loop only surfaces a frame when the
// agent's decision is the one being waited on; an opponent-only replacement is
// resolved in place.
func (c *localConn) play(s *engine.BattleState) {
	foe := 1 - c.me

	for !s.Ended() {
		switch s.Phase {
		case engine.PhaseChoosing:
			if !c.emitView(s) {
				return
			}
			act, ok := c.awaitAction(s, c.me)
			if !ok {
				return
			}
			var acts [2]engine.Action
			acts[c.me] = act
			acts[foe] = c.decideOpp(s, foe)
			engine.ResolveTurn(c.dex, s, acts)

		case engine.PhaseReplace:
			var sw [2]*engine.Action
			if s.Replace[c.me] {
				if !c.emitView(s) {
					return
				}
				act, ok := c.awaitAction(s, c.me)
				if !ok {
					return
				}
				sw[c.me] = &act
			}
			if s.Replace[foe] {
				a := c.decideOpp(s, foe)
				sw[foe] = &a
			}
			engine.ResolveReplace(s, sw)

		default:
			c.emitError(fmt.Sprintf("battle reached unexpected phase %q", s.Phase))
			return
		}
	}

	winner := s.Winner
	c.emit(protocol.MatchUpdate{
		Type:   protocol.FrameEnd,
		View:   viewPtr(ai.MakeView(s, c.me)),
		Winner: &winner,
		Turn:   s.Turn,
	})
}

// decideOpp asks the built-in opponent for its action, falling back to the
// first legal action if it errors or proposes something illegal. Same
// substitution eval.decide makes, and for the same reason: the battle must
// always make legal progress.
func (c *localConn) decideOpp(s *engine.BattleState, side int) engine.Action {
	v := ai.MakeView(s, side)
	legal := ai.LegalActions(v)
	act, err := c.opp.Decide(context.Background(), v)
	if err != nil || !isLegalAction(legal, act) {
		if len(legal) > 0 {
			return legal[0]
		}
	}
	return act
}

// awaitAction blocks for the agent's action and gates it on the engine's own
// legality list. An illegal or malformed action is answered with a FrameError
// and the agent is asked again — the battle never advances on a bad action,
// which is what keeps a confused agent from silently forfeiting a turn.
func (c *localConn) awaitAction(s *engine.BattleState, side int) (engine.Action, bool) {
	legal := engine.LegalActionsDex(c.dex, s, side)

	for {
		msg, ok := c.recv()
		if !ok {
			return engine.Action{}, false
		}
		switch msg.Type {
		case protocol.MsgAction:
			act := engine.Action{
				Kind:         kindFromWire(msg.Kind),
				Index:        msg.Index,
				SwitchTarget: msg.SwitchTarget,
			}
			if !isLegalAction(legal, act) {
				if !c.emitError(fmt.Sprintf(
					"%s %d is not legal right now; legal actions: %s",
					msg.Kind, msg.Index, describeActions(legal))) {
					return engine.Action{}, false
				}
				continue
			}
			return act, true

		case protocol.MsgLeaveRoom:
			return engine.Action{}, false

		default:
			// submit_team once the battle is active. The gateway ignores it
			// (the room is gone); say so, since an agent that resubmits is
			// otherwise stuck waiting for an ack that cannot arrive.
			if !c.emitError("the battle is already active — submit_team is no longer accepted") {
				return engine.Action{}, false
			}
		}
	}
}

// room builds the picker-room payload. The opponent is always attached and
// always already submitted: it is a program, it does not dawdle, and pretending
// otherwise would make the agent wait for nothing.
func (c *localConn) room(submitted bool) *protocol.RoomUpdate {
	return &protocol.RoomUpdate{
		Phase: protocol.RoomPhaseOpen,
		You: protocol.RoomSlot{
			Attached:  true,
			Submitted: submitted,
			Trainer:   localTrainerName,
		},
		Them: protocol.RoomSlot{
			Attached:  true,
			Submitted: true,
			Trainer:   localOpponentName,
		},
	}
}

// emitView sends the agent's fog-of-war view: FrameState the first time (the
// battle becoming active), FrameTurn thereafter — the same sequence a gateway
// client sees.
//
// The frame carries only the typed View, never RawView. That is correct here
// and only here: RawView exists so a relay can forward a server's redaction
// byte-for-byte instead of losing fields through a decode, and this view was
// built fresh from battle state rather than decoded off a wire. session.wireOut
// falls through to marshaling the typed View, which applies the same
// MarshalJSON redaction the gateway would have applied.
func (c *localConn) emitView(s *engine.BattleState) bool {
	frame := protocol.FrameTurn
	if !c.sentState {
		frame = protocol.FrameState
		c.sentState = true
	}
	return c.emit(protocol.MatchUpdate{
		Type: frame,
		View: viewPtr(ai.MakeView(s, c.me)),
		Turn: s.Turn,
	})
}

// emitError sends a non-fatal error frame. Reported as a bool so callers can
// abandon the battle when the consumer has gone away.
func (c *localConn) emitError(msg string) bool {
	return c.emit(protocol.MatchUpdate{Type: protocol.FrameError, Message: msg})
}

// emit publishes one frame, or reports false if the battle was closed while
// we were trying. Selecting on c.stop is what stops a Close from deadlocking
// against a full updates buffer.
func (c *localConn) emit(u protocol.MatchUpdate) bool {
	select {
	case c.updates <- u:
		return true
	case <-c.stop:
		return false
	}
}

// recv takes the next client frame, or reports false once the battle is
// closed.
func (c *localConn) recv() (protocol.WsClientMsg, bool) {
	select {
	case msg := <-c.inbox:
		return msg, true
	case <-c.stop:
		return protocol.WsClientMsg{}, false
	}
}

// viewPtr copies v to the heap. MatchUpdate.View is a pointer, and taking the
// address of a loop-local would alias the next frame's view.
func viewPtr(v ai.View) *ai.View { return &v }

// kindFromWire maps the wire's action kind to the engine's, matching
// httpapi.kindFromWire: "switch" is the only non-default, anything else is a
// move. Duplicated rather than exported across the package boundary because
// the gateway's copy is about decoding an untrusted socket and this one is
// about decoding a tool call — same two lines, different trust stories.
func kindFromWire(s string) engine.ActionKind {
	if s == protocol.ActionKindSwitch {
		return engine.ActionSwitch
	}
	return engine.ActionMove
}

// isLegalAction reports whether a is in the legal set, comparing with
// Action.Equal so SwitchTarget is followed rather than pointer-compared.
func isLegalAction(legal []engine.Action, a engine.Action) bool {
	for _, l := range legal {
		if l.Equal(a) {
			return true
		}
	}
	return false
}

// describeActions renders a legal-action list for an error message, e.g.
// "move 0, move 3, switch 2". This is the one place an agent learns what it
// could have done, so it names every option rather than a count.
//
// Deduplicated on kind+index: LegalActionsDex enumerates a self-switch move
// once per bench member it could pivot into, which is right for a search but
// would print "move 1" five times to a reader.
func describeActions(legal []engine.Action) string {
	if len(legal) == 0 {
		return "(none)"
	}
	type key struct {
		kind engine.ActionKind
		idx  int
	}
	seen := make(map[key]bool, len(legal))
	parts := make([]string, 0, len(legal))
	for _, a := range legal {
		k := key{a.Kind, a.Index}
		if seen[k] {
			continue
		}
		seen[k] = true
		kind := protocol.ActionKindMove
		if a.Kind == engine.ActionSwitch {
			kind = protocol.ActionKindSwitch
		}
		parts = append(parts, fmt.Sprintf("%s %d", kind, a.Index))
	}
	return strings.Join(parts, ", ")
}
