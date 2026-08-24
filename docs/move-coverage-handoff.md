# Widen PokeArena's move coverage

You are picking up PokeArena after PR #152 closed the last of the engine
defects that the Showdown port found. **Your job is to bring denylisted moves
into the dataset and implement them.** Use as many subagents as the work needs —
this is deliberately parallel, and token cost is not the constraint.

The point is not only the test rows it closes. The roster is 80 species today
and will grow slowly later; every move implemented now is a move that already
works the day a species that learns it arrives. Moves first, Pokémon after.

**Do not add species.** The species filters stay exactly as they are. If a piece
of work seems to need a new Pokémon, it is out of scope — say so and move on.

## Read these first, in this order

1. `docs/showdown-port.md` — how the ported suite and its ledger work.
2. `cmd/data-sync/transform.go`, the `denylistMoves` map — your backlog, with a
   one-line rationale above each group.
3. `docs/showdown-findings.md`, the two closing sections — what the last pass
   learned, including three diagnoses that were confidently wrong.
4. `internal/engine/callbackmoves.go` — the pattern almost every move here will
   follow, and its header explains why the pattern exists.

## How a move gets into the dataset

This is the whole mechanism, and it is short:

```
data/moves.json  =  union of all 80 species' learnsets (gens 1-9)  −  denylistMoves
                    576 moves                                        40 blocked
                    = 536 shipped
```

Verified exactly: 576 − 40 = 536, no discrepancies either way.

So **removing a move from `denylistMoves` and re-running `go run ./cmd/data-sync`
adds it, with no new species.** That is the seam you are working on.

46 entries are on the denylist; 40 of them are in the union and are therefore
yours to unblock. The other 6 (`decorate`, `doom-desire`, `metal-burst`,
`sketch`, `assist`, `camouflage`) are not learned by any of the 80 species, so
removing them changes nothing — leave them.

The 56 moves that ledger rows name as absent but that are *not* on the denylist
(`thousandarrows`, `magmastorm`, `spiderweb`, `stickyweb`, `mindblown`, …) need
a species that learns them. Out of scope. Do not touch the filters in
`cmd/data-sync/filter.go`.

## What blocks each group, and what it is worth

Row counts are ledger rows in `internal/engine/showdown/gaps_test.go` that name
the move as missing. A move worth 0 rows is still worth implementing if it is
cheap — it is coverage for the species that arrive later — but rows are the
signal you can measure.

| group | moves | rows | what it needs |
|---|---|---|---|
| Custom HP arithmetic | belly-drum 9, memento 6, pain-split 2, final-gambit 2, endeavor 0, super-fang 0 | **19** | Nothing new. Each is a small self-contained handler. |
| Calls another move | sleep-talk 9, copycat 3, snore 1, metronome 0, mimic 0, mirror-move 0, me-first 0 | **13** | Real move-calling — except Snore, which is grouped here in transform.go but is really just "usable only while asleep" and is cheap on its own. |
| Future-impact damage | future-sight 12 | **12** | A delayed-damage queue on the side. New state. |
| Two-turn, doubles-flavoured | sky-drop 11 | **11** | A two-turn move that carries its target. Read canon carefully before deciding it transfers to singles at all. |
| Reactive damage | counter 3, mirror-coat 2, bide 1 | **6** | The *amount* of damage taken this turn, per category. `Volatiles.HurtThisTurn` and `DamagedThisTurn` exist; the amount does not. |
| Type / identity change | transform 3, soak 2, reflect-type 2, conversion-2 1, conversion 0 | **8** | Writing a Pokémon's types mid-battle. `Pokemon.BaseStats` and `BaseAbility` already show the revert-on-switch-out shape to copy. |
| Guaranteed hit | lock-on 1, mind-reader 0 | **1** | A "next move cannot miss" volatile. The comment on the denylist says this was deferred until Laser Focus landed; Laser Focus landed (`Volatiles.LaserFocus`), so this is unblocked. |
| Doubles-only | helping-hand, follow-me, rage-powder, spotlight, quash, coaching, hold-hands, ally-switch, after-you, dragon-cheer, the three pledges | 3 | **Leave denied.** There is no ally in singles, and the comment above `coaching` in transform.go explains what goes wrong if you map them anyway. |
| Superseded | mud-sport, water-sport | 0 | **Leave denied.** Terrain replaced them. |

Suggested order: custom HP arithmetic first (19 rows, no new machinery, and it
teaches you the codebase), then reactive damage, then type changes, then
future-sight. Decide about move-calling and sky-drop only after you have read
upstream on both.

## The safety net you have, and the one you do not

**`TestNoCuratedMoveIsInert` (internal/engine/move_inert_test.go) is the reason
this task is safe.** It plays every curated move in a fixture built to give it
something to act on, and fails on any move that neither changes the battle nor
says anything beyond its own name. So you *cannot* remove a move from the
denylist, forget to implement it, and have it slip through — the audit will fail
the moment the move ships. Let it drive you: enable the move, watch the audit
fail, implement until it passes.

"But it failed!" satisfies the audit. That is deliberate: a move that visibly
refuses is an honest outcome, and for one or two of these it may be the right
one. A move that narrates success and does nothing is what the audit exists to
prevent.

**`TestMoveCoverage` will move, and you must read the diff rather than
re-recording it.** It is a committed snapshot of moves whose upstream definition
asks for behavior the engine does not have. Adding a curated move can grow that
set. Shrinking is good; growing means either you shipped a move whose semantics
are not modeled, or you removed engine support. Re-record with
`-update-coverage` only once you have understood every line that changed.

## Rules

**Never weaken a ported test to make it pass.** If a case looks wrong, it is
either a translation bug (fix the port, say so) or the engine is right and
Showdown's case does not transfer (re-file the row with the reason). Changing an
assertion to match current behavior defeats the entire exercise.

**Never edit** `harness_test.go`, `names_test.go`, `doc.go`, or
`harness_selftest_test.go`. Edit `gaps_test.go` only to delete rows you closed or
re-file rows you re-classified.

**Read upstream before you write.** Showdown is checked out for you; if it is
not, clone `smogon/pokemon-showdown` and read `data/moves.ts`,
`sim/battle-actions.ts`, `sim/battle.ts`. The last pass got three diagnoses
confidently wrong by reasoning from a mechanism's *name* instead of reading it,
and each was caught only by a test. Assume you will do the same at least once.

**Expect the golden corpus to move, and check rather than assume.** No archetype
roster carries a denylisted move today, so in principle adding one changes
nothing. Verify it. If fixtures do move, re-record with
`go test ./internal/engine/ -run TestFullGame_MatchesGolden -update-golden` and
say in the commit how many of the 147 moved and why.

**Expect some existing engine tests to fail, and read each one carefully.**
Decide honestly whether the test was pinning a bug (rewrite it, and record the
reason in the test's own doc comment) or whether you broke something real. Nine
tests were rewritten in the last pass and none was deleted. Never delete a test
to get green.

**A comment that contradicts your finding deserves care, not deletion.** This
codebase is heavily commented and the comments are usually half-right, which is
the dangerous kind. Read upstream before overruling one, and update it in the
same commit as the behavior.

**Regenerate data, never hand-edit it.** `data/moves.json` is generated. Change
`denylistMoves` or `internal/specs`, then run `go run ./cmd/data-sync`. If a
payload you need is being dropped, the fix is in the transform or in the specs
vocabulary — the last pass found three flags (`protect`, `gravity`, `healblock`)
that were silently dropped and each turned out to *be* the whole rule its
mechanic read.

**Verify before every push:**

```
go test ./... -count=1
make test-showdown
make lint
```

**Land each group as its own PR** so a data regeneration and any golden
re-record are reviewable in isolation. Do not open a PR unless asked.

## The state you are inheriting

```
1989 ported cases, 362 passing, 505 accounted for, 1122 out of scope
505 gapMissing   the mechanic or the move does not exist
  0 gapBug       the engine gives a wrong answer   ← none left
```

`make test-showdown` is green because every remaining failure is quarantined
with a reason in `gaps_test.go`, not because everything is implemented. The
suite lives behind the `showdown` build tag, so ordinary CI never compiles it —
run it yourself.

Of the 505, roughly 231 row-mentions name a missing move. 73 of those are
reachable by removing denylist entries; the rest need species you are not adding.

One row is deliberately left open and is **not** yours: Uproar / Throat Chop.
Its ledger entry explains at length why the cheap fix is quietly wrong. Do not
close it as a warm-up.

## Report at the end

Rows closed. Moves brought off the denylist, and any you decided to leave on it
with the reason. Whether `TestMoveCoverage` grew or shrank and what changed.
Golden fixtures moved. Anything you found that the ledger did not have.
