# The PokéArena Benchmark Board

One chart, one axis: **how often each trainer beats the reference AI.** Sorted
best to worst, colored by the harness that drove it — the SWE-bench layout with a
Pokédex skin.

![PokéArena benchmark board](benchmark-board.png)

Open the interactive version: [**benchmark-board.html**](benchmark-board.html)
(standalone, script-free; needs only a web font + the PokéAPI sprite CDN).

## What the axis means

Every bar is **win rate against the reference expectimax bot** (`expectimax-d2`),
with a **Wilson 95% CI** whisker. Two data sources land on that one axis:

- **Baselines** (deterministic agents) — scored on their head-to-head vs the
  reference in the reproducible round-robin across the 6-team library.
- **Agentic harnesses** (a live model driving the battle through the MCP tools) —
  scored on their record vs the in-engine AI.

The dashed line at 50% is "even with the AI."

## This snapshot (2026-07-05)

| # | Contestant | Harness | Win rate vs AI | n |
|---|------------|---------|----------------|---|
| 1 | **agy · Gemini** | Antigravity (agentic) | **95%** [76–99] | 20 |
| 2 | heuristic | baseline | 72% [66–77] | 239 |
| 3 | expectimax-d1 | baseline | 51% [45–57] | 240 |
| 4 | **claude · haiku** | Claude Code (agentic) | 45% [33–58] | 58 |
| 5 | expectimax-d3 | baseline | 45% [38–51] | 240 |
| 6 | random | baseline | 1% [0–4] | 240 |

Two things the board makes obvious:

- **The harness/model dominates.** Gemini in an agentic harness beats the AI
  ~95% of the time; the same *kind* of setup with claude-haiku sits at a coin
  flip. Same engine, same opponent — the driver is the variable.
- **More search isn't more skill.** A cheap hand-tuned heuristic (72%) beats
  fixed-depth expectimax, and *deeper* search plays *worse* (d1 > d3) in the
  mirror round-robin.

## Honest limitations

- **Different opponents' teams.** Baselines play the mirror-matched library;
  the agentic AI plays tuned teams from `data/ai-teams.json`. Both answer "can
  you beat a strong expectimax bot," but it's not a bit-for-bit continuation —
  read the agentic bars as a **showcase strip**, not a reproducible column.
- **agy is Genesis-only.** Antigravity hit its daily quota after Genesis
  (19–1); Spectrum and Keystone came back 100% unfinished and are **excluded**
  (a config that decides zero battles is dropped, not booked as losses).
  Retriable when the quota resets.
- **Small agentic n.** 20 battles/team on subscription. The baselines are
  n≈240; the CIs are wide on the agentic bars by design.

## Reproduce it

```sh
# 1. Baseline round-robin (free, deterministic) across the library
bin/bench -agents 'random,heuristic,expectimax@1,expectimax@2,expectimax@3' \
  -games 20 -runs runs -out runs/arm1-baseline.jsonl

# 2. Agentic strip (subscription; needs the stack up + MCP built)
scripts/bench/run-batch.sh claude haiku Genesis 20 5 cc-haiku-Genesis
scripts/bench/run-batch.sh agy "Gemini 3.1 Pro (High)" Genesis 20 3 agy-gemini-Genesis
#   ...repeat per team

# 3. Render the board
bin/bench-board -baseline runs/arm1-baseline.jsonl -agentic /tmp/pk-agentic \
  -out docs/benchmark-board.html
```

Raw tallies behind this snapshot live in [`board-data/`](board-data/).
