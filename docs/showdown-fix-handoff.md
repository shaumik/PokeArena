# Follow-up prompt: close the Showdown port's findings

> **Done — do not paste this into a fresh agent.** This work was carried out;
> the ledger has no `gapBug` rows left and the port went from 254 to 362
> passing. Kept as a record of what was asked for and what state it was asked
> in. `docs/showdown-findings.md` has the outcome, including which of this
> prompt's own framings turned out to be wrong. The follow-on work — bringing
> denylisted moves into the dataset — has its own prompt in
> `docs/move-coverage-handoff.md`.

Paste everything below the line into a fresh agent.

---

You are picking up PokeArena after PR #150 landed a full port of Pokémon
Showdown's simulator test suite. **Your job is to fix the engine defects the
port found.** Use as many subagents and dynamic workflows as the work needs —
this is deliberately parallel, and token cost is not the constraint.

## Read these first, in this order

1. `docs/showdown-findings.md` — the report. 31 written-up findings plus a
   synthesis of what they have in common. The synthesis is the important part.
2. `docs/showdown-port.md` — how the suite and its ledger work.
3. `internal/engine/showdown/gaps_test.go` — the ledger. 613 rows; the 77 with
   `Kind: gapBug` are your backlog, and most carry an engine `file:line`.

## The state you're inheriting

```
1989 ported cases, 254 passing, 613 accounted for, 1122 out of scope
 77 gapBug      the engine is wrong          ← your work
536 gapMissing  the mechanic doesn't exist   ← not your work unless asked
```

`make test-showdown` is green **because every failure is quarantined with a
reason**, not because the engine is right. The suite is behind the `showdown`
build tag, so ordinary CI never compiles it.

The ledger self-prunes, and that is the mechanic you will live inside: when you
fix a bug, its case starts passing, and the run then **fails** with "this case
passes now — delete its row". That is your signal that a fix landed. Delete the
row in the same commit as the fix.

## The single most important thing to understand

Almost none of the 77 are arithmetic errors. They are **scope-of-application
errors** — a rule implemented correctly, then consulted at some of the places
that need it. That means **fixing them one case at a time is the wrong shape of
work.** Six findings share one cause; twenty-three share another. Fix the cause,
and a cluster of cases goes green together.

Four clusters, sized from the ledger:

| Cluster | Bug rows | The actual cause |
|---|---|---|
| **Groundedness** | ~23 | `isGrounded` (`terrain.go:50`) and `computeDamage`'s own Ground-immunity chain (`damage.go:173-195`) are two predicates for one concept, neither a superset of the other. Gravity is in neither. `terrain.go:45` documents the omission itself. |
| **Mold Breaker reach** | 5 | `abilityBlocksSecondaries(def)`, `abilityIgnoresStages(p)`, `abilityBlocksStatLowerByFoe(def, stat)`, `itemIsRemovable(p)`, `dampActive(s)` all decide a defender-side question with **no attacker in scope**, so the flag cannot reach them. Signature change, not five guards. |
| **Entry / faint ordering** | 7 | `ResolveReplace` walks sides by index and `doSwitch` runs hazards + `applyOnSwitchIn` inline, so entry effects interleave with simultaneous switch-ins. The lead path (`turn.go:60-62`) already does it right — copy that shape. |
| **`inflictStatus` bypass** | 2+ | `doRest` sets status directly and re-makes only the Chesto check; the terrain, ability and Safeguard guards were never re-made. |

The remaining ~40 are individually-scoped and parallelize cleanly.

Start with groundedness and Mold Breaker. They are the two with the best ratio
of blast radius to diff size, and neither needs a design decision.

## Two fixes that are not in `internal/engine` at all

- **`cmd/data-sync/transform.go:683`** — the `default:` branch maps every
  unrecognized Showdown target to `foe`, which sweeps up `allies` and
  `adjacentAlly`. Consequence: **Howl and Coaching boost your opponent.** Fix
  the branch to *error* on an unknown target rather than guess; the collapse is
  only safe for values somebody has actually looked at. Then regenerate the
  affected rows in `data/moves.json`. This same branch is half of why Protect
  blocks hazard setting.
- **Worry Seed / Skill Swap / Simple Beam / Role Play** are pickable and
  silently do nothing — they log a successful use and change nothing. Either
  implement ability-setting or make them fail visibly. Do not leave a move that
  narrates success and does nothing.

## Rules

**Never weaken a ported test to make it pass.** If a case looks wrong, it is
either a translation bug (fix the port, say so) or the engine is right and
Showdown's case doesn't transfer (re-file the row as `gapScope` with the
reason). Changing an assertion to match current behavior defeats the entire
exercise.

**Never edit** `harness_test.go`, `names_test.go`, `doc.go`, or
`harness_selftest_test.go`. Edit `gaps_test.go` only to delete rows you closed
or re-file rows you re-classified.

**Expect the golden corpus to move.** `internal/engine/testdata/fullgame-golden.json`
hashes log text as well as damage, so almost any behavior fix shifts fixtures.
Re-record with
`go test ./internal/engine/ -run TestFullGame_MatchesGolden -update-golden`,
and say in the commit how many of the 147 moved and why. `docs/engine-findings.md`
has the precedent for how this repo writes that up.

**Expect some existing engine tests to fail, and read each one carefully.** This
repo has previously shipped tests that pinned buggy behavior — `docs/engine-findings.md`
records one that "asserted the contradictory state directly and called it Gen 5+
semantics". When an existing test fails against your fix, decide honestly
whether it was pinning the bug (rewrite it, and note that in the commit) or
whether you broke something real (fix your change). Never delete an existing
test to get green.

**Verify before every push:**
```sh
go test ./... -count=1          # ordinary CI
make test-showdown              # the port; must be green
make lint                       # both tagged and untagged
```

**Findings that contradict an existing source comment deserve care.**
`terrain.go:45`, `damage.go:193`, `damage.go:213`, `turn.go:880`, `buffs.go:38`
each assert something the port disagrees with. Those comments were written
deliberately and are usually half-right. Read the upstream Showdown source
before overruling one, and update the comment in the same commit as the
behavior.

## Caveats about what you inherited

- The 31 written-up findings are the **reviewed subset**. The other ~46 gapBug
  rows have a one-line reason naming a mechanism, but nobody wrote an essay.
  Re-verify a row before acting on it — one agent-reported finding in the
  original pass was simply wrong and was caught by tracing it.
- The 536 `gapMissing` rows are a **mechanical inventory**, not individual
  judgements. Reliable as a list of what's absent; not 536 considered opinions.

## Suggested shape

Scout the clusters inline first, then fan out: one workflow per cluster, one
agent per independent fix inside it, with an adversarial verify stage that
re-runs the affected ported cases *and* the existing engine suite. Land each
cluster as its own PR so a golden re-record is reviewable in isolation. Groundedness
and Mold Breaker touch the same files — sequence them rather than running them
in parallel.

Report at the end: rows closed, rows re-filed and why, golden fixtures moved,
and anything you found that the port didn't.
