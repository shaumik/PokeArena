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
	FrameState = "state" // initial snapshot when the battle becomes ACTIVE
	FrameTurn  = "turn"  // result of a resolved turn (log + new view)
	FrameEnd   = "end"   // battle over (winner + final view)
	FrameError = "error" // non-fatal protocol or validation error
	FrameInfo  = "info"  // human-readable status ("Waiting for opponent…")
	FrameRoom  = "room"  // picker-room state update (open / starting)
)

// Client → server message types. WsClientMsg.Type carries one of these.
const (
	MsgAction     = "action"      // ACTIVE: a move or switch
	MsgSubmitTeam = "submit_team" // OPEN: pick a team for this side
	MsgLeaveRoom  = "leave_room"  // OPEN: voluntary exit (equivalent to WS close)
)

// Action kinds sent client → server. WsClientMsg.Kind carries one of these
// when WsClientMsg.Type == MsgAction.
const (
	ActionKindMove   = "move"
	ActionKindSwitch = "switch"
)

// RoomPhase is the high-level phase carried in a FrameRoom payload. The
// transition to ACTIVE is signalled by the *next* frame being a
// FrameState, not by a separate "active" RoomPhase value.
type RoomPhase string

const (
	RoomPhaseOpen     RoomPhase = "open"     // accepting attaches + submit_team
	RoomPhaseStarting RoomPhase = "starting" // both submitted; engine spinning up
)

// RoomSlot is one side's progress inside the picker room. From the
// receiving slot's perspective: "you" is themselves, "them" is the
// opponent. Trainer is non-strategic — purely for the SPA's "vs Red"
// label, gated by §6 fog-of-war (which does not redact names).
type RoomSlot struct {
	Attached  bool   `json:"attached"`
	Submitted bool   `json:"submitted"`
	Trainer   string `json:"trainer,omitempty"`
}

// RoomUpdate is the payload of a FrameRoom. The gateway broadcasts one
// on every state change (attach, submit, transition) and re-broadcasts
// on entry to STARTING so clients can resync the timer.
type RoomUpdate struct {
	Phase      RoomPhase `json:"phase"`
	You        RoomSlot  `json:"you"`
	Them       RoomSlot  `json:"them"`
	DeadlineMS int64     `json:"deadline_ms"` // ms remaining on the room's 300s deadline
}

// MatchUpdate is the gateway → client frame for a live_pvp battle. The
// JSON tags are part of the contract; do not rename them without a
// coordinated change in every client.
type MatchUpdate struct {
	Type    string           `json:"type"` // one of the Frame* constants above
	View    *ai.View         `json:"view,omitempty"`
	Log     []engine.LogLine `json:"log,omitempty"`
	Winner  *int             `json:"winner,omitempty"` // 0 or 1 on FrameEnd
	Turn    int              `json:"turn,omitempty"`
	Message string           `json:"message,omitempty"`  // FrameError, FrameInfo
	Room    *RoomUpdate      `json:"room,omitempty"`     // FrameRoom only
}

// WsClientMsg is the client → gateway frame. Type discriminates the
// shape: MsgAction reads Kind+Index, MsgSubmitTeam reads Picks,
// MsgLeaveRoom reads nothing more. Unused fields are simply ignored.
type WsClientMsg struct {
	Type  string            `json:"type"`
	Kind  string            `json:"kind,omitempty"`
	Index int               `json:"index,omitempty"`
	Picks []engine.TeamPick `json:"picks,omitempty"`
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
