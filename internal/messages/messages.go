// Package messages defines the versioned contract carried over RabbitMQ:
// the work jobs, the domain events, and the topology names. Producers and
// consumers in different services share only this package.
package messages

import (
	"pokearena/internal/engine"
)

// RabbitMQ topology.
const (
	ExchangeWork   = "pokearena.work"   // direct: competing-consumer work queues
	ExchangeEvents = "pokearena.events" // topic: domain events, routing key {event}.{battleID}

	QueueQuickSim    = "quicksim.jobs"      // full AI-vs-AI battle jobs
	QueueAI          = "ai.jobs"            // AI decision requests
	QueueLeaderboard = "leaderboard.events" // durable consumer of battle.completed
	QueueLiveSession = "live.session.jobs"  // start a live-battle coordinator (one owner wins)
)

// Live-battle channel routing-key prefixes on the work and events exchanges.
// Per-battle queues/bindings are derived by appending the battle id (and, for
// frames, the slot). The session owner consumes actions; gateways bind frames.
const (
	// RKLiveAction is the work-exchange routing key for inbound player actions:
	// "live.action.{battleID}". The session owner declares a durable queue bound
	// to this key and consumes with manual ack — actions must not be lost.
	RKLiveAction = "live.action."
	// RKLiveFrame is the events-exchange routing-key prefix for outbound per-slot
	// frames: "live.frame.{battleID}.{slot}". The gateway holding a slot's socket
	// binds its key and forwards bytes to the WebSocket. Frame loss is tolerable
	// (the client resyncs from persisted state).
	RKLiveFrame = "live.frame."
)

// LiveActionKey returns the action routing key for a battle.
func LiveActionKey(battleID string) string { return RKLiveAction + battleID }

// LiveFrameKey returns the per-slot frame routing key. slot is "p1" | "p2".
func LiveFrameKey(battleID, slot string) string {
	return RKLiveFrame + battleID + "." + slot
}

// Event type names. Routing keys are "{eventType}.{battleID}", so a consumer
// can bind one battle ("*.<id>") or every battle of a type ("battle-completed.*").
const (
	EventBattleStarted   = "battle-started"
	EventTurnResolved    = "turn-resolved"
	EventAIDecided       = "ai-decided"
	EventBattleCompleted = "battle-completed"
)

// --- work jobs ---

// QuickSimJob asks a battle-worker to simulate an entire AI-vs-AI battle.
//
// P1Team / P2Team are the bare dex-number lineups (the persisted battle
// record). P1Picks / P2Picks, when present, carry the per-Pokémon movesets
// and abilities the operator chose in the builder; the worker prefers them
// and only falls back to default movesets from the bare lineup when they
// are empty (older jobs, or the randomize-and-go path without edits).
type QuickSimJob struct {
	BattleID string            `json:"battle_id"`
	Seed     uint64            `json:"seed"`
	P1Name   string            `json:"p1_name"`
	P2Name   string            `json:"p2_name"`
	P1Team   []int             `json:"p1_team"`
	P2Team   []int             `json:"p2_team"`
	P1Picks  []engine.TeamPick `json:"p1_picks,omitempty"`
	P2Picks  []engine.TeamPick `json:"p2_picks,omitempty"`
}

// LiveSessionStart is published by the gateway when a live / live_pvp battle is
// created. It rides the work exchange on QueueLiveSession, so the competing-
// consumer semantics elect exactly one battle-session instance as the owner of
// that battle's coordinator (the same pattern Quick Sim uses for quicksim.jobs).
//
// Everything the coordinator needs to reconstruct the match deterministically
// travels in the job: the seed, the trainer names, and which slots are remote
// (ws) vs in-process AI. AITeam carries the pre-picked roster for the AI slot in
// "live" mode so the opponent is identical to what the gateway would have built.
type LiveSessionStart struct {
	BattleID string            `json:"battle_id"`
	Mode     string            `json:"mode"` // "live" | "live_pvp"
	Seed     uint64            `json:"seed"`
	P1Name   string            `json:"p1_name"`
	P2Name   string            `json:"p2_name"`
	Kinds    [2]string         `json:"kinds"`             // "ws" | "ai" per slot
	AITeam   []engine.TeamPick `json:"ai_team,omitempty"` // the AI slot's roster ("live" mode)
}

// LiveAction is one inbound player message relayed from a gateway WS bridge to
// the session owner over the durable action queue. Turn makes redelivery
// idempotent: the owner ignores an action for an already-resolved turn rather
// than double-applying it. Phase selects which field is meaningful.
type LiveAction struct {
	BattleID string            `json:"battle_id"`
	Slot     string            `json:"slot"`             // "p1" | "p2"
	Turn     int               `json:"turn"`             // for idempotent dedup
	Phase    string            `json:"phase"`            // "submit" | "action" | "disconnect"
	Picks    []engine.TeamPick `json:"picks,omitempty"`  // Phase == "submit"
	Action   engine.Action     `json:"action,omitempty"` // Phase == "action"
}

// Live action phases.
const (
	LivePhaseSubmit     = "submit"
	LivePhaseAction     = "action"
	LivePhaseDisconnect = "disconnect"
)

// AIJob asks the ai-service to choose an action for one side of a live battle.
// JobID correlates the request with its AIDecided reply.
type AIJob struct {
	JobID    string `json:"job_id"`
	BattleID string `json:"battle_id"`
	Turn     int    `json:"turn"`
	Side     int    `json:"side"`
}

// --- domain events ---

// BattleStarted announces a battle's simulation/turn loop has begun.
type BattleStarted struct {
	BattleID string `json:"battle_id"`
}

// TurnResolved carries one resolved turn: its log and the full post-turn state.
type TurnResolved struct {
	BattleID string              `json:"battle_id"`
	Turn     int                 `json:"turn"`
	Log      []engine.LogLine    `json:"log"`
	State    *engine.BattleState `json:"state"`
}

// AIDecided returns the AI's chosen action for an AIJob.
type AIDecided struct {
	JobID     string        `json:"job_id"`
	BattleID  string        `json:"battle_id"`
	Turn      int           `json:"turn"`
	Side      int           `json:"side"`
	Action    engine.Action `json:"action"`
	Reasoning string        `json:"reasoning,omitempty"`
}

// BattleCompleted announces a finished battle. The leaderboard-worker reloads
// the authoritative record from Postgres; Winner is included for live push.
type BattleCompleted struct {
	BattleID  string `json:"battle_id"`
	Winner    int    `json:"winner"`
	TurnCount int    `json:"turn_count"`
}
