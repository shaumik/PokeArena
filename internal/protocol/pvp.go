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
	"encoding/json"

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
	FrameAI    = "ai"    // AI reasoning line streamed alongside an AI-side turn
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
// transition to ACTIVE is signaled by the *next* frame being a
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
	Winner  *int             `json:"winner,omitempty"` // on FrameEnd: 0, 1, or 2 for a draw
	Turn    int              `json:"turn,omitempty"`
	Message string           `json:"message,omitempty"` // FrameError, FrameInfo
	Room    *RoomUpdate      `json:"room,omitempty"`    // FrameRoom only

	// RawView is the exact "view" JSON as it arrived on the wire, captured on
	// unmarshal and never re-serialized (json:"-"). A relay must forward this
	// rather than re-marshal View: ai.View has no hp_pct field, so decoding a
	// server-redacted view into it drops the foe's public HP% and zeroes HP —
	// re-marshaling would then emit hp_pct:0, making every foe look fainted.
	// Forwarding RawView preserves the server's redaction byte-for-byte.
	RawView json.RawMessage `json:"-"`
}

// UnmarshalJSON decodes a MatchUpdate and additionally snapshots the raw "view"
// object into RawView, so a client that relays the view onward (the MCP server)
// can forward the server's redaction verbatim instead of losing fields through
// the typed ai.View round-trip.
func (m *MatchUpdate) UnmarshalJSON(b []byte) error {
	type alias MatchUpdate // strip UnmarshalJSON to avoid infinite recursion
	var a alias
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	*m = MatchUpdate(a)
	var probe struct {
		View json.RawMessage `json:"view"`
	}
	if err := json.Unmarshal(b, &probe); err != nil {
		return err
	}
	m.RawView = probe.View
	return nil
}

// WsClientMsg is the client → gateway frame. Type discriminates the
// shape: MsgAction reads Kind+Index, MsgSubmitTeam reads Picks,
// MsgLeaveRoom reads nothing more. Unused fields are simply ignored.
type WsClientMsg struct {
	Type  string `json:"type"`
	Kind  string `json:"kind,omitempty"`
	Index int    `json:"index,omitempty"`
	// SwitchTarget names the bench slot a self-switch move (U-turn, Volt
	// Switch, Baton Pass) should bring in. Only read when Kind is a move and
	// the chosen move self-switches. Absent means "engine picks", which it
	// does deterministically — see engine.Action.SwitchTarget.
	SwitchTarget *int              `json:"switch_target,omitempty"`
	Picks        []engine.TeamPick `json:"picks,omitempty"`
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

// LivePlayPath builds the WebSocket join path for a single-player live-mode
// battle: one human WS slot facing the programmatic AI. Live mode carries no
// slot or token query — the gateway hardcodes the human to p1 and the battle
// ID is the whole auth model (see httpapi handleLiveWS). The absence of a slot
// param is precisely what routes the gateway to the live handler instead of the
// pvp one, so this must not append one.
func LivePlayPath(battleID string) string {
	return "/api/battles/" + battleID + "/play"
}
