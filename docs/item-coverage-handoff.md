# Follow-up prompt: widen PokeArena's item coverage

Paste everything below the line into a fresh agent.

---

# Widen PokeArena's item coverage

You are picking up PokeArena after PR #154 brought 24 moves off the data-sync
denylist. **Your job is the same shape of work one layer over: bring items into
the dataset and implement them.** Use as many subagents as the work needs — this
is deliberately parallel, and token cost is not the constraint.

The point is not only the test rows it closes. Items are *universal* — unlike
moves they have no learnset to be scoped by, so every item implemented now works
for every Pokémon that ever enters this dex. That makes item coverage cheaper per
row than move coverage was, and it is why this is the next piece.

**Do not add species.** The species filters in `cmd/data-sync/filter.go` stay
exactly as they are. If a piece of work seems to need a new Pokémon, it is out of
scope — say so and move on. (§"What is left after you" explains why that
constraint is now the binding one, and what it costs.)

## Read these first, in this order

1. `docs/showdown-port.md` — how the ported suite and its ledger work.
2. `docs/move-coverage-handoff.md` — the *previous* task, which is the closest
   thing to a worked example of this one. Its "How a move gets into the dataset"
   section describes a seam; yours is the same seam with the polarity flipped.
3. `cmd/data-sync/transform.go`, the `curatedItems` map — your backlog.
4. `internal/engine/items_core.go` and its siblings (`items_berries.go`,
   `items_modifiers.go`, `items_field.go`, `items_reactive.go`, `items_moves.go`,
   `items_fling.go`) — the registry pattern almost every item here will follow.

## How an item gets into the dataset

This is the whole mechanism, and it is shorter than the move one:

```
data/items.json  =  curatedItems ∩ upstream catalog
                    128 curated       581 upstream
```

`curatedItems` is an **allowlist**, not a denylist. Moves were the inverse — the
union of 80 learnsets *minus* a denylist — which is why that task's seam was
"remove an entry". Yours is "add one". Adding a slug to `curatedItems` and
re-running `go run ./cmd/data-sync` ships the item.

The transform errors on a slug that is not in the upstream catalog, so a typo or
an upstream rename fails the sync rather than shipping a hole.

### The one thing that is genuinely different from moves

**`data/items.json` carries `id` and `name`. That is all it carries.** Not a
description, not a base-power hook, not a fling value, not a natural-gift type —
the whole file is 128 two-field objects, and the upstream dump it is filtered
from has exactly the same two fields.

This is deliberate, not a dropped payload: `dumpItems` in
`tools/data-sync/refresh-upstream/refresh.js` says so in a comment, on the
reasoning that items have no learnset to scope against so the dump takes the
whole catalog and lets the Go allowlist choose. Every item's *behavior* is
hand-written in `internal/engine/items_*.go` and nothing is data-driven.

So do not go looking for an effect block to wire up, and do not "fix" the dump to
carry one. Shipping an item is: add the slug, write the hook, name it in the
registry with a `Desc`. Read upstream's `data/items.ts` for the behavior, because
the dataset will not tell you any of it.

## The backlog

The port has **465 accounted-for gaps**. They split cleanly, and the split is the
most useful thing in this document:

| | rows | |
|---|---:|---|
| blocked *only* by a missing item | **70** | **yours** — the seam above reaches every one |
| blocked by a missing ability or move | 385 | needs species; not yours |
| naming no dataset absence at all | 10 | real mechanic gaps; see below |

These are measured from the full blocker list of every gap row, not from the
one-line summary in `gaps_test.go` — that summary truncates to the first blocker
with a `(+N more)`, so classifying from it undercounts. Regenerate the real thing
with `make test-showdown-report`, which writes `showdown-report.json` with a
`detail` array per case. Do that before you trust any number in this document,
including the ones below.

**36 distinct items** account for those 70 rows, and **all 36 are present in the
upstream catalog** — verified, not assumed. There is no "this item does not exist
upstream" case to handle.

### Start here: nineteen rows are a table extension

Seventeen **plates** and **Sea Incense** are ×1.2 type boosters, and
`registerTypeBoosters()` in `items_modifiers.go` is already a table of exactly
those — eighteen entries, one per type, each a `typeBooster(kind, name, type)`.
Adding these is adding rows to that table.

The plates look like they need Arceus and do not. Upstream generates four cases
per plate; three are about a plate *in Arceus's hands* and the port already skips
them. The fourth — `"should be removed if not held by an Arceus"` — stands Mew in
for Arceus as a Knock Off user and asserts the plate comes off a Clefable like
any other item. Read `internal/engine/showdown/items_plates_test.go`; its header
explains the substitution. Nothing about the reachable case needs Multitype or
Judgment.

That is **19 rows for two dozen lines**, and it is the cheapest work in the
backlog. Do it first so the harder items land against a moved baseline.

### The rest, by rows each would unblock

Counts are item-only rows — rows where an item is the *sole* blocker, so
shipping it is expected to close them. They do not sum to 70: two rows each name
two items (Eviolite's needs Meadow Plate as well, and the Seeds row needs both
seeds), so those rows appear against each.

| item | rows | what it needs |
|---|---:|---|
| `ability-shield` | 14 | refuses every ability suppression aimed at the holder |
| `adrenaline-orb` | 9 | +1 Speed when the holder is Intimidated |
| `eject-pack` | 4 | force the holder out when its own stats drop |
| `eject-button` | 4 | force the holder out when it is hit |
| `red-card` | 3 | force the *attacker* out when the holder is hit |
| `mail` | 3 | mostly a "cannot be removed" marker |
| `booster-energy`, `room-service` | 2 each | |
| `eviolite`, `mirror-herb` | 1 each | |
| `electric-seed` + `grassy-seed` | 1 | one row needs both |

Four of the top five are the *same mechanic*: something happens, and the holder
or the attacker is forced off the field. `applyForceSwitch` (`forceswitch.go`)
already exists and already knows about Ingrain, Suction Cups and the Sky Drop
hold. Read it before you write any of the four; they are one feature, not four.

An item worth 0 rows is still worth implementing if it is cheap — it is coverage
for the battles that happen later — but rows are the signal you can measure.

### Four you should probably decline, and why

Say which of these you took and which you left. Declining with a reason is a
result; declining silently is not.

- **The four drives** (5 rows). A drive held by anything but a Genesect does
  nothing at all in canon — the Techno Blast retype is the whole item. There is
  no Genesect and no Techno Blast, so the honest implementation is an item that
  is inert by design, and `TestItemCatalogJoinsRegistry` requires every modeled
  item to carry a non-empty `Desc`. Shipping them means deciding what an
  inert-by-design hold looks like in this registry. That is a design call worth
  making explicitly, not by accident, and 5 rows may not be worth it.
- **`normalium-z`** (1 row). No Z-crystal is in the dataset and Z-moves are not
  modeled; the port already skips 53 Dynamax and 36 Terastallization cases for
  the analogous reason. One row is not a reason to open that door.
- **`red-orb`** (2 rows). Both are Desolate Land cases and the orb's job is
  Primal Reversion on a Groudon that is not in the dex.

## The safety net you have

**`TestItemCoverage` and `TestItemRegistrySubsetOfCatalog`
(`internal/engine/itemcoverage_test.go`) are a matched pair, and together they
are why this task is safe.** `AuditItems` reports every catalog item the engine
does not model; the fixture `testdata/item_coverage.json` is currently `[]`, so
any slug you add and do not implement makes the report non-empty and the test
fails. The second test guards the other direction: an `itemRegistry` key that is
not in the catalog fails. So you cannot ship an item without implementing it, and
you cannot implement one without shipping it. Let them drive you: add the slug,
watch the audit fail, implement until it passes.

Three more in the same file are worth knowing about before they surprise you:
`TestItemCatalogJoinsRegistry` (a modeled item must carry a `Desc`),
`TestItemNamesMatchCatalog`, and `TestItemRegistryKindMatchesKey`.

This is the item-side equivalent of `TestNoCuratedMoveIsInert`, and it is
*stricter* — the move audit could only catch a move that did nothing at all,
which is why Sonic Boom shipped for months dealing 1 damage instead of 20. The
item pair catches absence outright.

What it does **not** catch is an item that is modeled but modeled wrongly. Write
behavior tests against upstream, not against what the engine does.

## Rules

**Never weaken a ported test to make it pass.** If a case looks wrong, it is
either a translation bug (fix the port, say so) or the engine is right and
Showdown's case does not transfer (re-file the row with the reason). Changing an
assertion to match current behavior defeats the entire exercise.

Two cases in the last pass could pass *while measuring nothing* — one had its
premise removed by a stand-in substitution, one stopped reaching its own setup
once a move it depended on started working. Both were fixed rather than deleted.
Expect at least one of yours to be like that, and check a suspiciously easy pass
before you delete its row.

**Never edit** `harness_test.go`, `names_test.go`, `doc.go`, or
`harness_selftest_test.go`. Edit `gaps_test.go` only to delete rows you closed or
re-file rows you re-classified. **Re-file aggressively**: a row whose stated
blocker you just shipped is a row that now lies, even if it still fails for some
other reason. Thirteen needed re-filing last time.

**Read upstream before you write.** Clone `smogon/pokemon-showdown` if it is not
already checked out and read `data/items.ts`, `sim/battle-actions.ts`,
`sim/pokemon.ts`. The last two passes each got several diagnoses wrong by
reasoning from a mechanism's *name* instead of reading it. Assume you will too.

**A comment that contradicts your finding deserves care, not deletion.** This
codebase is heavily commented and the comments are usually half-right, which is
the dangerous kind. The last pass found `substitute.go` listing Memento among the
bypass-sub moves (it is not, in gen 9, and the difference was a whole upstream
case) and `docs/modernization-audit.md` calling Sky Drop "Doubles-only really"
(it is not, and eleven ledger rows proved it). Read upstream before overruling
one, and update it in the same commit as the behavior.

**Regenerate data, never hand-edit it.** `data/items.json` is generated. Change
`curatedItems`, then run `go run ./cmd/data-sync`.

**Verify before every push:**

```
go test ./... -count=1
make test-showdown
make lint
```

**Land each group as its own PR** so a data regeneration is reviewable in
isolation. The natural grouping is by mechanic, not by row count — the plates are
one commit, and the four force-switch items are one commit. Do not open a PR
unless asked.

**The golden corpus should not move, but check.** `testdata/archetype-teams.json`
is a static roster file carrying 15 items, and none of the 36 is among them — so
adding items cannot reach it, the same way no denylisted move reached it last
time. If `TestFullGame_MatchesGolden` moves anyway, that is a finding worth
chasing before you re-record it: something is reading the catalog that should be
reading the roster.

## The state you are inheriting

```
1989 ported cases, 401 passing, 465 accounted for, 1123 out of scope
465 gapMissing   the mechanic or the thing does not exist
  0 gapBug       the engine gives a wrong answer   ← still none
```

`make test-showdown` is green because every remaining failure is quarantined with
a reason in `gaps_test.go`, not because everything is implemented. The suite is
behind the `showdown` build tag, so ordinary CI never compiles it — run it
yourself.

## The 10 rows that are neither items nor species

These name no dataset absence. They are real mechanic gaps, and they are the only
other thing in the ledger you could close without growing the roster. Take them
if you finish the items; each is independent.

| rows | what it is |
|---:|---|
| 3 | **Semi-invulnerability.** Electric / Misty / Grassy Terrain "should not affect Pokémon in a semi-invulnerable state". This engine models none: a Pokémon mid-Fly is hittable by everything, and `gimmicks.go`'s `cancelAirborneCharge` says so deliberately. Sky Drop ships the hold without it (see `skydrop.go`'s header for why that costs almost nothing in singles). Building it touches Fly, Dig, Dive, Bounce and Phantom Force as well — five moves already shipping under a documented degradation — so it is an engine-wide mechanic change, not a move fix. Golden fixtures are safe: no archetype carries any of the five. |
| 1 | **Sky Drop vs a >200 kg target.** Weight is not in the dataset at all — `data/pokedex.json` carries types, base stats, abilities and genders and no mass. Adding it also unblocks Grass Knot, Low Kick, Heavy Slam, Heat Crash, Float Stone, Heavy Metal and Light Metal, so it is probably worth more as its own ticket than as Sky Drop's eleventh row. |
| 1 | **Disable should interrupt a rampage.** Disable does not break an Outrage lock today; the move stays choosable. |
| 1 | **Focus Punch's charge message after switches.** An ordering question in `ResolveTurn`'s pre-turn block, not a mechanic. |
| 1 | **The Metronome item off called moves.** Its streak counter should key on the move a caller called. Now that Sleep Talk, Copycat and Metronome exist, this is reachable — see `tickMetronome` in `turn.go` and the substitution seam in `calledmoves.go`. |
| 1 | **Reflect Type vs a typeless target.** Needs Burn Up to actually make its user typeless; the move is curated and unmodeled. |
| 1 | **Shed Shell vs Sky Drop.** The engine is right — `LegalActionsDex` refuses the switch. Upstream asserts that *submitting* it throws, and this harness has no way to reject a choice the controller makes anyway. Probably unclosable without touching `harness_test.go`, which you must not. |
| 1 | **Uproar / Throat Chop.** **Not yours.** Deliberately left open across three passes now; its ledger entry explains at length why the cheap fix is quietly wrong. Do not close it as a warm-up. |

## What is left after you

Be clear-eyed about this, because it is the whole remaining shape of the project.

**385 of the 465 rows need species this dex does not have.** 71 distinct
abilities block 228 rows, and **not one of the 71 is among the engine's 111
registered abilities** — they are genuinely absent, and none is carried by any of
the 80 species, so implementing them without a roster change would be writing
code no battle can reach. 69 distinct moves block another 179 rows for the same
reason: they are not in any kept species' learnset, so no denylist edit reaches
them.

So after the items and the 10 mechanic rows, the ledger stops being an engine
backlog and becomes a **roster decision**. Roughly:

```
465 gaps today
 -70  items          ← this task
 -10  mechanics      ← this task, if you get there
====
385  need species
```

That decision is not yours to make unasked, and this prompt does not make it. But
whoever writes the *next* handoff after this one should be told plainly that
"widen the dex" is the only lever left with real reach, and should be given the
numbers above rather than discovering them.

## Report at the end

Rows closed. Items brought into `curatedItems`, and any you decided to leave out
with the reason — the drives, the Z-crystal and Red Orb in particular. Whether
`TestItemCoverage` / `TestItemRegistrySubsetOfCatalog` stayed matched. Whether
`TestMoveCoverage` moved. Golden fixtures moved, and how many of the 147.
Anything you found that the ledger did not have — both previous passes found more
that way than from the backlog they were handed.
