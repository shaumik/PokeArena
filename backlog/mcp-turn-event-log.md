# MCP: surface the per-turn event log to the agent

**Status:** not started

**Why:** Today the agent sees only a `BattleView` snapshot before/after each
turn and has to reverse-engineer what happened by diffing HP/PP/active-index
fields against the previous snapshot. That breaks in real cases:

- Foe switches Active A out for Active B and the attacker hits B on the
  switch-in — looks identical to "A fainted, B replaced" if you only diff
  the snapshot. (Seen in practice: an agent claimed a KO that never
  happened, then mis-modeled the rest of the battle off that.)
- A miss vs a low damage roll is invisible — PP changes are the same.
- Status procs (burn from flamethrower, paralysis from body-slam) are
  inferrable only after the next turn, and only if the agent thinks to look.
- Foe choice of move is fully hidden unless PP happens to decrement
  exactly one slot.

**The fix is almost free** — the log already exists end to end and is
silently dropped at the last hop:

- `engine/turn.go` emits typed `LogLine{Type, Side, Text}` entries
  (`move`, `damage`, `crit`, `effective`, `resisted`, `miss`, `immune`,
  `switch`, `faint`, `status`, `stat`, `heal`, `recoil`, `fail`, `win`).
- `protocol.MatchUpdate` already carries `Log []engine.LogLine` over the
  WS.
- `internal/mcpserver/session.go` dispatch loop (`case FrameTurn`)
  retains `u.View` but discards `u.Log`.

**Scope:**
- Session keeps the log from the most recent turn frame (`s.recentLog`),
  cleared on `Act` so each `Wait` returns only events since the last
  agent decision. Replace-phase frames may produce multiple log batches
  before the agent acts; accumulate, don't overwrite.
- Add `recent_log []LogLine` to `waitOut` (and to `viewOut` for the
  non-blocking path). Empty array when nothing happened (initial join,
  timeout wakes).
- `LogLine.Type` is the machine-readable discriminator; `Text` stays for
  rendering. Document the type vocabulary in
  [`docs/mcp-protocol.md`](../docs/mcp-protocol.md) so agents can switch
  on it without reading engine source.

**Explicitly out of scope:** turning `LogLine` into a richer structured
event (`{type:"damage", target_side, target_slot, amount, source_move}`).
The text-only form is enough to fix the diff-the-snapshot problem; a
structured-event refactor is a separate, larger change and can come
later if agents end up parsing `Text` with regexes.

**Acceptance:** A `wait()` return after a turn where the foe switched and
the agent's move hit the switch-in contains, in order, at least: a
`switch` line for the foe leaving, a `switch` line for the foe entering,
a `move` line for the agent's attack, and a `damage` line naming the
switched-in Pokémon. An agent can determine "did the foe switch?" from
the events without diffing snapshots.

**Depends on:** nothing — the data is already flowing.
**Required by:** [[mcp-wait-semantics]] (clearer wake semantics is less
useful if the agent still can't reconstruct the previous turn).
