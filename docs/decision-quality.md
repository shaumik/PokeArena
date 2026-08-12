# Decision quality — scoring how a model *chooses*, not just whether it wins

Win rate answers one question: who won? It cannot tell you *why*. A model can win
because it played well, or because its opponent stumbled, or because the team it
was dealt was strong. Across twelve games that noise doesn't wash out — the
ranking you get is real, but it's coarse, and it hides how a model actually
reasons turn to turn.

Decision quality measures the reasoning directly. For every free choice a model
made in a recorded battle, we ask: **how much better could it have played?**

## How it works

Every live battle stores the full engine state at each turn. The engine is a
pure, deterministic function of `(state, actions)` — given the same state and the
same actions, it always produces the same next state. That lets us do three
things exactly:

1. **Recover the move played.** We re-simulate each stored turn, enumerating the
   legal actions for both sides until we find the pair that reproduces the next
   stored state *byte for byte*. Because the engine is deterministic from the
   stored RNG, that pair is exactly what was played — and the match doubles as a
   validity check on the recording. (Faint turns, where a KO forces a mid-turn
   replacement, are recovered the same way by searching the replacement picks.)

2. **Ask a stronger player what it would have done — from the same view.** An
   expectimax oracle scores every legal action at the decision point. Crucially,
   it decides from `ai.MakeView(state, side)`: the *identical* fog-of-war
   projection the model itself saw. The oracle never sees the opponent's hidden
   moves, exact HP, or bench. It is a better player looking at the same
   information, not an omniscient one — so the comparison is fair.

3. **Measure the regret.** Regret is the value the choice gave up: the oracle's
   value for its best action minus its value for the action the model actually
   played. Regret 0 means the model picked an option the oracle rates as good as
   its own best (there are often several). A large regret means the model left
   real value on the table.

We roll these per-decision regrets up per model:

- **Blunder rate** — the share of choices with regret above a threshold (~a
  third of a Pokémon's worth of position). This is the headline: how often the
  model made a materially bad move.
- **Median regret** — the typical size of a shortfall. We use the median, not
  the mean, because regret is heavy-tailed: a single missed lethal scores off the
  chart (≈ the value of winning), and one of those would swamp a mean.
- **Match rate** — how often the model's move was the oracle's literal top pick.
  It's deliberately coarse: two moves of equal value both count as "right," so a
  low match rate with low regret just means the position had many good options,
  not that the model erred.

## Does the metric actually rank policies correctly?

Before trusting it on models, it should order players whose relative strength is
already known. It does. 72 offline games on **team library v2** — six teams × 3
seeds × 4 policies, each in the scored seat against the same heuristic opponent,
oracle at depth 3:

| policy | win rate (95% CI) | decisions | blunder rate | match rate | median regret |
|---|---|---|---:|---:|---:|
| expectimax d2 | 44% [25, 66] | 513 | **3%** | 68% | 0 |
| expectimax d1 | 61% [39, 80] | 596 | 11% | 43% | 12 |
| heuristic | 56% [34, 75] | 602 | 21% | 27% | 111 |
| random | 0% [0, 18] | 466 | 39% | 22% | 192 |

Blunder rate is monotone in policy strength — random worst, then heuristic, then
the two searches. That is the soundness property the metric needed and had never
been shown to have.

As a cross-check on the harness itself, expectimax d2's 44% independently
reproduces the v2 depth-sweep figure in [benchmark.md §6](benchmark.md) (42.9%,
95% CI [36.8, 49.2]) from a completely different code path — offline capture
here, the `bench` runner there.

(The v2 calibration table above was measured before the heuristic baseline was
fixed for the wasted turns this very metric found — see §6. The fix left the
depth-2 figure unchanged to the exact game, so the cross-check still stands,
but the calibration's absolute numbers describe the pre-fix opponent.)

And then the ordering falls apart the moment you change the judge.

## The judge decides the answer

Expectimax is one family of player. The heuristic is another — depth-0, no
lookahead, no opponent model. Scoring **the same 72 battles** against each:

| policy | vs expectimax d3 | vs heuristic |
|---|---:|---:|
| expectimax d2 | **3%** (best) | 19% (3rd) |
| expectimax d1 | 11% (2nd) | 13% (2nd) |
| heuristic | 21% (3rd) | **2%** (best) |
| random | 39% (worst) | 22% (worst) |

**On the three skilled policies the two judges rank in exactly opposite order.**
Each one crowns its own family. Match rate says it plainly: the heuristic policy
matches the heuristic oracle's top pick 92% of the time and the expectimax
oracle's 27% — same player, same games, same fog-of-war view. The number is
about the judge.

Only one finding survives both judges: **random is worst.**

This is not a small caveat on the metric; it is a limit on what a single-oracle
blunder rate can mean. Reading one as an absolute quality score is reading the
oracle's family resemblance. What the metric can support:

- **A floor.** Both judges separate incompetent play from competent play cleanly.
- **Within-family comparisons.** d1 vs d2 against an expectimax oracle is
  meaningful; heuristic vs d2 against that same oracle is not.
- **Agreement across judges.** A policy both families rate well is genuinely
  well-rated. That is the strongest claim available, and it needs two oracles.

The fog-of-war fairness the method guarantees does nothing about any of this —
information fairness and algorithmic fairness are different axes, and only the
first was ever argued.

## The oracle has to be deep enough to be a judge

A separate finding from the same data, and a hard requirement rather than a
caveat. At oracle depth 2 the expectimax judge gives random 35% and the
heuristic 33% — a 2-point margin, meaning **it cannot tell a random player from
a competent one**. At depth 3 the same comparison is 43% vs 15%.

So depth 3 is not a tuning preference; below it the metric stops working. Note
this cuts against intuition from [benchmark.md §6](benchmark.md), where
expectimax *wins fewer games* as depth rises — playing well and judging well are
not the same capability, and the depth that is bad for one is required for the
other.

## The threshold is a severe cut, not a sloppiness detector

At 300, even the random policy's *median* regret is 192 — below the bar. Blunder
rate counts a genuinely bad tail, which is what it was meant to do, and it is why
median regret is reported next to it rather than instead of it.

## What the model numbers said

> **These are v1-era numbers and cannot be re-run.** They were measured before
> [team library v2](../backlog/2026-08-04T03-40-library-v2-trained-spreads.md)
> gave every pick a nature and an EV spread, so they describe a format that is no
> longer what ships. Unlike the depth sweep — which was re-measured on v2 — these
> cannot be: the battles lived in a local Postgres and their model attribution in
> `/tmp`, and both are gone. Reproducing the table means paying for a fresh batch
> across four vendors, not re-running a script. They are kept here because the
> *shape* of the finding is the point, and labelled because the alternative is
> leaving v1 results sitting next to v2 ones with nothing to tell them apart —
> the exact failure the v2 re-sweep was written up to avoid.

From the attributed batch — twelve live games per model, 48 battles, 945
scored decisions:

| model | win rate | blunder rate | median regret |
|---|---|---|---|
| Gemini 3.1 Pro | 67% | **21%** | 47 |
| Claude Opus 4.8 | **75%** | 25% | 81 |
| Claude Sonnet 4.6 | 25% | 26% | 49 |
| Claude Haiku 4.5 | 8% | 29% | 91 |

The tidy story would be "better decisions, more wins." It's *almost* that — Haiku
is worst on both, and the field broadly tracks — but the interesting part is
where the two measures **disagree**:

- **Opus wins the most (75%) yet is not the cleanest chooser.** It blunders more
  often than Gemini (25% vs 21%) and gives up more when it does (median regret 81
  vs 47). Opus wins "dirtier": it converts good positions even after a shaky move,
  or its raw play strength carries games its per-turn decisions don't fully earn.
- **Gemini is the cleanest decision-maker** — fewest blunders, smallest typical
  regret — while winning less than Opus. It plays the tidiest game on the board;
  the win column doesn't fully reward that here.

That gap is the whole point. Win rate ranked Opus first and Gemini second.
Decision quality flips them on "who plays the cleanest game." Neither view is
wrong — they measure different things, and a benchmark that only reported win
rate would have told you Opus is simply better, full stop, and missed that Gemini
makes fewer mistakes per move.

**That reading no longer stands up, and the two-oracle result above is why.**
"Blunders least, wins less" is exactly what expectimax d2 produces against this
oracle, and there the cause is kinship with the judge rather than cleaner play.
Swapping in a second judge reversed the ranking of every skilled policy.

No language model is an expectimax, so this does not show the model ordering is
*wrong*. It shows there is no evidence it is right: the one time we could check
whether a single oracle's blunder ordering tracks quality, it didn't. The
conclusion that survives is the one both judges agreed on — a floor. Haiku being
worst on win rate *and* blunder rate is consistent with genuinely weaker play.
The Opus/Gemini flip, which was the headline, needs both oracles to mean
anything, and the battles it was computed from no longer exist.

## Caveats

- **Small n.** Twelve games per model. These numbers are directional, not
  final; the blunder-rate gaps between Opus, Sonnet, and Gemini are within what a
  larger sample could move.
- **The oracle is a yardstick, not ground truth.** Expectimax at a fixed depth is
  a strong Gen-1 player but not a solver; a deeper oracle would lower everyone's
  blunder rate and could reorder the close rows. What matters is that *every*
  model is measured against the *same* yardstick from the *same* view.
- **The yardstick has a family, and it decides the ranking.** Measured, not
  suspected: two oracles of different families rank the same three skilled
  policies in opposite orders. Never report a single-oracle blunder rate as a
  quality score. Report the floor, within-family gaps, or agreement between
  families.
- **Depth 3 is a floor for the expectimax oracle.** At depth 2 it cannot
  distinguish random play from competent play (2-point margin). A judge weaker
  than this stops measuring anything.
- **Two oracles is the minimum, not the ideal.** Expectimax and the heuristic
  are both hand-built Gen-1 players and could share blind spots the way they
  share an author. A third of a genuinely different kind — a trained policy —
  would test that, and does not exist yet.
- **The threshold is a knob.** "Blunder" is regret above a fixed cut; the match
  and median columns are there so the picture doesn't hinge on that one number.

## Reproduce it

**No infrastructure needed.** `decision-sim` plays deterministic policies and
writes the same export shape the live path persists, so the whole pipeline runs
from a checkout — this is how the v2 calibration table above was produced, and
re-running it with the same flags reproduces it exactly:

```sh
go run ./cmd/decision-sim -out /tmp/dq -games 3 \
  -policies random,heuristic,expectimax-d1,expectimax-d2

# Score the same battles against each judge and compare the orderings.
go run ./cmd/decision-eval -manifest /tmp/dq/manifest.tsv -oracle expectimax -depth 3
go run ./cmd/decision-eval -manifest /tmp/dq/manifest.tsv -oracle heuristic
```

Run both. A number from one judge alone is not interpretable — see above.

Scoring dominates the wall clock, and only for the searching judge: the 72-game
run takes ~9 minutes against expectimax d3 and ~7 *seconds* against the
heuristic, because the latter does no lookahead.

**From live battles** — the path that produced the model table, and the one that
needs a gateway, a Postgres, and real API keys:

```sh
# Score one battle (stdin/-in is the {seed,winner,turns} export db-replay uses):
decision-eval -in battle.json -side 0 -depth 3

# Per-model table from a batch of attributed runs (maps bid= -> model,
# exports each battle from Postgres, scores, rolls up):
scripts/bench/decision-report.sh /tmp/pk-agentic-v2            # text table
scripts/bench/decision-report.sh /tmp/pk-agentic-v2 --json     # for the report

# Fold the table into the HTML report:
POKEARENA_DQ_JSON=decision-quality.json scripts/bench/build-report.sh
```

Fairness is by construction: `ai.MakeView` is the one projection every agent —
the language models over the live gateway and the expectimax oracle offline —
decides from, and the oracle reconstructs its search from that view alone.
