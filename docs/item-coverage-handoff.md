# Item coverage: done, and what it left behind

This file was written as a handoff prompt for widening item coverage. **That
work is finished** — see the five `items:` commits — so what follows is the
record of it and an honest account of what is left, rather than a task.

## What shipped

Thirty items, `curatedItems` 128 → 158, and the Showdown port from **401
passing / 465 gaps to 448 passing / 418 gaps**.

| group | items | rows closed |
|---|---|---:|
| Arceus's plates + Sea Incense | 18 | 18 |
| Ability Shield | 1 | 13 |
| Eject Button, Red Card, Eject Pack | 3 | 3 |
| Adrenaline Orb (+ Own Tempo) | 1 | 7 |
| Mail, 4 terrain seeds, Room Service, Mirror Herb | 7 | 6 |

`TestItemCoverage` is still `[]` and `TestMoveCoverage` still `null`, so every
curated item is modeled and nothing regressed. None of the 147 golden fixtures
moved: `testdata/archetype-teams.json` is a static roster carrying 15 items and
no new one is among them.

Twenty-five rows were **re-filed** rather than closed — their stated blocker
shipped, so the row had started to lie. Most now name an absent ability.

## Five items deliberately not curated

Each would be registered, described, and unable to fire in any battle this
engine can build. The reasons are in the ledger rows too.

| item | rows | why not |
|---|---:|---|
| the four drives | 5 | held by anything but a Genesect a drive does nothing; the Techno Blast retype is the whole item |
| Booster Energy | 2 | switches on Protosynthesis or Quark Drive, both Paradox abilities absent from the engine and every in-dex species |
| Red Orb | 2 | Primal Reversion on a Groudon |
| Normalium Z | 1 | no Z-crystal is in the dataset and Z-moves are not modeled |
| Eviolite | 1 | **no species in this dex is NFE** — checked, not assumed: the upstream dump carries `evos` per species and it is empty for all 80, Chansey/Onix/Porygon included, because `refresh.js` filters out later-generation evolutions |

## The ledger now

```
1989 ported cases, 448 passing, 418 accounted for, 1123 out of scope
418 gapMissing
  0 gapBug        ← still none, across three passes
```

The 418 split:

| | rows | |
|---|---:|---|
| need an absent ability or move | **392** | needs species; not an engine backlog |
| name no dataset absence | 15 | the real mechanic gaps, below |
| blocked only by a declined item | 11 | the table above |

72 distinct abilities block 235 rows and 69 distinct moves block another 179.
Not one of the 72 is among the engine's 111 registered abilities, and none is
carried by any of the 80 species — so **the only lever with real reach left is
widening the dex**, and that is a roster decision rather than engine work.

## The 15 mechanic rows

Independent of each other, and the only thing left that could be closed without
growing the roster.

| rows | what it is |
|---:|---|
| 4 | **Mid-turn switch requests.** Three Eject Pack rows and one Eject Button row assert `Phase == replace` on one side — a live Pokémon owing a replacement the *player* chooses. `PhaseReplace` means "a fainted active owes a replacement" by state invariant, and raising it for a live one needs `ResolveTurn` to suspend mid-turn. The items work; the holder leaves immediately with the engine picking, exactly as U-turn already does. |
| 3 | **Semi-invulnerability.** The three terrains "should not affect Pokémon in a semi-invulnerable state". This engine models none — see `gimmicks.go`'s `cancelAirborneCharge`, which says so deliberately — and building it touches Fly, Dig, Dive, Bounce and Phantom Force. No archetype carries any of the five, so golden fixtures are safe. |
| 1 | **Weight.** `data/pokedex.json` carries no mass. Also unblocks Grass Knot, Low Kick, Heavy Slam, Heat Crash, Float Stone, Heavy Metal and Light Metal, so it is worth more as its own ticket than as Sky Drop's row. |
| 1 | **Disable should interrupt a rampage.** |
| 1 | **Focus Punch's charge message after switches** — an ordering question in `ResolveTurn`'s pre-turn block. |
| 1 | **The Metronome item off called moves** — reachable now that Sleep Talk, Copycat and Metronome exist. |
| 1 | **Reflect Type vs a typeless target** — needs Burn Up to make its user typeless. |
| 1 | **Adrenaline Orb vs a Contrary holder at -6 Speed** — needs Contrary. |
| 1 | **Shed Shell vs Sky Drop.** The engine is right; upstream asserts that *submitting* the choice throws, and this harness cannot reject a choice the controller makes anyway. Probably unclosable without touching `harness_test.go`. |
| 1 | **Uproar / Throat Chop.** Still deliberately open across four passes. Its ledger entry explains at length why the cheap fix is quietly wrong. |

## Rules that still apply to anything here

Unchanged from the two handoffs before this one, and all three passes found them
load-bearing:

- **Never weaken a ported test to make it pass.** A wrong-looking case is a
  translation bug (fix the port, say so) or a case that does not transfer
  (re-file the row with the reason).
- **Never edit** `harness_test.go`, `names_test.go`, `doc.go`, or
  `harness_selftest_test.go`. Edit `gaps_test.go` only to delete rows you closed
  or re-file rows you re-classified — and **re-file aggressively**.
- **Read upstream before you write.** Every pass has got diagnoses wrong by
  reasoning from a mechanism's name. This one thought plates needed Arceus, that
  Eviolite was blocked by missing data, and that Forewarn was implemented.
- **A comment that contradicts your finding deserves care, not deletion.** This
  pass corrected four: two saying the plates were not in the item set, one
  claiming the engine implements Forewarn, and `TestClone_DeepCopiesEveryVolatile`
  promising that an unlisted pointer volatile "surfaces immediately" when three
  had already slipped past it.
- **Regenerate data, never hand-edit it.** `go run ./cmd/data-sync`.
- **Verify before every push:** `go test ./... -count=1`, `make test-showdown`,
  `make lint`.

`make test-showdown-report` writes `showdown-report.json`, whose `detail` array
carries every blocker per case. Classify from that, never from the one-line
summary in `gaps_test.go` — it truncates to the first blocker with a `(+N more)`
and undercounts badly.
