# Open work from the second battle royale

Everything the second six-team agent tournament turned up that is *not* already
fixed. The four defects it confirmed landed in `#145`, along with the report and
six new rosters; `docs/engine-findings.md` is the record of those and is closed.
This file is the opposite: the things still outstanding, with enough context to
pick one up cold.

Filed as a document rather than as issues because it is meant to be read top to
bottom once and then referenced by path — the ordering below is itself the
recommendation.

**Two things to know before touching any of this.** `bin/royale` is gitignored,
so a stale binary is invisible in `git status` — rebuild it (`go build -o
bin/royale ./cmd/royale/`) before trusting any observed behaviour. And log text
is part of the golden replay fingerprint (`fingerprint` in
`fullgame_integration_test.go` hashes `Type`, `Side` and `Text` for every line),
so nothing cosmetic is free: a reworded line re-records fixtures. The first
tournament learned this the expensive way — a change with no mechanical effect
whatsoever shifted 40 fixtures.

---

## 1. Harvest — implement it — DONE

Implemented as an end-of-turn hook in `internal/engine/abilities.go`, with unit
coverage for both weather branches, the has-item refusal, the berries-only gate,
and a whole-turn test that eats a Sitrus and gets it back. Left below for the
record; nothing outstanding.

Registered inert at `internal/engine/abilities.go:287`.

Canon: at end of turn, if the holder has no item and most recently consumed a
Berry, restore it — 100% of the time in harsh sunlight, roughly 50% otherwise.

The restore already exists. Recycle does exactly this at
`internal/engine/items_moves.go:296-301`, reading the `Pokemon.LastConsumedItem`
field that `loseItem` maintains (`items.go:541`). So this is a weather-gated hook
in `applyAbilityEndOfTurn`, not new state.

*Why it matters beyond completeness:* a tournament roster was built around
Harvest as a declared pillar of its strategy. Its pilot discovered mid-match, by
reading source, that the ability does nothing, and the team effectively played
the whole tournament a Pokémon and a half short.

**Done when:** Harvest returns a consumed Berry at end of turn under sun every
time and about half the time otherwise; does nothing if the holder already holds
an item; a test covers both weather branches and the has-item refusal.

## 2. Close the fog-of-war hole in the harness

**The team name gives away the archetype.** `cmd/royale` prints:

> `You are X in slot p1. Your opponent is The Low Ceiling — their roster and archetype are hidden.`

which is a sentence that refutes itself. The champion of the second tournament
said outright that the name told it Trick Room before turn one, and it led into
the room instead of switching. Every name in `royale/teams/` is a tell: Perish
Row, The Caltrops, Guillotine Club, Miasma, Meridian, The Low Ceiling.

This is the direct descendant of the first tournament's theme-string leak. That
one was fixed in the harness; this one was reopened by naming, and it currently
lives in `royale/RUNBOOK.md` as a rule asking the organiser to choose neutral
names. A rule is weaker than making the leak impossible.

**Suggested shape:** a public codename on the team file, distinct from the
roster's identity, and shown to the opponent in place of the real name.
Touchpoints: `teamFile` in `cmd/royale/main.go`, `cmdTeam`'s foe line, and
`renderView` in `cmd/royale/render.go`.

**Done when:** nothing the opposing agent can read — name, theme, or anything
else — narrows the archetype before the first switch-in.

## 3. Test the broker

`cmd/royale` has **no tests at all**. It is the component that enforces fair
play, and it is the only untested layer in the path: `internal/ai` covers the
engine-side projection well (`itemfog_test.go` and friends).

Invariants worth pinning:

- the opponent's `-why` is stripped before it reaches the other player
  (`printRecords` cuts at `"  // "`, `cmd/royale/main.go:634`) — a leak here
  hands over the opponent's entire read every turn;
- referee commands (`log`, `report`) refuse a wrong or absent judge token;
- `view` never renders any part of the foe's bench;
- `act` refuses an action the engine calls illegal, and refuses a second
  submission in the same phase;
- the turn cap adjudicates on Pokémon standing, then total HP, then draw.

## 4. Make `royale validate` refuse a roster built on nothing

Cheap, and it is what would have caught the Harvest mistake before the
tournament rather than during it. `validate` currently checks legality —
species, learnset, spread, clauses — and says nothing about whether the
mechanics a roster depends on are actually implemented.

**Done when:** validating a team whose pick names an inert ability (or an item
with no registered behaviour) prints a warning naming the slug, and the roster
blurbs in `royale/teams/*.json` can no longer promise something the engine does
not do.

## 5. Damage-model grouping — a decision, then one commit

Two referees independently noted the same approximation, and `damage.go:329-337`
already documents it openly — the comment even names it a "fidelity gap worth
naming": ability and item hooks are applied as a single
lumped multiplier in the final group, so **Sheer Force** behaves as a final
damage multiplier rather than a base-power modifier, and type-boost items
(**Charcoal**, **Poison Barb**) sit in the final group where canon puts them in
the base-power group.

Both were cleared as NOT-A-BUG because the engine states the gap rather than
hiding it. But it is the last named gap in the Showdown-rounding work, and
unlike everything else in this file it changes real damage numbers.

**If you do it:** do both in one commit, because both re-record all 147 golden
fixtures (`go test ./internal/engine/ -run TestFullGame_MatchesGolden
-update-golden`), and re-derive the published tables in `docs/benchmark.md` in
the same change — there is precedent for exactly this in the rounding work, and
a note in `engine-findings.md` about why measurements that cannot be re-derived
are not allowed to sit in that document.

## 6. Neutralizing Gas — the real feature

The one referee-confirmed defect from the second tournament that was *not*
fixed. It is registered inert and now honestly filed as such (a test pins the
documentation against the registry so nothing claims otherwise), but the ability
still does nothing, and a team lost a Pokémon switching Weezing in to suppress
an ability that was never suppressed.

Canon: while the holder is on the field, every *other* ability on the field is
suppressed; on switch-out or faint, suppressed abilities resume.

**Why this is a feature and not a fix:** the registry entry is the easy half.
The read path is `abilityOf()`, which is called from everywhere, so suppression
has to be threaded through it — and then the hard question is what "resume"
means for abilities whose effects already happened. Weather set by Drought
before the gas arrived stays up; an Intimidate that already fired does not
un-fire; but Multiscale and Regenerator must genuinely stop and restart. Related
prior art in the tree: `abilityBreaksMold` (`abilities.go:1323`) for the
ignore-an-ability read, and `Pokemon.BaseAbility` (`battle.go:317`, added in
`#145`) for the save-and-restore pattern.
`abilities.go:1646` already excludes it from `abilityTraceable`.

## 7. Cosmetic log fidelity — probably not worth it

Recorded so nobody re-files them, with a recommendation against acting:

- `"It's super effective!"` prints *after* the damage line; Showdown emits it
  before (sf1 referee).
- Drought's line is generic — `"X's ability set the weather!"`
  (`abilities.go:1369`) — and does not name the ability (r1m3 referee).
- Effect Spore splits its three outcomes uniformly inside the 30% trigger;
  canon is 9/10/11% (r1m3 referee, deliberate and documented at the call site).

The first two are pure presentation and each re-records 147 fixtures. If they
are ever done, bundle them with item 5 so one fixture re-record covers
everything. The third is trivially correctable and genuinely canon-inexact, so
it is the only one of the three with a real argument for doing.

## 8. Forewarn

Inert, and blocked on threading the dex into `OnSwitchIn` so it can rank the
foe's moves by power. Low value on its own; worth doing only if that plumbing is
wanted for something else.

---

## Not in scope, deliberately

`illuminate`, `run-away` and `healer` are inert **by design** in a
trainer-versus-trainer singles battle — wild-encounter rates, fleeing, and
healing an ally that does not exist. They are correctly registered and
documented as such. No action.
