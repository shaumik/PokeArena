# Backlog

One file per action item. Edit liberally; delete when done.

## Active

### Critical path to Pv-Claude
- [pv-claude](pv-claude.md) — umbrella: human vs Claude Code over MCP
- [mcp-server](mcp-server.md) — `pokearena-mcp` exposing `join`/`view`/`wait`/`act`/`leave` — **next up**
- [disconnect-detection](disconnect-detection.md) — slot-WS drop → 30s grace → forfeit

### Nice-to-have (not blocking)
- [team-builder-moves](team-builder-moves.md) — pick 4 moves explicitly in UI
- [claude-team-drafting](claude-team-drafting.md) — Claude proposes teams via MCP

## Done

- [gateway-second-slot](gateway-second-slot.md) — claimable second-trainer WS slot;
  two browser tabs play each other end-to-end. Smoke test at `cmd/pvp-smoke`.
- [join-token-security](join-token-security.md) — first-claim-wins, 32-byte CSPRNG
  token, atomic Lua claim, opaque error to client. Disconnect-grace still pending
  in [[disconnect-detection]].
