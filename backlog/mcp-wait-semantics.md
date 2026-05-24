# MCP: clarify wait()/phase semantics for simultaneous turns

**Status:** not started

**Why:** The current contract is "Wait returns ready when it's your turn
to act" — fine in the abstract, but the engine resolves both sides'
choices *simultaneously* in `PhaseChoosing`, and during `PhaseReplace`
only the side(s) with a fainted active need to act. The view doesn't
distinguish three real cases an agent should care about:

1. You need to choose this turn, opponent also choosing — your call is
   **blind to their choice** (the common case).
2. You need to choose, opponent doesn't (they're mid-replace from an
   earlier faint, or it's a one-sided replace turn) — your call is
   informed.
3. Opponent needs to replace, you don't — `wait()` should not return
   `ready: true` for you, but today it sometimes does with
   `phase:"replace", replace:false`, which is confusing enough that
   agents have called `act` and gotten bounced.

An agent that knows it's choosing blind plays more defensively (avoids
fragile-to-prediction switches); an agent that thinks it has full info
will overcommit. This affected play in a real session — a Primeape
switch into a hidden Mewtwo that was decided "blind but the agent didn't
know it was blind."

**Scope:**
- Add to `waitOut`: `awaiting: "you" | "opponent" | "both"` and
  `simultaneous: bool`. The session already tracks `needsAction` per
  side via the dispatcher; computing this is cheap.
- `wait()` returns `ready: true` only when *your* action is needed
  (`awaiting` includes "you"). Pure opponent-replace turns block until
  the opponent acts and the next state frame arrives.
- Document the three cases in
  [`docs/mcp-protocol.md`](../docs/mcp-protocol.md) §3 with the same
  table style as the existing state-machine table.

**Minor doc fixes to bundle in:**
- `foe_bench_alive` is the count of unfainted Pokémon on the opponent's
  *bench* (excluding the active). The field name reads ambiguously
  during faint-replace transitions; either rename to
  `foe_bench_unfainted` or pin the definition in the protocol doc with
  a worked example. (Don't rename without checking the SPA — `web/`
  almost certainly reads it.)
- The phrase "Validate against the legal action set implied by the
  latest view" in `act`'s description becomes accurate only once
  [[mcp-legal-actions]] ships. Update the description in that PR.

**Acceptance:** An agent loop that only acts when `awaiting` includes
"you" never gets `errNotYourTurn`. Replace-phase turns where only the
opponent is replacing never wake the agent's `wait()` with
`ready: true`.

**Depends on:** nothing.
**Sibling:** [[mcp-legal-actions]] (closely related — both are about
removing "what's actually legal right now" ambiguity).
