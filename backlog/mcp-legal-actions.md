# MCP: expose the legal-action list on the view

**Status:** not started

**Why:** Today the agent reconstructs the legal action set from
`phase`, `replace`, the active Pokémon's `moves[*].pp`, and which team
members are unfainted. The logic to do this *correctly* already exists
server-side as `ai.legalActions(view)` (`internal/ai/agent.go`) — it's
used by the in-process harness AI but not exposed over the MCP surface.
So the agent reimplements it from the view, badly, and the gateway
catches the failures with `errNotYourTurn` / illegal-action rejections.

This is a small but high-value protocol fix:

- Eliminates a whole class of "I called act() but it bounced" loops.
- Removes ambiguity about Struggle (`index: -1` when all PP is gone) —
  the agent doesn't need to know that convention, the legal list just
  shows it.
- Makes replace-phase semantics self-documenting: only switches appear.

**Scope:**
- Add `legal_actions []Action` to `View` in `internal/ai/agent.go`,
  populated by calling the existing `legalActions(v)` at projection time
  inside `MakeView`.
- The `Action` shape is already on the wire (`engine.Action{Kind,Index}`
  via the client-side `WsClientMsg`), so this is purely additive.
- No engine change; no new validation. The gateway's existing
  illegal-action rejection stays as the authoritative gate — the view
  field is a courtesy, not a contract the server promises to honor in
  the face of a stale snapshot.

**Wire-format note:** `MakeView` is called from the engine package and
the result is serialized to clients. Including the legal set roughly
doubles the payload during `PhaseReplace` (only switches) but is a
small addition during `PhaseChoosing`. Worth the bytes.

**Acceptance:** An agent that only ever picks from `view.legal_actions`
never receives an illegal-action error from the gateway. Replace phase,
post-faint switch, and all-PP-zero Struggle all surface as items in the
list with no special-casing on the client.

**Depends on:** nothing.
**Pairs well with:** [[mcp-move-metadata]] (annotate each `move`-kind
action with BP/type) and [[mcp-damage-prediction]] (annotate each move
action with a damage-range estimate). Each is independently useful.
