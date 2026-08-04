# Library v2 — the teams finally use the spreads

Third and last round of the [EV/nature/IV plan](2026-08-03T20-01-evs-natures-and-ivs.md).
PR 1 made spreads legal, PR 2 made them settable, and both shipped with every
team still at EV 0 / IV 31 / neutral so the engine change was provably inert.
This is the round that spends that inertness: `data/benchmark-teams.json` goes
to v2 with a nature and an EV spread on all 36 picks, and `data/ai-teams.json`
takes the same ones so the opponent a player meets in `mode=live` is the same
strength as the one the benchmark measures.

## The ruleset string was lying in two directions

`eval.Ruleset()` read `L50, IV31/EV0, neutral nature, no items` and was wrong
about half of it before this round even started — items shipped batches ago.
It would have gone wrong about the other half the moment these teams landed.

The bug is not the stale text, it's the category error. That one string was
describing two different things at once:

- what the **format permits** — the rules `ValidateTeam` enforces
- what the **teams actually use** within those permissions

Those move independently, and only one of them is knowable from the engine.
So they're two fields now. `Ruleset()` is built from `engine.Level`,
`engine.MaxIV`, `engine.MaxEVPerStat`, `engine.MaxEVTotal` — it cannot drift
from the validator because it *is* the validator's constants. `TeamProfile()`
counts the picks: `36 picks: 36 EV-trained, 36 natured, 0 custom IVs, 0
holding items`. It cannot drift either, because nothing about it is asserted.

Both go in the run header. The benchmark doc's central claim is that "a result
can never be silently reinterpreted under a different format" — that claim was
false in the specific case where the format holds still and the *teams* move,
which is exactly what happened today. `team_profile` next to `team_library`
closes it.

`TestRulesetDescribesPermissionsOnly` asserts the *absence* of "EV0", "IV31",
"neutral nature", "no items" from the permissions line. Asserting an absence
feels odd until you notice that re-adding any of them is precisely the
regression, and every other test in the package would still pass.

## The curation guard found something on its first run

I wrote `TestTeamLibrary_NaturesDoNotHurt` for the mistake a spread makes
easiest: a nature that lowers the very stat its holder attacks with. A Timid
physical attacker is a 10% tax on the mon's whole job, the team is perfectly
legal, and nothing in the engine objects.

It failed immediately, on a set I had personally waved through:

```
team "Bruiser": Tauros is Jolly (-spatk) but attacks with fire-blast, a special move
```

I'd reasoned about this one while authoring and talked myself past it — base
40 Sp.Atk, Fire Blast is dead weight anyway, keep the Speed. That reasoning is
correct about the *nature* and wrong about the conclusion: if the move is dead
weight, the move is the problem. Fire Blast off 40 Sp.Atk was never real
coverage, not even under the old neutral format. Cut it for Megahorn, which is
physical, hits the Grass and Psychic the Fire Blast was nominally there for,
and lets Jolly be honest.

The tempting fix was an exemption in the test for "low base stat." That would
have been the wrong instinct on the very first thing the guard caught. The
only exemption is fixed-damage moves — Seismic Toss deals damage equal to the
user's level regardless of Attack, which is exactly why Chansey can run Bold
for free.

## Measuring the discontinuity instead of asserting it

The plan promised the v1→v2 break would be "explicit and dated." Explicit is
cheap to write and worth nothing without a number, so I replayed both
libraries through the same heuristic mirror, 60 seeds per team:

| Team | v1 turns | v2 turns | games that changed |
|---|---:|---:|---:|
| Genesis | 29.9 | 24.9 | 58 / 60 |
| Spectrum | 32.4 | 33.9 | 52 / 60 |
| Keystone | 32.7 | 34.0 | 58 / 60 |
| Bruiser | 17.6 | 16.5 | 53 / 60 |
| Bastion | 57.1 | 73.1 | 60 / 60 |
| Blitz | 27.0 | 23.6 | 56 / 60 |

Offense got faster, and the wall team got much harder to break — 252 HP plus
252 in the matching defence is real, and Bastion's games run ~28% longer for
it. Worst case over 720 games is 92 turns against a 20,000-decision cap, so
the tail costs wall-clock and nothing else.

Two things I checked because they could have quietly broken:

**The honesty property held.** The benchmark's stated soundness check is "on
every team, a better policy beats a worse one." Heuristic still goes 36-0 over
random across all six trained teams.

**Cross-team balance improved**, which I did not expect. Advisory flags from
`cmd/team-validate` went 13 → 9; Bruiser climbed from 10% to 34% overall,
Bastion 29% → 34%, while Blitz fell 78% → 66% and Genesis 74% → 66%. Letting
slow bulky mons buy bulk instead of nothing narrowed the gap between the
archetypes. It's advisory — the battle benchmark is mirror-matched, so
cross-team strength cancels — but it's the right direction.

## Spread policy, such as it is

- **252 / 252 / 4 everywhere.** I nearly "optimised" this to 248 on the theory
  that L50 wastes the last 4 EVs. Wrong: `2·Base + IV` is odd with 31 IVs, so
  the extra `floor(4/4)` tips the division and all three investments buy a
  point. Checked before committing this time rather than after.
- **Speed only where it can be won.** Under base 65, the EVs go to bulk —
  Snorlax at base 30 is not outrunning anything, so Speed investment there is
  a donation.
- **Role, not species.** Keyed by (team, slot): Bastion's Rhydon is a wall and
  Genesis's is a wallbreaker, and they should not share a spread.

## What this closes

The three-PR plan is done. `docs/benchmark.md` §3 now separates permissions
from usage, §7 carries the v2 table and the curation rules. The one thing I'd
still call open is that `cmd/team-validate`'s advisory flags are now measuring
a meta nobody has tuned for — the spreads were authored for competence, not
for cross-team parity, and parity is a Build-track concern anyway.
