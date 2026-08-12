# Fixing the baseline the metric found

The verifiable-error metric was built the day before to measure *contestants*.
The first thing it measured was our own reference opponent, and it found the
heuristic spending turns on moves the rules make impossible: 27 boost-at-cap
turns across six games, and Thunder Wave re-applied to an already-paralysed
target for six consecutive turns.

## Two bugs that look like one

They are not the same mistake, and the difference is the interesting part.

**The boost branch had no check at all.** It valued a self-boost at 55 while
healthy, never consulting `me.Stages`. At +6 the move cannot change anything and
it still scored 55, which beats most attacks. So the agent boosted, and boosted,
and boosted.

**The status branch did have a check** — `return 0` with the comment "a status
move is wasted on an already-statused foe." Someone saw this exact case and
guarded it. The guard was just not strong enough: a damaging move that deals no
damage also scores 0, and `Decide` breaks ties toward the earliest legal action,
so a dead status move in an early move slot won the tie and was replayed every
single turn.

That second one is the one worth remembering. The code contained the right
belief, correctly commented, and was still wrong — because 0 is not a neutral
value in a scoring function whose other outputs bottom out at 0. The fix is a
`deadMoveScore` constant well below anything a live option can score, including
switching, since giving up a turn to reposition genuinely beats spending it on a
guaranteed no-op.

## Not the engine, and that distinction did the work

Every one of these moves *fails correctly* in the engine. That is precisely why
they were detectable — the metric watches for actions that provably cannot
accomplish anything, and the engine's correct rejection of them is what makes
"provably" true.

So the blast radius was bounded in a way worth stating: rules, replays,
determinism, fairness — all untouched. What changed is one *player*. But that
player is the opponent every published figure in §6 is measured against, so
"only the bot" still meant re-measuring the document's only load-bearing table.

## Re-sweeping, and being wrong about what I expected

I expected the fix to move the numbers a little and braced for having to
re-caveat §6. 240 games per depth, same methodology as before:

| Depth | pre-fix | post-fix |
|---|---:|---:|
| 1 | 54.4% | 53.8% |
| 2 | 42.9% | 42.9% |
| 3 | 42.1% | 42.1% |

Depths 2 and 3 are identical to the exact game — 103/240 and 101/240 both times.
Depth 1 moved by two games out of 240.

The reason, once you look: the wasted turns clustered in already-decided stall
positions. The six-turn Thunder Wave loop was on the Bastion wall team at turns
72–77 — a game whose outcome was long settled. A bot wasting turns in a position
it has already won or lost does not change the result. Twenty-seven wasted turns
sounds like a lot until you notice which turns they were.

Two things I'd have gotten wrong by reasoning instead of measuring: I would have
guessed the effect was larger, and if the numbers *had* moved I would have had
no way to tell whether the fix or ordinary variance did it. Re-running the whole
sweep on a deterministic offline harness costs 22 minutes of compute and no
money, which is a very cheap way to convert a guess into a fact.

## The sweep is now committed

It had been a scratch test. That was the same mistake as the last three rounds
in miniature — a published number whose derivation lives in a `/tmp` file. It
is now `TestDepthSweep`, gated behind `POKEARENA_DEPTH_SWEEP=1` so it never
runs in the suite, and §6 carries the command. The figures in the document can
be re-derived rather than trusted.

## What I did not fix, and why that was the harder call

The heuristic also under-values Rest — the move carries no effect block in the
dataset, so it never reaches the heal branch and lands on a flat score of 5.
Fixing that would make the baseline meaningfully stronger.

I left it. The line I drew: **correct a provable waste, do not change a
strategy.** A move that cannot possibly work is a defect in any sense of the
word, and fixing it makes the bot do what it already meant to do. Teaching it to
use Rest well is a judgement about how the game should be played, and it would
change the baseline's strength — which is a decision about what the benchmark
measures, not a bug fix, and it should be made deliberately and announced rather
than smuggled in beside a correctness patch.

The same line explains why the ability-immunity case is absent from the metric
itself: Levitate blanks a Ground move, but the attacker may not know the
ability, so charging it would penalise unknowable information.

## The loop that closed

Built a metric to judge outside contestants; it immediately indicted the
reference opponent. That is a good sign about the metric and an uncomfortable
one about the baseline, and the useful version of both is that the tool works on
its author. Worth expecting the next measurement instrument to do the same.
