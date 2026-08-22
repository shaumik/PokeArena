# Findings from the Showdown port

What bringing over Pokémon Showdown's test corpus
([`docs/showdown-port.md`](showdown-port.md)) has said about this engine.

    1989 cases ported     the whole of upstream's test/sim
     254 pass
     612 accounted for    76 engine defects, 536 unimplemented mechanics
    1122 out of scope     singles-only, 80 species, gen-9, no gimmicks

Each entry below has been read back against the engine source and the upstream
case before being written down — a red test is a question, and only the ones
with an answer belong here. The machine-readable list of everything still red,
answered or not, is the `gaps` map in `internal/engine/showdown/gaps_test.go`,
where all 612 rows carry a reason and most carry a `file:line`.

Nothing here is fixed yet. This is the report, not the changelog.

## The shape of what was found

Almost none of these are arithmetic. Two are — toxic residual rounding, and
Struggle taking its quarter from the wrong quantity — and the rest are
**scope-of-application errors**: a rule implemented correctly, then consulted at
some of the places that need it.

That distinction matters for how they get fixed, and for why the engine's own
tests could not have found them. Four recurring shapes:

**A predicate whose signature cannot ask the question.** Mold Breaker is the
clearest: `abilityBlocksSecondaries(def)`, `abilityIgnoresStages(p)`,
`abilityBlocksStatLowerByFoe(def, stat)`, `itemIsRemovable(p)` and
`dampActive(s)` all decide a defender-side question with no attacker in scope,
so the flag that should suppress them is unreachable. Six abilities, one
plumbing change.

**Two predicates for one concept.** Groundedness is answered by `isGrounded` and
by `computeDamage`'s own chain, and neither is a superset of the other, so
Telekinesis dodges Earthquake but still feels terrain and an Iron Ball holder is
the reverse. Gravity is in neither.

**An effect reached from outside the path that guards it.** Intimidate calls
`applyStagesFromFoe` directly and misses the Substitute check that lives in the
move path. Rest calls `doRest` directly and misses every sleep guard in
`inflictStatus`. Both places already re-make *one* of the checks they skipped —
White Herb, Chesto Berry — which is what makes them look considered.

**Ordering fixed by index where canon orders by rule.** Entry effects run
between simultaneous switch-ins instead of after them, hazards apply in a
hard-coded sequence rather than the order laid, terrain heals walk side 0 then
side 1 rather than by Speed, and a double KO is scored a draw because the phase
check happens after both sides are already empty.

A striking number sit next to a comment that documents the gap or asserts the
wrong canon: `terrain.go:45` and `pseudoweather.go:211` name the groundedness
omissions outright; `damage.go:193` claims Ring Target lifting ability
immunities is canon; `damage.go:213` states Gen 8's Charge rule as current;
`turn.go:880` calls the Rapid Spin placement deliberate; `buffs.go:38` says Yawn
bypasses Safeguard. The reasoning is visible and usually half-right, which is
exactly the kind of error a test written from the same mental model will agree
with.

## Confirmed engine defects

### Protect blocks hazard setting, and the cause is the transform default again

*Upstream:* surfaced by the Toxic Spikes, Stealth Rock and Spikes ports.

Measured, one turn, nothing else on the field:

    Snorlax used Protect!
    Gengar used Stealth Rock!
    Snorlax protected itself!          →  no rocks on Snorlax's side

The same for Spikes, Toxic Spikes and Thunder Wave. In a competitive game this
is enormous: the standard answer to a hazard lead becomes "press Protect".

Two causes meeting, and the second is one already in this document.

`protectBlocksFoeMove` (`protect.go:81`) blocks **everything** that is not
`TargetSelf` and does not carry `bypass-protect`. Canon blocks only moves
carrying Showdown's `protect` flag, which hazard and field moves do not have.
So the predicate is inverted: it defaults to blocking and lists the exceptions,
where canon defaults to allowing and lists what is blocked.

That alone would be survivable if hazards were not marked foe-targeting — and
they are, because of the `default: foe` branch in
`cmd/data-sync/transform.go:691` that also gave us
[Howl and Coaching](#howl-and-coaching-boost-the-opponent). Stealth Rock is
`foeSide` upstream, Spikes and Toxic Spikes likewise; all three land in this
dataset as `target: foe`, and Protect then reads them as attacks.

So one unexamined `default` branch produces two unrelated-looking severe bugs:
a move that buffs the opponent, and a shield that walls off the entire hazard
game. Fixing either symptom alone leaves the other. What the transform needs is
to refuse an unrecognized target rather than guess, and what Protect needs is
the `protect` flag — which `flagsAllowlist` currently drops, so the data side
has to move first.

### A thrown Leppa Berry restores no PP

*Upstream:* `Leppa Berry: should restore PP to the first move with any PP
missing when eaten forcibly` (`test/sim/items/leppaberry.js`).

Fling delivers a berry to the target through `flingBerryOnto`
(`items_moves.go:410`), which fires exactly two of the registry's berry hooks:
`OnStatus` and `OnHPThreshold`. Leppa Berry's effect lives in neither. It has
its own trigger, `applyItemPPRestore` (`items_berries.go:262`), called from one
place — `turn.go:530`, immediately after a move pays its PP — so the only way
this engine's Leppa Berry ever fires is on its holder, at the moment one of the
holder's own moves hits zero PP.

That natural trigger is canon. What is missing is the forced-eat path: Fling,
Bug Bite, Pluck and Harvest all activate a berry outside its usual condition,
and canon's Leppa in that case tops up **the first move with any PP missing**,
not the first move at zero. Throw one here and the target eats it, logs
`"ate the thrown Leppa Berry!"`, and gets nothing.

The function's own comment is what makes this look like an oversight rather
than a decision: it lists the hooks it deliberately does *not* fire (the
damage-reaction berries, which have no meaning thrown at somebody) and does not
mention Leppa at all.

Same shape, worth checking together: `applyBerryEatingMove` (Pluck / Bug Bite)
is the other forced-eat path.

### An item gained mid-turn retroactively cancels a status move already chosen

*Upstream:* `Assault Vest: should not prevent the use of Status moves`
(`test/sim/items/assaultvest.js`).

Assault Vest is checked twice: once in `LegalActions` (`battle.go:891`), so the
move never appears on the menu, and once in `executeMove` (`turn.go:552`), as
belt-and-braces against a controller that ignores the legal set. The second
check is the problem — it reads the item **as of execution**, not as of choice.

So the upstream fixture works: a Klutz holder Tricks its Assault Vest onto a
foe who has already selected Calm Mind and is slower. Canon runs the Calm Mind;
the choice was legal when it was made. This engine refuses it and logs
`"cannot use status moves!"`, because by the time the move executes the vest is
on the wrong Pokémon.

The re-check is a good idea and should stay. What it should test is the state
the choice was validated against, not the state at execution.

The same pattern is worth auditing wherever a mid-turn item change can reach a
gate that `LegalActions` also enforces — Trick, Switcheroo, Knock Off, Thief,
Symbiosis and the pinch berries all move items inside a turn.

### Mold Breaker cannot reach half of what it should, and the reason is signatures

*Upstream:* the Shield Dust, Unaware, Clear Body, Sticky Hold, Damp and Levitate
files — six cases that two triage passes reached independently and agreed on.

The flag itself is fine. `BreaksMold` exists, Mold Breaker sets it, and
`abilityBreaksMold` (`abilities.go:1385`) answers correctly. What is wrong is
how it is consulted: at **five hand-placed call sites** (`damage.go:172`,
`damage.go:488`, `turn.go:1120`, `turn.go:1206`, `items.go:1047`) rather than as
a property of the move being resolved. Anything not on that list is unreachable.

And for four of the misses it is worse than an omission — the predicate
*cannot* ask the question, because the attacker is not in its signature:

    abilityBlocksSecondaries(def *Pokemon)                   effects.go:251   Shield Dust
    abilityIgnoresStages(p *Pokemon)                         damage.go:411    Unaware
    abilityBlocksStatLowerByFoe(def *Pokemon, stat string)   effects.go:590   Clear Body, Hyper Cutter,
                                                                              Big Pecks, Keen Eye
    itemIsRemovable(p *Pokemon)                              items_moves.go   Sticky Hold

Adding a `!abilityBreaksMold(atk)` to each call site means changing four
signatures and every caller, which is why this reads as one piece of work
rather than four one-line fixes. The alternative canon uses — suppress the
target's ability for the duration of an ability-ignoring move, the way
Showdown's `Battle#suppressingAbility` does — is a bigger change but makes the
sixth case (a Levitate holder dragged onto Spikes by a mold-breaking Roar) fall
out for free, and that one cannot be fixed by threading an attacker at all,
because `isGrounded` is called from the hazard path with no move in scope.

One caveat found on the way: Showdown's Sticky Hold exempts Knock Off outright,
so upstream's item comes off with or without Mold Breaker. `items.go:576`
documents the divergence deliberately. Fixing the plumbing does not settle that
one.

### Sturdy activates through Endure

*Upstream:* `Sturdy: should not trigger when the user also uses Endure`
(`test/sim/abilities/sturdy.js`).

`dealDamage` orders the survival effects carefully and comments on why: Endure
clamps first, and Focus Sash is deliberately *not* spent when Endure already
saved the Pokemon, "there's no reason to burn the sash" (`turn.go:1374`).

Sturdy is not in that ordering at all. Its clamp is applied at the end of
`computeDamage` (`damage.go:352`), so by the time `dealDamage` runs, the damage
is already capped at HP-1 and Endure's `dmg >= def.HP` test is false. The
Pokemon survives either way — but it announces Sturdy, and canon announces
Endure.

The fix is the same shape as the Focus Sash one already there: Sturdy is a
survival effect and belongs in `dealDamage`'s precedence chain, not upstream of
it. `DamageResult.Sturdy` already carries the flag across the boundary, so the
information is in the right place; only the decision is in the wrong one.

### The ability-changing moves are pickable and silently do nothing

*Upstream:* `Flash Fire: should lose the Flash Fire boost if its ability is
changed` (`test/sim/abilities/flashfire.js`), and every case in the family.

Worry Seed, Skill Swap, Simple Beam and Role Play are all in `data/moves.json`,
all legal to pick, and none of them appears anywhere in `internal/engine`.
Played, they log a successful use and change nothing — not even a "But it
failed!":

    Venusaur used Worry Seed!     Flareon's ability: flash-fire -> flash-fire
    Venusaur used Simple Beam!    Flareon's ability: flash-fire -> flash-fire
    Venusaur used Role Play!      Flareon's ability: flash-fire -> flash-fire
    Venusaur used Skill Swap!     Flareon's ability: flash-fire -> flash-fire

This is the exact failure `internal/engine/inertaudit.go` was written to
prevent, in its own words: "A tournament team declared Harvest a pillar of its
strategy and found out mid-match, by reading source, that the slug was
registered inert." That audit covers abilities and items. The equivalent for
moves, `coverage.go`, can only see gaps that are visible in the *declarative*
shape of the upstream data — and says so:

> Gaps are detected from the **declarative** shape of upstream data only —
> behavior encoded in Showdown JS callbacks (`basePowerCallback`,
> `onModifyMove`, etc.) is invisible to this audit.

Ability replacement is exactly such a callback. So this family sits in the one
blind spot the repo's own tooling documents, which is a good argument for the
port existing: it found a class of gap that no check inside this repository can
see.

(Gastro Acid is the exception and does work — it suppresses rather than
replaces, so it rides a declarative volatile.)

### Mold Breaker pierces six gates where canon pierces twelve

*Upstream:* the `should be suppressed by Mold Breaker` case in
`clearbody.js`, `damp.js`, `shielddust.js`, `stickyhold.js`, `unaware.js`, and
`levitate.js`.

Better stated once than six times, because it is one shape. Mold Breaker is a
flag (`BreaksMold`) consulted at each defender-ability gate rather than a
blanket suppression, which is the right design — canon's list is specific, not
"ignore everything". Today six gates ask: the type-multiplier override, the
crit block, the incoming-damage multiplier, the OHKO immunity, Overcoat and
Soundproof. These do not:

| Ability | Gate | Why it is not a one-line fix |
|---|---|---|
| Shield Dust | `abilityBlocksSecondaries` (3 call sites) | takes only the defender |
| Unaware | `abilityIgnoresStages(def)`, `damage.go:412` | attacker is in scope; the check simply is not made |
| Clear Body | `abilityBlocksStatLowerByFoe`, `effects.go:590` | takes only the defender and the stat |
| Damp | `dampActive(s)`, `turn.go:604` | asks the field, not the attacker |
| Sticky Hold | `itemIsRemovable(p)`, `items.go:581` | takes only the holder |
| Levitate on a forced switch-in | hazard grounding | the dragging move's user is not threaded through |

Only Unaware is a one-line change. The other five are predicates written
without an attacker in scope, which is exactly why the gap exists and what it
costs to close: the signatures have to grow before the condition can be added.
Worth doing as one piece of work rather than six.

### Sandstorm chips on the turn it expires — and an earlier fix picked the wrong side of this

*Upstream:* `Weather damage calculation: should wear off on the final turn
before weather effects are applied` (`test/sim/misc/weather.js`), which asserts
that after five turns of a five-turn sandstorm the target has taken exactly
**four** chips of 1/16.

This engine deals five. `applyWeatherResidual` runs at `turn.go:176` and
`tickWeather` at `turn.go:257`, so the chip always lands before the countdown
and the final turn is chipped by weather that is about to be gone.

What makes this worth reading twice is that
[`engine-findings.md`](engine-findings.md) records a deliberate decision in
exactly this area. A referee found that weather-keyed end-of-turn abilities
(Solar Power, Dry Skin, Rain Dish, Ice Body, Hydration) missed their tick on
the weather's final turn *while sandstorm's chip landed on that same turn*, and
the fix moved the countdown after the ability ticks so that "one residual phase
gives one answer about whether the weather is up".

The inconsistency was real and the diagnosis was right. The resolution went the
wrong way. Canon's single answer is that on the final turn the weather is
**already over** when residuals run — so neither the chip nor the abilities
fire. This engine now consistently fires both.

Fixing it means moving the countdown to the top of the residual phase rather
than the bottom, which restores the consistency the earlier fix was after while
matching canon. Note that this will move the golden replay corpus again
(`internal/engine/testdata/fullgame-golden.json`), as that fix did.

### Residual damage is ordered by side, not by Speed

*Upstream:* `Weather damage calculation: should run residual weather effects in
order of Speed` (`test/sim/misc/weather.js`).

`applyWeatherResidual` walks `for i := 0; i < 2; i++` (`residuals.go:100`) —
player one, then player two, every turn. Canon orders the residual phase by
Speed, fastest first.

Usually invisible, and lethal exactly when it matters: when a chip kills, who
faints first decides what the surviving Pokemon sees. Aftermath, Destiny Bond,
Moxie and a Perish Song countdown all read that order, and so does which side
is asked for a replacement. It is also the sort of thing that makes a replay
diverge from the real game only in the games that were close.

The same fixed-order loop is worth checking across the other residual passes in
`residuals.go`, not just the weather one.

### The recoil family reuses one block that does not fit all of it

*Upstream:* `Rock Head: should not block recoil from Struggle`, `Rock Head:
should not block crash damage` (`test/sim/abilities/rockhead.js`).

`struggleMove` is declared with `Self: &domain.Effect{Recoil: 0.25}`
(`turn.go:15`) and its comment says "25% recoil rides on the user via the
standard self-effect block". That reuse is the defect: the standard block is
not the shape Struggle needs, in two independent ways.

**The fraction is of the wrong quantity.** `effects.go:382` computes
`round(dmgDealt * e.Recoil)` — a quarter of the damage dealt, which is right
for Double-Edge and wrong for Struggle. Since Gen 4, Struggle costs the user a
quarter of its **maximum HP**, whatever it dealt. A Struggle into a resist
therefore costs almost nothing here and a quarter of the bar in canon, which is
the difference between Struggle being a last resort and being free.

**Rock Head blocks it.** The same line is gated `!abilityBlocksRecoil(atk)`, so
a Rock Head user Struggles for nothing. Canon exempts Struggle from Rock Head
specifically — it is not recoil in the sense the ability cares about. The Magic
Guard half of the same condition *is* correct and should stay.

**Crash damage does not exist.** High Jump Kick and Jump Kick carry only a
`contact` flag in `data/moves.json` — no self-effect — and nothing in
`internal/engine` mentions a crash. A missed High Jump Kick costs its user
nothing, where canon takes half its maximum HP. This is the third member of the
same family and the reason to fix them together: recoil, Struggle recoil and
crash damage are three different rules that this engine currently models as one
rule and a half.

### Groundedness is modeled twice, and the two copies disagree

*Upstream:* the behavior failures in `ironball.js`, `ringtarget.js`,
`gravity.js`, `ingrain.js`, `electricterrain.js`, `grassyterrain.js`,
`mistyterrain.js`, `levitate.js`, `toxicspikes.js` — around twenty cases, which
is why this is worth stating as one thing.

Canon has a single predicate. Everything that cares whether a Pokemon is on the
ground — Ground-type immunity, Spikes, Toxic Spikes, Arena Trap, every terrain
effect — reads the same answer. This engine has two, and they were written for
different questions:

**`isGrounded(p)`** (`terrain.go:50`), read by fourteen sites: Arena Trap,
Spikes, Toxic Spikes and all of the terrain rules. It considers Iron Ball, Air
Balloon, Flying type and Levitate. It does not know about Magnet Rise,
Telekinesis, Smack Down, Ingrain or Gravity.

**An ad-hoc chain inside `computeDamage`** (`damage.go:172-198`), for
Ground-move immunity only. It considers Levitate, Air Balloon, Smack Down,
Magnet Rise, Telekinesis and Ring Target. It does not know about Iron Ball,
Ingrain or Gravity.

Neither list is a superset of the other, so the same Pokemon can be on the
ground for one rule and airborne for another. Verified directly:

    Iron Ball Charizard, hit by Earthquake     153/153 HP   (canon: grounded, takes it)
    Gravity up, Charizard hit by Earthquake    153/153 HP   (canon: grounded, takes it)
    Smack Down Charizard, hit by Earthquake      0/153 HP   (correct)

Iron Ball is the sharpest illustration, because `isGrounded` checks it *first*
and comments on why — "Iron Ball drags the holder down regardless of typing or
Levitate, so it is checked before either of them". That reasoning is right and
the damage path never sees it. A Flying-type holding an Iron Ball is grounded
for Electric Terrain and immune to Earthquake at the same time.

By source reading, the mirror gaps are: a Smack Down or Magnet Rise target has
the right Ground-move behavior but the wrong Spikes and terrain behavior; an
Ingrained or Gravity-affected Pokemon is airborne for everything.

The fix is to have one predicate and delete the other, which is a bigger change
than any single case here suggests and much smaller than twenty separate ones.
It is also the finding most likely to matter in a real game: Spikes plus a
Levitate or Flying pivot is ordinary play, and this engine currently answers
that board differently depending on which rule is asking.

### Intimidate reaches through a Substitute

*Upstream:* `Intimidate: should be blocked by Substitute`
(`test/sim/abilities/intimidate.js`).

The Substitute guard for foe-induced effects lives in `applyEffect`
(`effects.go:303`) — the path a *move's* effects take. Intimidate does not take
that path: its hook calls `applyStagesFromFoe` directly (`abilities.go:211`),
and `applyStagesFromFoe` checks Mist, the Clear Body family and Clear Amulet,
but not Substitute.

The same shape as the Mold Breaker finding, and here the author saw it coming.
The line immediately after the drop reads:

> Intimidate reaches the foe from applyOnSwitchIn, nowhere near a move's boosts
> block, so the herb check has to be made here.

Exactly right — being outside the move path means the checks that path performs
have to be re-made locally. The White Herb one was; the Substitute one was not.

### Entry effects run between simultaneous switch-ins instead of after them

*Upstream:* `Intimidate: should wait until all simultaneous switch ins after
double-KOs have completed before activating`.

`ResolveReplace` walks the sides in index order and calls `doSwitch` for each
(`turn.go`), and `doSwitch` runs hazards and the switch-in ability hook
immediately for the Pokemon it just brought in (`switching.go:114-117`). After
a double KO that produces an asymmetry:

1. p1's replacement enters and its Intimidate fires — against p2's slot, which
   still holds the corpse. The hook's `if foe.Fainted { return }` swallows it.
2. p2's replacement enters and its Intimidate fires normally, against p1's new
   active.

So p1's Pokemon is intimidated and p2's is not, and which side gets the boost
depends only on side index. Canon brings both replacements in first and then
runs entry effects, which is what the upstream case is named after.

This is not Intimidate-specific — it is the whole entry phase. Drought and
Drizzle racing on a double KO resolve by side index rather than by Speed, and a
hazard that KOs one replacement changes what the other one's entry sees.

Worth noting the lead path already does this correctly: `turn.go:60-62` installs
both leads and *then* calls `applyOnSwitchIn` for each. The replace path is the
one that interleaves.

### Howl and Coaching boost the opponent

*Upstream:* surfaced by `Dancer: should only copy dance moves used by other
Pokemon` (`test/sim/abilities/dancer.js`), which read +3/+0 where it expected
+2/+3 and made the Howl in its fixture visible.

Not an engine defect — a data-pipeline one, and the only finding so far that
lives outside `internal/engine`.

`cmd/data-sync/transform.go:683` maps Showdown's target vocabulary onto this
engine's two values. Fifteen upstream targets collapse to `foe` or `self`,
which is the right simplification for a singles engine — but the `default`
branch sends *everything* unrecognized to `foe`, and that sweeps up the
ally-facing targets:

    howl        upstream target "allies"       →  foe
    coaching    upstream target "adjacentAlly" →  foe

Both are self- or ally-boosting moves. Measured directly:

    Snorlax used Howl!       Snorlax atk +0   |  Chansey atk +1
    Snorlax used Coaching!   Snorlax atk +0   |  Chansey atk +1, def +1

They are legal to pick, and using either hands the opponent a free boost. Howl
is on eleven species' learnsets in this dataset.

Sixteen of our moves have ally-facing upstream targets, but the other fourteen
are unaffected — Reflect, Light Screen, Aurora Veil, Mist, Safeguard, Tailwind,
Quick Guard and Wide Guard ride dedicated side-condition handlers that never
read the target field (verified: Reflect correctly lands on the user's side),
and the rest are unimplemented for other reasons. Howl and Coaching are the two
that reach the generic `primary.boosts` path, where the target decides who
gets the stat.

What is worth fixing is not only the two rows. The `default` branch carries this
comment:

> keep going — schema requires target only for status moves; damage moves
> default to foe. If this is a status move with unknown target, the validator
> will catch it.

The validator checks that a target is *present*, not that it is right, so
nothing caught it. An unrecognized target should be an error in the transform
rather than a silent `foe`: this dataset holds only two target values out of
Showdown's fifteen, and the collapse is only safe for the ones somebody has
actually looked at.

### Groundedness is two predicates that disagree, and Gravity is in neither

*Upstream:* the Gravity, Iron Ball, Ring Target, Arena Trap and terrain files —
eleven cases across five subsystems, all one root.

This engine answers "is this Pokemon on the ground?" twice, in two places, with
two different lists:

- `isGrounded` (`terrain.go:50`) knows Iron Ball, Air Balloon, Flying type and
  Levitate. Fourteen call sites depend on it: every hazard, all five terrain
  effects, and Arena Trap's switch block.
- `computeDamage` has its own chain (`damage.go:173-195`) for Ground-move
  immunity, and that one knows Levitate, Air Balloon, Magnet Rise, Telekinesis
  and Smack Down.

Neither is a superset of the other, and **Gravity is in neither**. The
consequences are one bug each way:

- A Telekinesis'd Pokemon is immune to Earthquake but still counts as grounded,
  so it takes Spikes and Electric Terrain still refuses to let it sleep.
- An Iron Ball holder is grounded for hazards and terrain but keeps its Flying
  or Levitate immunity to Earthquake, because `itemGrounds` never reaches the
  damage path.
- Gravity grounds nobody at all. `gravityActive` (`pseudoweather.go:213`) has
  exactly one caller — the accuracy boost in `resolveAccuracy`.

None of this is hidden. `terrain.go:45` says it plainly:

> Not folded in yet: Gravity and Ingrain, which canon checks *above* Iron Ball
> and which would ground a floater; and Magnet Rise, Telekinesis, Roost and
> Smack Down. Magnet Rise and Telekinesis are modeled as a Ground-move immunity
> in damage.go but do not reach this predicate, so their holders still take
> Spikes and still feel terrain.

`pseudoweather.go:211` says the same about Gravity. So the port did not discover
this; it **measured what the note costs** — eleven upstream cases across five
subsystems that a reader of either comment would not have predicted. That is
the argument for one predicate rather than two, which is a bigger change than
adding Gravity to both.

### Toxic damage truncates in the wrong place

*Upstream:* `Toxic Poison: should inflict 1/16 of max HP rounded down, times
the number of active turns with the status` (`test/sim/misc/statuses.js`).

`residuals.go:31` computes the tick as `p.MaxHP * p.ToxicCounter / 16` — one
truncation, after the multiply. Canon truncates the sixteenth first and then
multiplies. On a 325 HP Chansey:

    engine   20  40  60  81  101  121  142  162
    canon    20  40  60  80  100  120  140  160

The two agree for the first three ticks and diverge from the fourth on, which
is exactly why this survived: a test that badly poisons something and plays
three turns sees the right numbers.

Worth reading next to `docs/engine-findings.md`'s OPEN-3, which was the same
class of mistake — damage carried unrounded and floored once at the end, where
Showdown truncates at each boundary — found and fixed for the *move* damage
path. Residual damage was not part of that sweep. It is the same question asked
in a different room.

### Entry hazards fire in a fixed order rather than the order they were set

*Upstream:* `Hazards: should allow Berries to trigger between hazards`
(`test/sim/misc/hazards.js`).

`applyHazardsOnSwitchIn` (`hazards.go:83`) always applies Stealth Rock, then
Spikes, then Toxic Spikes, and returns as soon as the switch-in faints
(`hazards.go:98`). Canon applies the layers in the order they were laid.

The upstream fixture is built to make the difference lethal: Toxic Spikes is set
first, so canon poisons the incoming Pokemon, its Lum Berry is spent curing the
poison, and only then do the rocks land. Here the rocks kill it first and the
Toxic Spikes never run — the berry is still held on a fainted Pokemon.

The berry hook itself is correct (`inflictStatus` calls `applyItemStatusCure`,
`effects.go:572`). Only the fixed ordering is wrong, and it needs the side to
remember the order its layers went down.

### Every double KO is a draw

*Upstream:* `Speed ties: (slow) Perish Song faint order should be random`
(`test/sim/misc/turn-order.js`).

`updatePhase` scores a mutual wipe as a draw unconditionally (`turn.go:1556`).
Gen 5 onward decides it by faint order — the side whose last Pokemon faints
*first* wins — and on a speed tie that order is random. Measured over 50 seeds,
a Perish Song mirror ends `Winner = 2` on every one of them.

There is no order to appeal to yet either: `tickPerishSong` walks side 0 then
side 1 in fixed order (`turn.go:270`), the same index-order pattern as the
double-KO entry bug above.

This one is a product decision as well as a fix.
`docs/battle-state.md:793` documents the draw as an intended outcome, Elo scores
it 0.5, and the SPA renders a banner for it. Worth deciding deliberately rather
than inheriting.

### A pinch berry is eaten off the very hit that should have taken it

*Upstream:* `Sitrus Berry: should not heal if Knocked Off`
(`test/sim/items/sitrusberry.js`).

Pinch-item HP triggers fire inside the hit loop — `dealDamage` calls
`applyItemHPTriggers` at `turn.go:1462`, immediately after applying damage —
while a move's item removal runs later, in `executeMove` via
`applyItemMoveAfterHit` (`turn.go:905`) into `knockItemOff`
(`items_moves.go:118`).

So the order inverts. Knock Off brings the holder into Sitrus range, the berry
fires on that same hit, and `knockItemOff` then finds an empty slot:

    Mewtwo took 88 damage
    Mewtwo ate its Sitrus Berry!  (+45)      ← and no knock-off line at all

Canon fires Sitrus from `eachEvent('Update')` at the end of the action, after
`onAfterHit`. The same inversion applies to Thief and Covet against every pinch
item, so this is one ordering decision rather than three bugs.

Worth noting the upstream case's other assertion — that the item is gone —
passes here, for the wrong reason.

### Heavy-Duty Boots stop Toxic Spikes being absorbed

*Upstream:* `Toxic Spikes: should be absorbed by grounded Poison types`
(`test/sim/moves/toxicspikes.js`).

`applyHazardsOnSwitchIn` returns for a Heavy-Duty Boots holder before any
hazard is consulted (`hazards.go:90`, commented "checked once here rather than
per hazard"), which puts the early return above the Poison-type absorb branch
at `hazards.go:183`.

Canon runs the absorb regardless: the boots stop the wearer being poisoned, not
the layers being soaked up. So a booted Tentacruel should clear the field and
here leaves it laid for whatever switches in next. The grounding gate itself is
right — a Levitating Weezing correctly does not absorb.

One guard moved below one branch.

### Ring Target lifts immunities it should not, and flattens the ones it should

*Upstream:* three cases in `test/sim/items/ringtarget.js`.

`computeDamage` implements the item as one line: `if eff == 0 &&
itemLiftsOwnImmunities(def) { eff = 1 }` (`damage.go:197`). Three things go
wrong at once.

**It flattens rather than recomputes.** Ground on an Electric/Flying holder
should be 2× — the Flying immunity lifts and the Electric half decides — and
lands 1×. Fighting on Ghost/Poison should be 0.5× and lands 1×. Neither
effectiveness line is printed. The lift belongs per-type inside
`effectivenessWithLifts` (`aim.go:183`), where Foresight and Scrappy already do
exactly this.

**It lifts ability immunities.** The line runs after `abilityTypeMultOverride`,
so a Levitate holder with a Ring Target takes Earthquake. Canon's Ring Target is
a type-chart negation only.

**It lifts volatile immunities.** Same line, one gate later: Magnet Rise's zero
becomes a one.

The comment at `damage.go:193` asserts that lifting the ability and volatile
immunities is canon. It is not, and that claim is why the line reads as
considered.

### Destiny Bond can be re-armed every turn

*Upstream:* `Destiny Bond: should fail if used consecutively`.

`applyDestinyBondVolatile` has the consecutive-use guard and emits "But it
failed!" when the volatile is already up (`statusvols.go:159`). It can never
fire, because `ResolveTurn`'s transient sweep clears `Volatiles.DestinyBond` at
the top of every turn (`turn.go:294`), next to Protect and Endure.

So the guard only works twice inside one turn, and Destiny Bond is re-armable
indefinitely — a Pokemon can hold the threat up every turn it is alive. Canon
keeps the volatile until the user's next non-Destiny-Bond move and fails a
back-to-back use.

### Rapid Spin clears hazards after its user has fainted

*Upstream:* `Rapid Spin: should not remove hazards if the user faints`.

The sweep is gated on `hits > 0` (`turn.go:884`) and `applyRapidSpin`
(`hazards.go:336`) never looks at the user's HP, so a spinner killed by Rocky
Helmet still clears the field:

    Golbat fainted!
    Golbat blew away the hazards!

Canon gates every `removeSideCondition` on `pokemon.hp`. The comment at
`turn.go:880` says the placement is deliberate — "so a contact-faint counter
(Rough Skin) doesn't suppress the spin sweep" — which is precisely backwards,
and makes a suicide spin free.

### Charge is running the Gen 8 rule inside a Gen 9 engine

*Upstream:* two cases in `test/sim/moves/charge.js`.

`turn.go:914` clears `Volatiles.Charge` after **any** damaging move regardless
of type, and that line sits below the status-move early return at
`turn.go:799`, so no status move ever clears it. Gen 9 is the other way around:
the charge is spent by any Electric move including a status one, and survives
everything else.

Both halves are inverted, so Charge is simultaneously too easy to lose (Air
Slash eats it) and impossible to lose to Thunder Wave. The comment at
`damage.go:213` states the Gen 8 rule as if it were current, which is why this
reads as a decision rather than drift.

### A target's Soundproof cancels the whole sound move

*Upstream:* `Perish Song: should not affect other Pokemon with the ability
Soundproof`.

`resolveAccuracy` refuses a sound move outright when the target has Soundproof
and reports it as not landing (`turn.go:1206`), so `executeMove` never reaches
`applyStatusMove` and `applyPerishSong` never runs.

Soundproof is a per-target immunity in canon, not a veto on the move. A
field-wide sound move still starts the count on everything that heard it —
including, in this case, the Soundproof user's own side. Getting this right
matters more the moment anything other than singles exists.

### No self-heal move fails at full HP, and Roost pays for it

*Upstream:* `Roost: should fail if the user is at max HP`.

The declarative heal calls `healPokemon` (`state.go:35`), which caps at MaxHP
and logs nothing when HP does not move. Only Rest carries a full-HP gate
(`effects.go:169`). So Roost, Recover, Soft-Boiled, Slack Off and Milk Drink all
resolve silently at full HP — no heal line, no "But it failed!".

For four of those five that is cosmetic. For Roost it is not:
`effects.go:193` sets `Volatiles.Roost` **before** the heal is attempted, so a
Roost that should have failed still strips the user's Flying type for the turn.
A full-HP Aerodactyl becomes Ground-vulnerable for free — in the sibling case it
takes 24 from a Mud-Slap it should be immune to.

### Wonder Room swaps the stat stages and item multipliers too

*Upstream:* three cases in `test/sim/moves/wonderroom.js`.

`offensiveDefensiveStats` swaps the defensive slug (`damage.go:402`) and then
reads the raw stat, the stat *stage*, and the stat-modifying *item* off the
swapped slug (`damage.go:415`, `damage.go:424`). Canon swaps only the stored raw
stat; stages and items stay attached to the stat the move's category names.

The two halves come out exactly inverted. Against a physical hit under Wonder
Room, measured: 48 plain, 44 after Defense Curl, 32 with an Assault Vest — where
canon wants 48, 32, 48. Defense Curl's protection and the Assault Vest's
protection have swapped places.

Two smaller siblings ride along: the offensive override is not routed through
the swap at all, so Body Press keeps reading Defense under Wonder Room; and
Download compares raw stats only (`abilities.go:663`, and its comment says so),
so it cannot see the stage the swap is supposed to move.

### Clear Smog and Anger Point fire in the wrong order

*Upstream:* `Clear Smog: should trigger before Anger Point activates during
critical hits`.

Anger Point runs inside the strike loop, off the crit announcement in
`dealDamage` (`turn.go:1429`). A move's own `onHit` callback runs after the loop
(`turn.go:891`). So Anger Point sets +6 and Clear Smog then wipes it to zero:

    Primeape's Anger Point maxed its Attack!
    Primeape's stat changes were removed!

Showdown fires `singleEvent('Hit')` — the move — before `runEvent('Hit')` — the
ability — so the boost has to land last. Worth reading the triage note on this
one: the fix is to defer the crit reaction out of `dealDamage` to after the
move's own callbacks, not to reorder two arms of a switch.

The same file's other case is a plain Substitute miss: the Clear Smog callback
is gated on `hits > 0` alone, and a strike the doll eats still counts, so stat
boosts are wiped through a Substitute. The flag needed is already in scope —
`subAte` is computed two statements earlier and consulted by the item-theft
moves.

### Ingrain is rooted in name only

*Upstream:* all three cases in `test/sim/moves/ingrain.js`.

The volatile is set and heals correctly, and `LegalActions` refuses a voluntary
switch. Nothing else that Ingrain does is modeled:

- **It does not ground.** `isGrounded` never reads it (the note at
  `terrain.go:45` lists Ingrain among the omissions), so a rooted Pidgeot still
  reads 0× against Earthquake.
- **It does not stop phazing.** `applyForceSwitch` (`forceswitch.go:32`) guards
  only on Fainted and an empty bench — its header names Suction Cups as the
  only blocker — so Roar, Whirlwind, Dragon Tail and Circle Throw drag a rooted
  Pokemon out. `ingrainBlocksSwitch` is wired into `LegalActions` alone.
- **It does not Baton Pass.** `batonCarry` copies Stages, Confusion and
  Substitute (`switching.go:26`) and nothing else; the comment there already
  concedes the set is narrower than canon.

### Safeguard's comment states the wrong canon, and Yawn goes through

*Upstream:* `Yawn: should be blocked by Safeguard`.

`safeguardBlocksFoeVolatile` gates on exactly one slug — `return slug ==
"confusion"` (`buffs.go:176`) — and the comments at `buffs.go:38` and
`buffs.go:167` assert as canon that "Yawn bypasses Safeguard".

That is backwards, and interestingly so. Showdown's Safeguard refuses Yawn in
`onTryAddVolatile` *and* exempts it in `onSetStatus`. The exemption is real, but
it is what lets an **already-landed** Yawn's delayed sleep through — not what
lets Yawn land in the first place. Half the rule was read as the whole of it.

The consequence: a Pokemon falls asleep behind its own Safeguard. And the
sibling case that asserts the delayed sleep does land currently passes for the
wrong reason.

### Moxie boosts on the KO that ends the battle

*Upstream:* `Moxie: should not boost Attack when its user KOs the last Pokemon`.

`applyOnKO` fires inline from `executeMove` the moment the foe hits 0
(`turn.go:962`) and checks only that the attacker is alive. The win check lives
in `updatePhase`, at the very end of `ResolveTurn` (`turn.go:1555`).

Canon's `faintMessages` runs `checkWin` *before* `runEvent('AfterFaint')`, so a
battle-ending KO never reaches Moxie. Here Gyarados finishes the game at +1.

Harmless on its own — the battle is over — but the ordering is shared with every
other `AfterFaint` reaction, and it is the same "the phase check happens last"
shape as the double-KO draw.

## Not defects — what the tally already ruled out

Worth writing down so nobody re-files them.

**The roster, not the engine.** Nine of the first twenty-six out-of-scope cases
are Mimikyu and Disguise; three more are Prankster's Dark-type immunity, which
cannot be built at all because no Dark-type species is in this 80-species Kanto
dex. These are limits of what the dataset can express, not disagreements about
rules.

**Unimplemented is not wrong.** Normalize, Pick Up, Transform, Sky Drop,
Techno Blast, Judgment, Double Iron Bash and typed Hidden Power all produce red
cases, and all of them mean "this engine does not have that", which is a
feature request rather than a defect. The harness says so explicitly now:
`p.battle` asks `AbilityInertReason` / `ItemInertReason` and records the
engine's own sentence, so these separate themselves from real bugs without a
human reading each one.

**Hidden Power is a representation difference.** Showdown carries one move id
per type (`hiddenpowerfighting`); this dataset carries a single `hidden-power`.
A port naming the typed id fails to resolve, which reads as a missing move but
is really a question about how the dataset models the move.
