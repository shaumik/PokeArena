# Findings from the Showdown port

What bringing over Pokémon Showdown's test corpus
([`docs/showdown-port.md`](showdown-port.md)) has said about this engine.

Each entry has been read back against the engine source and the upstream case
before being written down — a red test is a question, and only the ones with an
answer belong here. The machine-readable list of everything still red, answered
or not, is the `gaps` map in `internal/engine/showdown/gaps_test.go`.

Nothing here is fixed yet. This is the report, not the changelog.

## Confirmed engine defects

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

### Mold Breaker does not pierce Shield Dust or Unaware

*Upstream:* `Shield Dust: should be negated by Mold Breaker`
(`test/sim/abilities/shielddust.js`), `Unaware: should be suppressed by Mold
Breaker` (`test/sim/abilities/unaware.js`).

Mold Breaker is a flag (`BreaksMold`) read at each defender-ability gate rather
than a blanket suppression, which is the right shape — canon's list of what it
pierces is specific. Six gates consult it today: the type-multiplier override,
the crit block, the incoming-damage multiplier, the OHKO immunity, Overcoat and
Soundproof. Two that canon includes are missing:

- **Shield Dust.** `abilityBlocksSecondaries` is called from three places —
  `effects.go:251`, `callbackmoves.go:355`, `items_reactive.go:246` — and none
  of them takes the attacker's ability into account.
- **Unaware.** `abilityIgnoresStages(def)` at `damage.go:412`, inside
  `offensiveDefensiveStats`. The function already has the attacker in hand, so
  the check is available; it simply is not made.

Both are one condition each, and both are the kind of omission a list-shaped
implementation invites: the flag is right, the list is short.

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
