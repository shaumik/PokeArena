# Spreads, round one — and the float trap that wasn't

PR 1 of the [EV/nature/IV plan](2026-08-03T20-01-evs-natures-and-ivs.md) is
in: the formula, the data, the wire fields, the validation, and the fog
shadows. Everything else in that plan still stands. Two things from the build
are worth writing down, because both contradict the plan.

## The float trap I predicted does not exist

The earlier entry says, with some confidence, that `floor(stat × 1.1)` in
float64 "lands one below canon on real stat values" because 1.1 has no exact
binary representation, and calls it "the single most likely place for a silent
off-by-one." I wrote that from the general shape of the argument — irrational
constant, floor, therefore danger — without measuring it.

I measured it. Over `s ∈ [0, 2000]`, which covers every stat this engine can
produce (roughly 21–310 at L50), `int(math.Floor(float64(s) * 1.1))` and
`s*11/10` agree on **every single value**. Same for 0.9. The reason is that the
doubles nearest 1.1 and 0.9 both sit slightly *above* the exact decimal, so the
product never falls short of the true value and the floor never rounds down a
step. The naive float implementation would have been correct.

The implementation still uses integer ratios, and the doc comments now say why
honestly: exactness by construction beats exactness that depends on a range
argument nobody can see from the code. `TestNatureRatioMatchesFloat` locks the
measurement in so the next person doesn't "fix" the integer math believing they
found a bug — which is a much likelier failure than the one I invented.

The lesson is the boring one. A plausible mechanism is not a measurement, and
writing it into a design doc gives it a confidence it hasn't earned. The check
took two minutes and I did it *after* committing the claim.

## The leak was real, but not where I was looking

The plan devoted a whole section to `foeWire` — embedding means new fields
serialize to the opponent unless shadowed, so `evs`/`ivs`/`nature` would leak
the spread. That was correct, and the shadows went in. It turned out to be the
*guarded* risk: `TestView_FoeTopLevelKeysAreAllowlisted` already existed, from
the batch where `last_consumed_item` shipped, and it catches exactly this. I
confirmed it by deleting each shadow in turn and watching it fail. The
allowlist test did its job without anyone having to remember the rule.

The unguarded copy was somewhere I never thought to look. `TeamPool.Pick`
rebuilt each `TeamPick` from an enumerated field list:

```go
out[i] = engine.TeamPick{
    DexNo:   s.DexNo,
    MoveIDs: append([]string(nil), s.MoveIDs...),
    Ability: s.Ability,
    Item:    s.Item,
}
```

Add a field to `TeamPick` and it silently vanishes on its way out of the AI
team pool. A curated Adamant 252-Atk set would have arrived in battle as a
neutral 0-EV one, with nothing failing anywhere — no error, no type mismatch,
just quietly worse Pokémon. This is a strictly nastier bug than the fog leak,
because a leak has an observer (the allowlist test) and a dropped field has
none until someone compares two numbers by hand.

Fixed with `TeamPick.Clone()`, which starts from a value copy so new fields are
carried by default and only pointers/slices need thought, plus
`TestTeamPickCloneIsDeep`, which reflects over the struct and fails if the
fixture leaves any field at its zero value. The next field added to `TeamPick`
gets a test failure telling it to make a decision.

Two hand-written copies of the same struct existed. Both are now one function.

## What actually shipped

- `data/natures.json` through the full ETL — `refresh.js` grows `dumpNatures()`,
  and the hand-authored `upstream/natures.json` matches what it will emit, so
  `make sync` reproduces the committed file byte for byte (verified). The
  other four dataset files are unchanged by the sync, which is the proof this
  round introduced no incidental data churn.
- `natures.json` is **required** by `LoadDexFS`, unlike the optional
  `items.json`. An absent item catalog degrades honestly; an absent nature
  table would make every nature slug illegal and read as a validation bug.
- `Spread` / `DefaultSpread()` / `resolveSpread` in the engine.
  `DefaultSpread()` is deliberately not the zero value — zero IVs are a legal,
  terrible spread, not "unspecified", which is also why EVs and IVs are
  pointers on the wire.
- `TestDefaultSpreadMatchesLegacyStats` re-derives the old formula inline and
  checks it against every species in the dex. A test that called `calcStat` to
  check `calcStat` would have proven nothing.

## Still open, on purpose

`eval.Ruleset()` still reports `IV31/EV0, neutral nature, no items`. Both
halves of that are now stale — items shipped some batches ago, and spreads are
legal as of this PR — but every shipped team library is still all-neutral, so
no run header is currently lying about the battles it describes. PR 3 owns the
string, alongside the EV'd libraries that will make it matter.
