// Package protocol defines the on-the-wire shapes for the gateway↔client
// WebSocket protocol used by live_pvp battles. Both the gateway (server
// side) and the MCP server / future CLI / future RL trainer (client side)
// import this package so the contract has exactly one definition.
//
// This is deliberately *only* the wire shapes — no I/O, no validation
// beyond what the JSON tags express. The protocol is the contract;
// MCP, stdio CLIs, and HTTP clients are presentation layers.
package protocol

import (
	"pokearena/internal/ai"
	"pokearena/internal/engine"
)

// Frame types sent server → client. The MatchUpdate.Type field carries
// one of these. Code that produces or consumes frames should use these
// constants rather than string literals so a typo is a build error.
const (
	FrameState = "state" // initial snapshot when both slots have attached
	FrameTurn  = "turn"  // result of a resolved turn (log + new view)
	FrameEnd   = "end"   // battle over (winner + final view)
	FrameError = "error" // non-fatal protocol or validation error
	FrameInfo  = "info"  // human-readable status ("Waiting for opponent…")
)

// Action kinds sent client → server. WsClientMsg.Kind carries one of these.
const (
	ActionKindMove   = "move"
	ActionKindSwitch = "switch"
)

// MatchUpdate is the gateway → client frame for a live_pvp battle. The
// JSON tags are part of the contract; do not rename them without a
// coordinated change in every client.
type MatchUpdate struct {
	Type    string           `json:"type"` // one of the Frame* constants above
	View    *ai.View         `json:"view,omitempty"`
	Log     []engine.LogLine `json:"log,omitempty"`
	Winner  *int             `json:"winner,omitempty"` // 0 or 1 on FrameEnd
	Turn    int              `json:"turn,omitempty"`
	Message string           `json:"message,omitempty"` // populated on FrameError, FrameInfo
}

// WsClientMsg is the client → gateway frame. v1 has exactly one Type
// ("action"); the Kind field then distinguishes move vs switch.
type WsClientMsg struct {
	Type  string `json:"type"`  // currently always "action"
	Kind  string `json:"kind"`  // one of the ActionKind* constants
	Index int    `json:"index"` // move slot 0..3 for move; team slot 0..5 for switch
}

// PlayPath builds the WebSocket join path for a live_pvp slot. Both the
// gateway (issuing URLs to the battle creator) and any client that
// constructs its own connect URL (the MCP server building from a
// battle_id + token pair) call this so the shape stays in lockstep.
//
// Returns the path only, not the scheme/host — callers prepend their
// origin or gateway base URL.
func PlayPath(battleID, slot, token string) string {
	return "/api/battles/" + battleID + "/play?slot=" + slot + "&token=" + token
}
