// Package livebattle owns the coordinator for a single live battle — whether
// that's "live" (one human WS + one in-process AI) or "live_pvp" (two human or
// agent WS clients). It drives the picker-room phase, holds the authoritative
// BattleState once ACTIVE, and runs the turn loop to completion.
//
// The coordinator is deliberately transport-agnostic. It talks to its slots
// through a FrameSink (outbound frames) and a small set of inbound channels a
// Producer feeds (actions, team submissions, disconnect). That boundary is the
// whole point of the package: the same coordinator runs unchanged inside the
// gateway (in-process channels backed by WebSockets) or inside a dedicated
// battle-session service (channels backed by a message broker). Nothing here
// imports the gateway, the broker, or net/http.
package livebattle

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"pokearena/internal/domain"
	"pokearena/internal/engine"
	"pokearena/internal/protocol"
)

// DefaultRoomDeadline is the picker-room budget per docs/team-picker-room.md §7.
// A single timer covers everything: abandoned URL, slow picker, idle attach. If
// the room is not ACTIVE by t+deadline, it dies. Ten minutes is long enough for
// a deliberate human draft against an LLM agent that's reasoning out its picks.
const DefaultRoomDeadline = 10 * time.Minute

// resyncInterval is the steady-state cadence at which the turn loop re-broadcasts
// the current view to a WS slot it is still waiting on — a safety net for a
// frame/reply lost over the broker. It is deliberately long: a normal turn (human
// or agent) resolves well inside it, so a player who is simply thinking is never
// re-prompted. It only fires once a slot has genuinely gone quiet, recovering a
// stalled battle within one interval.
//
// resumeResyncInterval is the much shorter cadence used only for the first
// choosing turn after a failover takeover, where the lost-frame risk is
// concentrated: the new owner's one-shot resync frame (or the client's reply to
// it) can race the per-battle action queue being (re)bound, and a lost round
// would otherwise deadlock the battle forever. Recovering that fast keeps a
// takeover unnoticeable; once the first post-takeover action flows, the loop
// relaxes to resyncInterval so it does not spam a thinking player thereafter.
const (
	resyncInterval       = 20 * time.Second
	resumeResyncInterval = 2 * time.Second
)

// SideKind tags whether a slot is driven by a remote WS/agent client or by an
// in-process AI driver. From the coordinator's POV the two are interchangeable:
// both produce actions on the same inbound channel.
type SideKind int

const (
	SideWS SideKind = iota
	SideAI
)

// Reason is why Run returned. The host branches its cleanup on it: a Completed
// or Disconnected/DeadlineExpired battle is finished and must be made terminal,
// whereas a Yielded battle has been handed to another owner and its state and
// action queue must be left intact for that owner to pick up.
type Reason int

const (
	// ReasonCompleted: the battle ended naturally (a winner). Run has already
	// recorded the result and cleared live state.
	ReasonCompleted Reason = iota
	// ReasonDisconnected: a slot's feeder went away mid-battle. The battle is
	// abandoned — no owner will ever drive it again.
	ReasonDisconnected
	// ReasonDeadlineExpired: the picker room expired before both sides submitted.
	ReasonDeadlineExpired
	// ReasonYielded: the host cancelled Run (lost ownership lease, or shutdown).
	// Another instance may now own this battle; leave its state alone.
	ReasonYielded
)

func (r Reason) String() string {
	switch r {
	case ReasonCompleted:
		return "completed"
	case ReasonDisconnected:
		return "disconnected"
	case ReasonDeadlineExpired:
		return "deadline-expired"
	case ReasonYielded:
		return "yielded"
	default:
		return "unknown"
	}
}

// FrameSink is the coordinator's outbound edge: one per-slot server frame at a
// time. The gateway backs this with a channel a WebSocket writer drains; the
// battle-session backs it with a broker publish keyed by slot. Close is called
// exactly once when the coordinator shuts down so any drainer can exit.
type FrameSink interface {
	SendFrame(slot int, u protocol.MatchUpdate)
	Close()
}

// AIDecider supplies actions for AI-driven slots. The gateway implements it by
// publishing an ai.job and awaiting the ai.decided reply (falling back to a
// local heuristic on timeout); the battle-session implements it by running the
// agent harness in-process. Start runs any background correlation machinery for
// the lifetime of the match (a no-op for the in-process decider).
type AIDecider interface {
	Start(ctx context.Context)
	Decide(ctx context.Context, st *engine.BattleState, side int) (action engine.Action, reasoning string)
	DecideReplace(ctx context.Context, st *engine.BattleState, side int) engine.Action
}

// StateCache is the ephemeral-state persistence the coordinator needs
// (Redis-backed in production). *cache.Cache satisfies it directly.
type StateCache interface {
	SaveState(ctx context.Context, st *engine.BattleState) error
	DeleteState(ctx context.Context, id string) error
	DeletePvPTokens(ctx context.Context, battleID string) error
}

// StateStore is the durable persistence the coordinator needs
// (Postgres-backed in production). *store.Store satisfies it directly.
type StateStore interface {
	SetBattleStatus(ctx context.Context, id, status string) error
	AppendTurn(ctx context.Context, battleID string, turnNo int, log, stateDigest []byte) error
	CompleteBattle(ctx context.Context, id string, winner, turnCount int) error
}

// PublishFunc fans a domain event out to spectators (and cross-process
// consumers). The gateway routes it through its Hub plus the broker; the
// battle-session publishes straight to the broker.
type PublishFunc func(ctx context.Context, eventType, battleID string, msg any)

// Deps bundles everything the coordinator needs from its host. Keeping it a
// struct of narrow interfaces (rather than one fat Host interface) lets each
// host supply exactly its own cache/store/broker without an adapter layer.
type Deps struct {
	Dex     *domain.Dex
	Cache   StateCache
	Store   StateStore
	Publish PublishFunc
	// AI decides AI-side actions. Nil is allowed only when no slot is SideAI.
	AI AIDecider
	// OnDone runs once at shutdown for host-specific cleanup (the gateway
	// removes the match from its registry; the battle-session releases its
	// ownership lease). May be nil.
	OnDone func(battleID string)
}

// Config constructs a Match. AITeams[i] is consulted only when Kinds[i] is
// SideAI. RoomDeadline of 0 selects DefaultRoomDeadline.
type Config struct {
	BattleID     string
	P1Name       string
	P2Name       string
	Seed         uint64
	Kinds        [2]SideKind
	AITeams      [2][]engine.TeamPick
	Sink         FrameSink
	Deps         Deps
	RoomDeadline time.Duration
	// DisconnectGrace is how long the coordinator waits after a slot's WS bridge
	// signals disconnect before declaring the slot gone and ending the battle. A
	// re-attach (under any connection id) within the window cancels it, so a
	// transient blip or page refresh no longer abandons an in-progress battle.
	// Zero means no grace — a disconnect ends the match immediately (the unit-test
	// default and the in-process gateway's historical behavior).
	DisconnectGrace time.Duration
	// TurnDeadline bounds how long a choosing/replace turn waits for a WS slot's
	// action before treating the slot as gone. It is the backstop for a gateway
	// that dies without sending a disconnect (a crash, not a clean close): no
	// disconnect message ever arrives, so without this the turn loop would
	// re-prompt forever. Zero disables it (the unit-test default).
	TurnDeadline time.Duration
}

// Match coordinates one live battle from creation through end. Slots attach a
// Producer (WS handler or broker pump) to feed actions/submissions inbound and
// receive frames via the Sink; AI slots are driven by in-process goroutines
// that write to the same inbound channels, so the turn loop treats every slot
// identically.
type Match struct {
	battleID     string
	createdAt    time.Time
	seed         uint64
	trainerName  [2]string
	kind         [2]SideKind
	aiTeam       [2][]engine.TeamPick
	roomDeadline time.Duration

	deps Deps
	sink FrameSink

	disconnectGrace time.Duration
	turnDeadline    time.Duration

	// conn tracks the live WS connection per slot for disconnect identity and
	// reconnect grace. Guarded by connMu because it is touched from the action
	// pump goroutine (SlotConnected/SlotDisconnected) and from grace timer
	// goroutines, never from the coordinator goroutine.
	connMu     sync.Mutex
	activeConn [2]string      // id of the connection currently bound to the slot
	graceGen   [2]uint64      // bumped on every (dis)connect to invalidate a pending grace timer
	graceTimer [2]*time.Timer // pending reconnect-grace timer, if any

	// state is nil during the OPEN phase; set by runOpenPhase once both teams
	// validate, read by everything after.
	state *engine.BattleState

	// resumed is true for a failover takeover: the match adopts an existing live
	// state and re-enters the turn loop without a picker phase.
	resumed bool

	// awaitingResume is true from a takeover until the first post-takeover action
	// arrives. While set, collectActions re-prompts on resumeResyncInterval so a
	// resync frame/reply lost to the queue (re)bind race recovers fast; it clears
	// once the inbound channel is confirmed flowing, dropping the loop back to the
	// slow steady-state resyncInterval. Read/written only by the coordinator
	// goroutine.
	awaitingResume bool

	// turn mirrors state.Turn atomically so the action Pump can drop stale
	// redelivered actions (Turn < current) without racing the coordinator
	// goroutine that owns state.
	turn atomic.Int64

	// Per-slot inbound channels (0=p1, 1=p2). A Producer or an AI driver writes
	// to actions/submits; closed is closed once when a slot disconnects;
	// attached is closed once when a slot registers (immediately for AI sides).
	actions   [2]chan engine.Action
	submits   [2]chan []engine.TeamPick
	attached  [2]chan struct{}
	closed    [2]chan struct{}
	closeOnce [2]sync.Once

	// once[i] serializes slot-registration; first caller wins. The network-side
	// guard (cache.ClaimSlot) is the real defense against double-attach; this is
	// the in-process belt.
	once [2]sync.Once
	won  [2]bool

	// submitted picks per slot, written only by the coordinator goroutine.
	submitted [2][]engine.TeamPick

	// done is closed at shutdown so producers blocked on a send can bail out.
	done chan struct{}
}

// NewMatch builds an OPEN-phase coordinator. AI sides are pre-attached (their
// attached channel is closed and won flag set), so the open phase never waits
// for them to register.
func NewMatch(cfg Config) *Match {
	deadline := cfg.RoomDeadline
	if deadline <= 0 {
		deadline = DefaultRoomDeadline
	}
	m := &Match{
		battleID:        cfg.BattleID,
		createdAt:       time.Now(),
		seed:            cfg.Seed,
		trainerName:     [2]string{cfg.P1Name, cfg.P2Name},
		kind:            cfg.Kinds,
		aiTeam:          cfg.AITeams,
		roomDeadline:    deadline,
		deps:            cfg.Deps,
		sink:            cfg.Sink,
		disconnectGrace: cfg.DisconnectGrace,
		turnDeadline:    cfg.TurnDeadline,
		done:            make(chan struct{}),
	}
	for i := 0; i < 2; i++ {
		// Capacity 1: one outstanding action/submission per slot per phase is
		// the whole protocol; further backpressure lives in the Producer.
		m.actions[i] = make(chan engine.Action, 1)
		m.submits[i] = make(chan []engine.TeamPick, 1)
		m.attached[i] = make(chan struct{})
		m.closed[i] = make(chan struct{})
		if m.kind[i] == SideAI {
			m.won[i] = true
			close(m.attached[i])
		}
	}
	return m
}

// NewResumedMatch builds a coordinator for a battle already in progress, for a
// failover takeover. It adopts the given live state and re-enters the turn loop
// directly — no picker phase, no battle-started event (those already happened).
// Engine purity guarantees the resumed line is identical to the one the dead
// owner was running. WS slots are fed lazily by the Pump on their next action;
// AI sides resume from the same state.
func NewResumedMatch(cfg Config, state *engine.BattleState) *Match {
	m := NewMatch(cfg)
	m.state = state
	m.resumed = true
	m.awaitingResume = true
	m.turn.Store(int64(state.Turn))
	return m
}

// BattleID returns the coordinated battle's id.
func (m *Match) BattleID() string { return m.battleID }

// CurrentTurn returns the turn number the coordinator is currently on (0 before
// the battle is ACTIVE). Read by the Pump to drop stale redelivered actions.
func (m *Match) CurrentTurn() int { return int(m.turn.Load()) }

// Done is closed when the coordinator shuts down.
func (m *Match) Done() <-chan struct{} { return m.done }

// Producer is the handle a slot's feeder (WS handler or broker pump) uses to
// push inbound traffic. Sends should race Done so a producer never blocks past
// the coordinator's lifetime.
type Producer struct {
	Actions chan<- engine.Action
	Submits chan<- []engine.TeamPick
	Done    <-chan struct{}
}

// Attach registers a slot's feeder. Returns (handle, true) on the first call
// for that slot; (zero, false) on any subsequent call. Two winners here would
// corrupt the coordinator — cache.ClaimSlot gates this at the network edge.
func (m *Match) Attach(slot int) (Producer, bool) {
	won := false
	m.once[slot].Do(func() {
		won = true
		m.won[slot] = true
		close(m.attached[slot])
	})
	if !won {
		return Producer{}, false
	}
	return Producer{Actions: m.actions[slot], Submits: m.submits[slot], Done: m.done}, true
}

// Disconnect signals that a slot's feeder is permanently gone. Idempotent — the
// coordinator reacts to the first signal and ends the match. This is the hard
// signal; connection-aware callers go through SlotDisconnected, which may defer
// to this after the reconnect grace lapses.
func (m *Match) Disconnect(slot int) {
	m.closeOnce[slot].Do(func() { close(m.closed[slot]) })
}

// SlotConnected records that connID is now the live connection for slot. It
// cancels any reconnect-grace timer in flight (the slot came back), and bumping
// graceGen invalidates a grace timer that may already have fired into its
// callback but not yet taken the lock. connID may be empty (legacy/test).
func (m *Match) SlotConnected(slot int, connID string) {
	m.connMu.Lock()
	defer m.connMu.Unlock()
	m.activeConn[slot] = connID
	m.graceGen[slot]++
	if m.graceTimer[slot] != nil {
		m.graceTimer[slot].Stop()
		m.graceTimer[slot] = nil
	}
}

// SlotDisconnected handles a disconnect signal for slot from connection connID.
//
//   - A non-empty connID that does not match the slot's live connection is a
//     stale or redelivered disconnect (e.g. the durable queue replaying an old
//     message after a takeover, or a blip's disconnect arriving after the player
//     already reconnected). It is ignored — that connection is no longer current.
//   - Otherwise the connection is retired (activeConn cleared) and, if a grace
//     window is configured, a timer is armed to declare the slot gone unless it
//     re-attaches first. With no grace window the slot is declared gone at once.
func (m *Match) SlotDisconnected(slot int, connID string) {
	m.connMu.Lock()
	if connID != "" && connID != m.activeConn[slot] {
		m.connMu.Unlock()
		return
	}
	m.activeConn[slot] = ""
	m.graceGen[slot]++
	gen := m.graceGen[slot]
	if m.graceTimer[slot] != nil {
		m.graceTimer[slot].Stop()
		m.graceTimer[slot] = nil
	}
	grace := m.disconnectGrace
	if grace <= 0 {
		m.connMu.Unlock()
		m.Disconnect(slot)
		return
	}
	m.graceTimer[slot] = time.AfterFunc(grace, func() {
		m.connMu.Lock()
		// Fire only if no (dis)connect intervened since we armed this timer.
		expired := m.graceGen[slot] == gen && m.activeConn[slot] == ""
		m.connMu.Unlock()
		if expired {
			m.Disconnect(slot)
		}
	})
	m.connMu.Unlock()
}
