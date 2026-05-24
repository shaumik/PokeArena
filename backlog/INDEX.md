# Backlog

One file per action item. Edit liberally; delete when done.

## Active

### Pv-Claude hardening (acceptance met; production-readiness pending)
- [disconnect-detection](disconnect-detection.md) — slot-WS drop → 30s grace
  → forfeit. Today a drop aborts the match cleanly but ungracefully.

### Nice-to-have
- [team-builder-moves](team-builder-moves.md) — pick 4 moves explicitly in UI
- [claude-team-drafting](claude-team-drafting.md) — Claude proposes teams via MCP

## Done

- [pv-claude](pv-claude.md) — umbrella: human vs Claude Code over MCP.
  End-to-end works: `claude mcp add pokearena -- go run ./cmd/pokearena-mcp`
  then any battle's p2 join URL plays through Claude.
  Smoke test at `cmd/mcp-smoke`.
- [mcp-server](mcp-server.md) — `pokearena-mcp` binary with all five
  tools (join_battle / view / wait / act / leave_battle). 4-commit
  ladder landed; 12 unit tests; one stdio smoke test.
- [gateway-second-slot](gateway-second-slot.md) — claimable second-trainer WS slot;
  two browser tabs play each other end-to-end. Smoke test at `cmd/pvp-smoke`.
- [join-token-security](join-token-security.md) — first-claim-wins, 32-byte CSPRNG
  token, atomic Lua claim, opaque error to client. Disconnect-grace still pending
  in [[disconnect-detection]].
