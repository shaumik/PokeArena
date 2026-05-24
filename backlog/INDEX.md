# Backlog

Action items only. One file per item; edit liberally; delete when done.
Stable design contracts live in [`docs/`](../docs/) and are referenced
from the README — keep them out of here.

## Active

### Hardening (production-readiness, not blocking the demo)
- [disconnect-detection](disconnect-detection.md) — slot-WS drop → 30s
  grace → forfeit. Today a drop aborts the match cleanly but
  ungracefully. The last must-have before strangers play.

### Nice-to-have
- [team-builder-moves](team-builder-moves.md) — pick 4 moves explicitly in UI
- [claude-team-drafting](claude-team-drafting.md) — Claude proposes teams via MCP

### MCP agent surface (raised by a Claude session that mis-tracked turn state)
- [mcp-turn-event-log](mcp-turn-event-log.md) — forward the already-emitted
  `LogLine` stream to the agent; today the dispatcher drops it. Highest
  leverage of the bunch.
- [mcp-legal-actions](mcp-legal-actions.md) — expose `ai.legalActions(view)`
  on `View` so agents stop reconstructing it (badly).
- [mcp-move-metadata](mcp-move-metadata.md) — include BP / accuracy / type
  / category on each `MoveSlot` in the view.
- [mcp-damage-prediction](mcp-damage-prediction.md) — per-legal-move
  damage range + OHKO chance, computed from the engine's own formula.
- [mcp-wait-semantics](mcp-wait-semantics.md) — `awaiting`+`simultaneous`
  on `waitOut`; stop waking the agent on opponent-only replace turns.

### Engine + content (future)
- [ability](ability.md)
- [animation](animation.md)
- [attacks-secondary-effects](attacks-secondary-effects.md)
- [bgx-sound](bgx-sound.md)
- [ev-stat-points](ev-stat-points.md)
- [status-condition](status-condition.md)
- [weather-condition](weather-condition.md)
