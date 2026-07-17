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

## What the numbers say

From the fresh attributed batch — twelve live games per model, 48 battles, 945
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

## Caveats

- **Small n.** Twelve games per model. These numbers are directional, not
  final; the blunder-rate gaps between Opus, Sonnet, and Gemini are within what a
  larger sample could move.
- **The oracle is a yardstick, not ground truth.** Expectimax at a fixed depth is
  a strong Gen-1 player but not a solver; a deeper oracle would lower everyone's
  blunder rate and could reorder the close rows. What matters is that *every*
  model is measured against the *same* yardstick from the *same* view.
- **The threshold is a knob.** "Blunder" is regret above a fixed cut; the match
  and median columns are there so the picture doesn't hinge on that one number.

## Reproduce it

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
