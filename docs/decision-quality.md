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

**Blunder rate is monotone in policy strength** — random worst, then heuristic,
then the two searches. That is the soundness property the metric needed and had
never been shown to have, and it is now a cheap regression test rather than a
claim (`decision-sim`, below, needs no infrastructure and no API spend).

Two things fall out of it that matter for reading any decision-quality table:

**The oracle is biased toward its own family.** Expectimax d2 has the *best*
blunder rate (3%) and match rate (68%) while posting the *worst* win rate of the
three real policies (44%). It is not the strongest player here; it is the player
most similar to the yardstick, because the yardstick is expectimax d3. Agreement
with the oracle therefore measures algorithmic kinship as well as quality, and
the fog-of-war fairness the method guarantees does nothing about that — it is a
different axis of fairness entirely. See the caveats.

**The threshold is a severe cut, not a sloppiness detector.** At 300, even the
random policy's *median* regret is 192 — below the bar. Blunder rate is counting
a genuinely bad tail, which is what it was meant to do, and it is why median
regret is reported next to it rather than instead of it.

As a cross-check on the harness itself, expectimax d2's 44% independently
reproduces the v2 depth-sweep figure in [benchmark.md §6](benchmark.md) (42.9%,
95% CI [36.8, 49.2]) from a completely different code path — offline capture
here, the `bench` runner there.

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

**One caveat on that reading, added after the v2 calibration above.** "Blunders
least, wins less" is structurally the same result expectimax d2 produces against
this oracle — and there the cause is known to be kinship with the yardstick
rather than cleaner play. That does not explain away the Opus/Gemini gap (no
language model is an expectimax), but it does mean the gap is not self-evidently
"Gemini reasons more cleanly." It could also be that Gemini's style happens to
sit closer to the oracle's. Separating those needs a second oracle of a
different family, which does not exist yet.

## Caveats

- **Small n.** Twelve games per model. These numbers are directional, not
  final; the blunder-rate gaps between Opus, Sonnet, and Gemini are within what a
  larger sample could move.
- **The oracle is a yardstick, not ground truth.** Expectimax at a fixed depth is
  a strong Gen-1 player but not a solver; a deeper oracle would lower everyone's
  blunder rate and could reorder the close rows. What matters is that *every*
  model is measured against the *same* yardstick from the *same* view.
- **The yardstick has a family.** Fog-of-war fairness is by construction, but
  *algorithmic* fairness is not: a policy that searches the way the oracle
  searches will agree with it more often for reasons unrelated to strength. The
  v2 calibration shows this directly — expectimax d2 blunders least while
  winning least. Read a low blunder rate as "close to this oracle," and only
  then as "good," and treat gaps between two policies of the *same* family as
  more meaningful than gaps across families.
- **A stronger oracle is not free of this.** Raising the depth makes the yardstick
  better *and* more expectimax-shaped, so it does not resolve the bias. A second
  oracle of a different family (a trained policy, say) is what would.
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
go run ./cmd/decision-eval -manifest /tmp/dq/manifest.tsv -depth 3
```

Scoring dominates the wall clock (the oracle searches every legal action at
every decision): the run above is 72 games and takes ~9 minutes, nearly all of
it in `decision-eval`.

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
