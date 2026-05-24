# Pv-Claude

**Status:** partially built. Gateway-side is done — two humans can play over WS.
The MCP server that lets Claude Code be one of those humans is the remaining piece.

**Why:** Differentiator. Every Pokémon sim has PvE and PvP. None lets you play against
an external agent over a stable protocol. Shows off: fog-of-war by construction, the
gateway treating any trainer-slot client uniformly, MCP as an extension surface.

**Acceptance:**
- Human starts a `live_pvp`-style battle from the web UI and gets a `battle_id` + a
  join URL for the second slot. ✓ landed.
- Claude Code (or any MCP client) connects via the MCP server, sees only its own side
  of the field, and plays the battle to completion. **pending — needs [[mcp-server]].**
- Disconnects and idle-outs forfeit cleanly; no hung battles. **pending —
  needs [[disconnect-detection]]; today a drop aborts the match.**

## Where we are

- ✓ [[gateway-second-slot]] — the gateway accepts any WS client on the open slot.
  This is the load-bearing piece: Claude Code is just "any WS client" once MCP
  is in front of it.
- ✓ [[join-token-security]] — token minting, atomic claim, opaque errors all live.
- ☐ [[mcp-server]] — the bridge from Claude's tool-call surface to our WS. Designed
  in detail; not implemented. **This is the next commit batch.**
- ☐ [[disconnect-detection]] — currently a WS drop aborts the match. Fine for v0
  demos, must-have before anyone non-friendly plays.

## Next step

Build `pokearena-mcp` per the [[mcp-server]] doc. Order of commits will mirror the
gateway-second-slot pattern: primitives → tool handlers → end-to-end script that
plays a real battle against the running gateway, similar in spirit to
`cmd/pvp-smoke`.

**Nice-to-have, not blocking:** [[team-builder-moves]], [[claude-team-drafting]].
