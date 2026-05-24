# Pv-Claude

**Status:** acceptance met. End-to-end works — a Claude Code session can
join a battle and play it to completion against a human in the browser.
The one production-readiness item still open is disconnect handling.

**Why:** Differentiator. Every Pokémon sim has PvE and PvP. None lets you play against
an external agent over a stable protocol. Shows off: fog-of-war by construction, the
gateway treating any trainer-slot client uniformly, MCP as an extension surface.

## Acceptance

- ✓ Human starts a `live_pvp`-style battle from the web UI and gets a `battle_id` + a
  join URL for the second slot.
- ✓ Claude Code (or any MCP client) connects via the MCP server, sees only its own side
  of the field, and plays the battle to completion. Verified by `cmd/mcp-smoke`.
- ☐ Disconnects and idle-outs forfeit cleanly; no hung battles.
  Today a drop aborts the match cleanly but ungracefully (the remaining
  player sees an abort, not a forfeit banner with a winner).
  Tracked in [[disconnect-detection]].

## How to use it

```bash
# 1. bring up the stack
docker compose up -d

# 2. wire pokearena-mcp into your Claude Code session
claude mcp add pokearena -- go run /absolute/path/to/poke-sys-design/cmd/pokearena-mcp

# 3. in a browser, create a live_pvp battle and copy the p2 join URL
#    (UI shows it after "Pv-Player — share a link to play")

# 4. tell Claude: "join_battle with battle_id=… slot=p2 join_token=… and play"
```

The MCP server runs on the user's machine, not in our cloud — see
[[mcp-server]] §2 for why this matters architecturally.

## Where we are

- ✓ [[gateway-second-slot]] — the gateway accepts any WS client on the open slot.
- ✓ [[join-token-security]] — token minting, atomic claim, opaque errors.
- ✓ [[mcp-server]] — `pokearena-mcp` binary with join/view/wait/act/leave.
- ☐ [[disconnect-detection]] — the last must-have before strangers play.

**Nice-to-have, not blocking:** [[team-builder-moves]], [[claude-team-drafting]].
