# Pv-Claude

**Status:** designing

**Why:** Differentiator. Every Pokémon sim has PvE and PvP. None lets you play against
an external agent over a stable protocol. Shows off: fog-of-war by construction, the
gateway treating any trainer-slot client uniformly, MCP as an extension surface.

**Acceptance:**
- Human starts a `live_pvp`-style battle from the web UI and gets a `battle_id` + a
  join URL for the second slot.
- Claude Code (or any MCP client) connects via the MCP server, sees only its own side
  of the field, and plays the battle to completion.
- Disconnects and idle-outs forfeit cleanly; no hung battles.

**Depends on:** [[mcp-server]], [[gateway-second-slot]], [[disconnect-detection]].

**Nice-to-have, not blocking:** [[team-builder-moves]], [[claude-team-drafting]].
