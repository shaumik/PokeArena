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
	QueueTurn        = "turn.jobs"          // single live-turn resolution jobs
	QueueAI          = "ai.jobs"            // AI decision requests
	QueueLeaderboard = "leaderboard.events" // durable consumer of battle.completed
)

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
type QuickSimJob struct {
	BattleID     string `json:"battle_id"`
	Seed         uint64 `json:"seed"`
	P1Name       string `json:"p1_name"`
	P2Name       string `json:"p2_name"`
	P1Team       []int  `json:"p1_team"`
	P2Team       []int  `json:"p2_team"`
	P1Difficulty string `json:"p1_difficulty"`
	P2Difficulty string `json:"p2_difficulty"`
}

// AIJob asks the ai-service to choose an action for one side of a live battle.
type AIJob struct {
	BattleID   string `json:"battle_id"`
	Turn       int    `json:"turn"`
	Side       int    `json:"side"`
	Difficulty string `json:"difficulty"`
}

// TurnJob asks a battle-worker to resolve one turn of a live battle — emitted
// only once both sides' actions are in.
type TurnJob struct {
	BattleID string `json:"battle_id"`
	Turn     int    `json:"turn"`
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

// AIDecided announces the AI picked an action (with optional reasoning).
type AIDecided struct {
	BattleID  string `json:"battle_id"`
	Turn      int    `json:"turn"`
	Side      int    `json:"side"`
	Reasoning string `json:"reasoning,omitempty"`
}

// BattleCompleted announces a finished battle. The leaderboard-worker reloads
// the authoritative record from Postgres; Winner is included for live push.
type BattleCompleted struct {
	BattleID  string `json:"battle_id"`
	Winner    int    `json:"winner"`
	TurnCount int    `json:"turn_count"`
}
