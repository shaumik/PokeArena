# MCP: damage-range prediction per legal move

**Status:** not started

**Why:** Stat numbers in the view (`Atk 154`, `Def 115`, …) and a move's
base power are sufficient *in principle* to compute expected damage,
but the agent has to know the engine's exact damage formula, RNG range,
and effective level scaling. In practice, an agent's mental model
diverges from the engine by an order of magnitude until it gets enough
observed turns to back-fit a multiplier — by which point the battle is
half over and several decisions were made on bad numbers. (Observed: an
agent's model was systematically 2× the engine's actual output for the
first ~5 turns because it assumed L=100 while the engine uses ~L=50
scaling.)

The engine already has the damage formula in code; running it once per
legal `move` action against the current foe is cheap and removes the
entire class of "agent guessed the formula wrong."

**Scope (smaller, ship-first):** expose what the agent needs to compute
damage itself:
- A `level int` field on each `Pokemon` in the view (or once on the
  battle, if the engine doesn't vary it).
- Damage formula documented in `docs/mcp-protocol.md` or
  `docs/live-pvp.md` (whichever owns wire semantics). One paragraph.

**Scope (bigger, where the real value is):** per-action prediction.
- For each `move`-kind entry in `view.legal_actions` (see
  [[mcp-legal-actions]]), include:
  - `dmg_min int`, `dmg_max int` — over the engine's damage-roll range,
    against the current foe, holding stages/status fixed.
  - `ohko_chance float` — fraction of the roll range that brings the
    foe to ≤ 0 HP.
  - `effectiveness float` — the type-chart multiplier (1.0, 2.0, 0.5,
    0.25, 0).
  - `accuracy int` — already covered by [[mcp-move-metadata]] but
    convenient to repeat here so the action carries its own predictions.
- Skip prediction for non-damaging moves (status-category): set fields
  to null / omit.
- Built from `engine.damage(…)` factored into a pure function the view
  projector can call. Stage modifiers / status effects (burn halving
  Atk) should be applied — the agent shouldn't have to redo what the
  engine will do at resolution time.

**Open question:** should the prediction account for the foe's *next*
move (e.g. account for a likely incoming attack)? **No.** That's
strategy, not engine math. The view tells you what the engine will do
on resolution given your move; opponent prediction is the agent's job.

**Acceptance:** An agent that picks the move with the highest
`(dmg_max if dmg_max < foe_hp else dmg_min * ohko_chance)` plays
correctly against a static foe in tests, *without* implementing any
damage formula client-side. Observed-vs-predicted damage match within
the engine's actual roll range across at least 50 sampled turns.

**Depends on:** nothing in core; the engine's damage function exists.
**Pairs with:** [[mcp-legal-actions]] (action list is the carrier),
[[mcp-move-metadata]] (BP/type/accuracy on the move itself).
