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
