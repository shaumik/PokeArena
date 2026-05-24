package mcpserver

import (
	"context"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"pokearena/internal/ai"
)

// errNotImplemented is what every tool returns in the skeleton commit.
// The shape is deliberate: agents see a clean MCP error with a stable
// code, not a panic or a silent empty result.
var errNotImplemented = errors.New("pokearena-mcp: not implemented yet — see backlog/mcp-server.md")

// Input/output types for each tool. JSON tags + jsonschema descriptions
// drive the schema the MCP client sees, and the descriptions are what
// the LLM reads when deciding which tool to call — they're prompts,
// not docs, and worth treating as such.

type joinBattleIn struct {
	BattleID string `json:"battle_id" jsonschema:"the battle's UUID, as printed by the gateway when the battle was created"`
	Slot     string `json:"slot" jsonschema:"which trainer slot to claim: 'p1' or 'p2'"`
	Token    string `json:"join_token" jsonschema:"the per-slot join token; treat as a password — never log it"`
}

type joinBattleOut struct {
	BattleID        string  `json:"battle_id"`
	Slot            string  `json:"slot"`
	YourTrainer     string  `json:"your_trainer"`
	OpponentTrainer string  `json:"opponent_trainer"`
	View            ai.View `json:"initial_view"`
}

// viewIn and waitIn carry no arguments today; the session knows which
// battle it's bound to. A typed empty struct is preferred over `any`
// because the SDK then generates a proper empty-object schema.
type viewIn struct{}

type waitIn struct {
	TimeoutSeconds int `json:"timeout_seconds,omitempty" jsonschema:"how long to block waiting for your turn; clamped to [1,120], default 60"`
}

type waitOut struct {
	Ready    bool     `json:"ready"`              // false on timeout; true on your-turn or battle-end
	Terminal bool     `json:"terminal,omitempty"` // true iff the battle just ended
	View     *ai.View `json:"view,omitempty"`     // set when Ready is true
}

type actIn struct {
	Kind  string `json:"kind" jsonschema:"'move' to use the active Pokémon's move at Index, or 'switch' to swap to the team member at Index"`
	Index int    `json:"index" jsonschema:"move slot 0..3 for kind=move, or team slot 0..5 for kind=switch"`
}

type actOut struct {
	Accepted bool `json:"accepted"`
	Turn     int  `json:"turn"`
}

type leaveIn struct{}

type leaveOut struct {
	OK bool `json:"ok"`
}

// registerTools wires every tool's schema + handler onto the underlying
// MCP server. Each handler currently returns errNotImplemented; commit 2
// replaces these bodies with real gateway-WS calls without touching
// signatures, so the tool surface is frozen here.
func (s *Server) registerTools() {
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name: "join_battle",
		Description: "Bind this MCP session to a battle slot. Opens a WebSocket to the gateway " +
			"and returns the initial fog-of-war view. Call this first; every other tool requires it.",
	}, s.joinBattle)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name: "view",
		Description: "Return the current fog-of-war view of the joined battle. Non-blocking. " +
			"Prefer wait() between turns; use view() only when you need the latest snapshot now.",
	}, s.viewBattle)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name: "wait",
		Description: "Block until it's your turn, the battle ends, or the timeout elapses. " +
			"This is the primary loop primitive — call wait, then act, then wait again.",
	}, s.waitForTurn)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name: "act",
		Description: "Submit your chosen action for the current turn. Validate against the " +
			"legal actions implied by the latest view before calling; the gateway will reject illegal actions.",
	}, s.actBattle)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name: "leave_battle",
		Description: "Cleanly close the WebSocket and release the session. If the battle is " +
			"still in progress this is a forfeit. After this, join_battle can be called again.",
	}, s.leaveBattle)
}

// All five handlers below are stubs: identical bodies, distinct
// signatures so the surface is real and typed. Commit 2 fills them in.

func (s *Server) joinBattle(_ context.Context, _ *mcp.CallToolRequest, _ joinBattleIn) (*mcp.CallToolResult, joinBattleOut, error) {
	return nil, joinBattleOut{}, errNotImplemented
}

func (s *Server) viewBattle(_ context.Context, _ *mcp.CallToolRequest, _ viewIn) (*mcp.CallToolResult, ai.View, error) {
	return nil, ai.View{}, errNotImplemented
}

func (s *Server) waitForTurn(_ context.Context, _ *mcp.CallToolRequest, _ waitIn) (*mcp.CallToolResult, waitOut, error) {
	return nil, waitOut{}, errNotImplemented
}

func (s *Server) actBattle(_ context.Context, _ *mcp.CallToolRequest, _ actIn) (*mcp.CallToolResult, actOut, error) {
	return nil, actOut{}, errNotImplemented
}

func (s *Server) leaveBattle(_ context.Context, _ *mcp.CallToolRequest, _ leaveIn) (*mcp.CallToolResult, leaveOut, error) {
	return nil, leaveOut{}, errNotImplemented
}
