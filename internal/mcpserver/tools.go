package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"pokearena/internal/ai"
	"pokearena/internal/engine"
)

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
	BattleID        string `json:"battle_id"`
	Slot            string `json:"slot"`
	YourTrainer     string `json:"your_trainer"`
	OpponentTrainer string `json:"opponent_trainer"`
	// Phase is one of "open" (picker — call submit_team next),
	// "starting" (transient), or "active" (battle running — View is set).
	Phase string   `json:"phase"`
	View  *ai.View `json:"initial_view,omitempty"`
}

type submitTeamIn struct {
	Picks []engine.TeamPick `json:"picks" jsonschema:"exactly 6 entries; each carries dex_no, 1-4 move IDs from that species' learn list, and an optional ability slug from get_pokemon.abilities (empty = use slot 0)"`
}

type submitTeamOut struct {
	Accepted bool `json:"accepted"`
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

// findPokemonIn / findPokemonOut: cheap discovery. Substring match against
// species names; output is the lightweight tuple agents need to decide
// which species to fetch in detail. Capped to keep output bounded as the
// dataset grows.
const findPokemonCap = 30

type findPokemonIn struct {
	Query string `json:"query" jsonschema:"case-insensitive substring of the species name; empty string returns the first 30 entries"`
}

type pokemonRef struct {
	DexNo int    `json:"dex_no"`
	Name  string `json:"name"`
	Type1 string `json:"type1"`
	Type2 string `json:"type2,omitempty"`
}

type findPokemonOut struct {
	Matches []pokemonRef `json:"matches"`
	Total   int          `json:"total_in_dex"`     // total species in the dataset
	Capped  bool         `json:"capped,omitempty"` // true if more matches existed beyond the cap
}

// getPokemonIn / getPokemonOut: full species detail for one dex number.
// Used after find_pokemon narrows the field; the moves array is the
// authoritative learn list for submit_team.
type getPokemonIn struct {
	DexNo int `json:"dex_no" jsonschema:"the species' Pokédex number, e.g. 25 for Pikachu"`
}

type getPokemonOut struct {
	DexNo     int           `json:"dex_no"`
	Name      string        `json:"name"`
	Type1     string        `json:"type1"`
	Type2     string        `json:"type2,omitempty"`
	Base      dexBaseStats  `json:"base"`
	Abilities []string      `json:"abilities,omitempty"` // ability slugs; index 0 is the default if submit_team omits the ability field
	Moves     []dexMoveInfo `json:"moves"`               // legal moves for this species; use the .id values in submit_team picks
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
		Name: "submit_team",
		Description: "Submit your team during the picker (OPEN) phase. Required after join_battle " +
			"if the returned phase is 'open'; ignored once the battle is 'active'. Each pick is " +
			"{dex_no, moves: [...], ability?} — 1-4 legal moves from that species' learn list, " +
			"plus an optional ability slug from get_pokemon.abilities (omit to default to slot 0). " +
			"Move and ability IDs must match exactly (kebab-case: 'body-slam', 'flash-fire'). Blocks " +
			"until the server accepts (returns accepted=true) or rejects (returns an error).",
	}, s.submitTeam)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name: "find_pokemon",
		Description: "Search the curated Pokédex by name substring. Returns lightweight matches " +
			"(dex_no + name + types). The dataset is filtered to a subset — not every species " +
			"is present. Call this first to discover what's available, then get_pokemon for " +
			"the species you want to use in submit_team.",
	}, s.findPokemon)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name: "get_pokemon",
		Description: "Fetch full details for one species by dex_no, including its legal move list " +
			"and ability slots. The move .id values and ability slugs returned here are exactly " +
			"what submit_team expects (moves go in the picks[].moves array; the ability goes in " +
			"the optional picks[].ability field — omit to default to abilities[0]).",
	}, s.getPokemon)

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

// Handlers are thin: marshal in, call session, marshal out. All state
// lives in s.session (see session.go). When the session returns an
// error, the SDK turns it into a {isError: true, content: <message>}
// MCP result automatically.

func (s *Server) joinBattle(ctx context.Context, _ *mcp.CallToolRequest, in joinBattleIn) (*mcp.CallToolResult, joinBattleOut, error) {
	out, err := s.session.Join(ctx, in.BattleID, in.Slot, in.Token)
	return nil, out, err
}

func (s *Server) viewBattle(_ context.Context, _ *mcp.CallToolRequest, _ viewIn) (*mcp.CallToolResult, ai.View, error) {
	v, err := s.session.View()
	return nil, v, err
}

func (s *Server) waitForTurn(ctx context.Context, _ *mcp.CallToolRequest, in waitIn) (*mcp.CallToolResult, waitOut, error) {
	out, err := s.session.Wait(ctx, in.TimeoutSeconds)
	return nil, out, err
}

func (s *Server) submitTeam(_ context.Context, _ *mcp.CallToolRequest, in submitTeamIn) (*mcp.CallToolResult, submitTeamOut, error) {
	if err := s.session.SubmitTeam(in.Picks); err != nil {
		return nil, submitTeamOut{}, err
	}
	return nil, submitTeamOut{Accepted: true}, nil
}

func (s *Server) actBattle(_ context.Context, _ *mcp.CallToolRequest, in actIn) (*mcp.CallToolResult, actOut, error) {
	out, err := s.session.Act(in.Kind, in.Index)
	return nil, out, err
}

func (s *Server) leaveBattle(_ context.Context, _ *mcp.CallToolRequest, _ leaveIn) (*mcp.CallToolResult, leaveOut, error) {
	if err := s.session.Leave(); err != nil {
		return nil, leaveOut{}, err
	}
	return nil, leaveOut{OK: true}, nil
}

func (s *Server) findPokemon(ctx context.Context, _ *mcp.CallToolRequest, in findPokemonIn) (*mcp.CallToolResult, findPokemonOut, error) {
	dex, err := s.fetchDex(ctx)
	if err != nil {
		return nil, findPokemonOut{}, fmt.Errorf("fetch dex: %w", err)
	}
	q := strings.ToLower(strings.TrimSpace(in.Query))
	matches := make([]pokemonRef, 0, findPokemonCap)
	capped := false
	for _, e := range dex {
		if q != "" && !strings.Contains(strings.ToLower(e.Name), q) {
			continue
		}
		if len(matches) >= findPokemonCap {
			capped = true
			break
		}
		matches = append(matches, pokemonRef{DexNo: e.DexNo, Name: e.Name, Type1: e.Type1, Type2: e.Type2})
	}
	return nil, findPokemonOut{Matches: matches, Total: len(dex), Capped: capped}, nil
}

func (s *Server) getPokemon(ctx context.Context, _ *mcp.CallToolRequest, in getPokemonIn) (*mcp.CallToolResult, getPokemonOut, error) {
	dex, err := s.fetchDex(ctx)
	if err != nil {
		return nil, getPokemonOut{}, fmt.Errorf("fetch dex: %w", err)
	}
	for _, e := range dex {
		if e.DexNo == in.DexNo {
			return nil, getPokemonOut{
				DexNo: e.DexNo, Name: e.Name,
				Type1: e.Type1, Type2: e.Type2,
				Base:      e.Base,
				Abilities: e.Abilities,
				Moves:     e.Moves,
			}, nil
		}
	}
	return nil, getPokemonOut{}, errors.New("no species with that dex_no in the curated dataset (call find_pokemon to list what's available)")
}
