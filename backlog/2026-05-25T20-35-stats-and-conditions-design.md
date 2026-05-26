# Stats and conditions — the round that takes us past "bare bones"

Today's bare-bones sim handles five stat stages (Atk/Def/SpA/SpD/Spe) and five
non-volatile statuses (Burn/Poison/Paralysis/Sleep/Freeze), enough to play but
not enough to be interesting. This entry captures the design discussion before
we start building the next round.

## What we're committing to this round

- **Accuracy / Evasion stages.** Adding `Acc` and `Eva` to the stage struct, plus a separate `accStageMultiplier` (3/(3-s) negatives, (3+s)/3 positives — *not* the symmetric offensive curve). Effective accuracy is `move.acc * mult(clamp(atk.Acc - def.Eva, -6, +6))`. A `bypass-acc` move flag skips the roll entirely.
- **Toxic as its own status enum.** `StatusToxic`, with a `ToxicCounter` field on the Pokémon. Residual damage is `MaxHP * counter / 16`, counter increments each turn, caps at 15. Counter resets when status clears or on switch-out.
- **Volatiles struct.** Typed, with the convention that stateful volatiles are pointer-or-nil and transient volatiles are bool. Two inhabitants this round: `*ConfusionState` (with turn counter 2–5, Gen 7+ 33% self-hit) and `bool Flinch` (set during one side's move, consumed at start of the other side's move that same turn).
- **Showdown-shaped move schema.** Rewriting `data/moves.json` and `domain.Move`. New shape has `primary` (status-move guaranteed effect), `self` (damage-move guaranteed self-effect), `secondaries: []` (rolled riders, multiple allowed), `flags: []`, `target`. Old `Effect` deleted entirely — we have no users to break.
- **`executeMove` factored into named phases:** `canAct → choosePP → announceMove → resolveAccuracy → dealDamage → applySelf → applySecondaries`. `applyResidual` stays separate. The seams matter even before we have ability hooks — they enforce the order of operations.
- **Bug fixes:** freeze thaws on Fire-type hit (and the move still lands); sleep counter resets on switch-out (Gen 5+ — today's code accidentally persists it); stage log wording goes from binary "sharply/not-sharply" to the full `rose | rose sharply | rose drastically` and `fell | harshly fell | severely fell` ladder.
- **`Rest` and `Cure` as new effect kinds**, because the existing `inflictStatus` rejects when a status is already present and Rest needs to cure-then-sleep through that.

## Decisions that took the longest to land

**Toxic: separate enum vs counter on Poison.** I argued for counter-on-Poison first ("less code churn"). Pushback from Shaumik made me re-examine. Flipped to separate enum because (1) the counter has to exist either way so we save nothing, (2) Showdown precedent (`psn` vs `tox`), (3) future Frostbite is the same pattern, (4) cure code stays compiler-honest. The "less code churn" argument turned out to be three additional `case` lines across three switch statements. Cheap.

**Volatiles: typed struct vs map[string]int.** Settled on typed struct quickly. The deeper question was zero-value-means-absent vs pointer-or-nil. Went with pointer-or-nil because "Confused: 0 turns" is ambiguous (just snapped out vs not confused) and pointer-nil-or-not is the unambiguous signal. JSON `omitempty` on the pointer keeps the wire format tidy.

**Confusion at 33% (Gen 7+) and Sleep reset on switch (Gen 5+).** Picked the modern defaults to match what most current players expect, and because Showdown does too. The codebase had accidentally landed on Gen 3–4 sleep behavior (no reset) and no confusion at all. Documenting the generation explicitly so future-us doesn't have to re-derive.

**Schema shape: Showdown-style array of secondaries, not single Effect.** The single Effect was already broken for Tri Attack (three independent secondaries). The new shape grows by adding optional fields, never by mutating existing ones — that's the load-bearing property. `primary` + `self` + `secondaries[]` separates guaranteed effects from rolled ones cleanly. Future weather/terrain/hazard fields can attach as more optional siblings without disturbing what's there.

**Toxic Spikes is not in scope this round.** Spent some time untangling it from Toxic representation. Toxic Spikes is an entry hazard (lives on the Side, not the Pokémon), with 0/1/2 layers determining whether a switch-in gets Poison or Toxic. The status, once applied, sits on the Pokémon — so hazard state and status state are decoupled, and we can ship Toxic now without the hazard machinery. The hazard system is its own chunk (alongside Reflect/Light Screen/Stealth Rock).

## Scope cuts and where they go

Deferred to future rounds, tracked as GitHub issues after this lands:

- Weather, Terrain, Side conditions (hazards + screens).
- Abilities and items.
- Volatile catalog round 2: LeechSeed, Substitute, Trap, Taunt, Encore, Disable, Charging (Solar Beam / Fly / Dig).
- Multi-hit moves, two-turn moves.
- Frostbite.

The stable design contract lives in [`docs/battle-state.md`](../docs/battle-state.md);
this diary entry is the *why*, that doc is the *what*.

## The framing question

Aside on positioning: the project bills itself as the first AI-focused Pokémon
sim. Showdown has bots and adapter layers, but it was built for human play.
The thesis here is to build the sim *from* the agent harness outward — MCP as
a first-class protocol, deterministic replay from seeds, JSON-native state.
Whether that thesis holds against scrutiny is another question, but it's the
one shaping these design choices: when a decision could go either way, we
take the side that's easier to expose to an external agent.
