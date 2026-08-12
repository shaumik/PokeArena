# Running the benchmark

Practical how-to for the PokéArena benchmark. For *what* it measures and *why*
(scope, metrics, limitations), see [benchmark.md](benchmark.md); this doc is
just the commands.

There are two arms:

1. **The deterministic benchmark** (`cmd/bench`) — programmatic agents and/or
   LLMs in a **thin one-shot harness**, run in-process, reproducible to the byte.
   No running stack required.
2. **The agentic-harness comparison** — the *same models* wrapped in a full
   agent runtime (Claude Code, Antigravity) playing live battles through the MCP
   tools. Requires the stack up. This is how we measure "harness matters."

---

## 1. Deterministic benchmark (`cmd/bench`)

Build once:

```sh
go build -o bin/bench ./cmd/bench
```

Programmatic baselines across the whole team library, 20 seeds each (both side
orientations), Elo + Wilson-interval win rates + a persisted run record:

```sh
bin/bench -agents random,heuristic,expectimax -games 20
```

Key flags:

| Flag | Default | Meaning |
|------|---------|---------|
| `-agents` | `random,heuristic,expectimax` | programmatic contestants |
| `-llm` | — | LLM contestants as `[label=][vendor:]model[/condition]` (keys from `<VENDOR>_API_KEY`) |
| `-games` | `20` | seeds per pairing per team (each played in both orientations) |
| `-teams` | `data/benchmark-teams.json` | competitive library; every team is mirror-matched and aggregated |
| `-team` | — | ad-hoc override: comma-separated dex numbers, mirrored to both sides |
| `-depth` | `2` | fixed search depth for the expectimax agent |
| `-budget-ms` | `0` | per-decision time budget in ms (recommended for LLM agents) |
| `-out` | stdout | JSONL trace path |
| `-runs` | `runs` | dir to persist the run record + append the index (`""` to disable) |
| `-pricing` | `data/model-pricing.json` | model pricing table for costing token usage |
| `-cot-budget` | `2048` | thinking token budget for `/cot` contestants |

Every run prints standings, a per-team Elo breakdown, and (for LLMs) **measured**
token cost, then saves a full record to `runs/<id>.json` and appends `runs/index.jsonl`.

### Add an LLM (thin harness)

```sh
export ANTHROPIC_API_KEY=sk-...
bin/bench -agents heuristic \
  -llm 'haiku=claude-haiku-4-5-20251001' \
  -games 10
```

The label (`haiku`) names the contestant; the value is the API model id, which
must have an entry in `data/model-pricing.json` for cost to be attributed (a
model with tokens but no price is reported as cost-unknown, never as free).
`expectimax@N` as an `-agents` entry enters a distinct fixed-depth contestant
(e.g. `-agents 'expectimax@1,expectimax@3'` for a depth sweep).

### Vendors and conditions — the board columns

The `-llm` grammar is `[label=][vendor:]model[/condition]`:

- **vendor** — `anthropic` (default), `openai`, `gemini`, or `ollama`. Each
  reads its key from its own env var (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`,
  `GEMINI_API_KEY`); `ollama` runs locally and needs no key. A run touches only
  the vendors it uses.
- **condition** — the standardized harness column: `raw` (default; one shot, no
  thinking) or `cot` (one shot, native thinking on, sized by `-cot-budget`). A
  bare model in `cot` mode auto-suffixes its label (`model:cot`) so the same
  model appears once per column.

So a cross-vendor Raw-vs-CoT board is one command:

```sh
export ANTHROPIC_API_KEY=... OPENAI_API_KEY=... GEMINI_API_KEY=...
bin/bench -agents expectimax \
  -llm 'claude-haiku-4-5-20251001/raw,claude-haiku-4-5-20251001/cot,openai:gpt-5/raw,openai:gpt-5/cot,gemini:gemini-2.5-flash/raw,ollama:llama3.1:8b/raw' \
  -games 20
```

Local (Ollama) contestants still report measured tokens, priced at zero — the
report shows them as **free**, distinct from cost-unknown. The condition renders
as a colored badge next to each model on the report.

### Reports and history

```sh
go build -o bin/bench-report ./cmd/bench-report
go build -o bin/bench-history ./cmd/bench-history

bin/bench-report                     # newest run in ./runs -> report.html (self-contained)
bin/bench-report -run runs/<id>.json # a specific run
bin/bench-history                    # timeline across all runs
bin/bench-history -agent haiku       # one contestant's Elo/cost trend
```

Reports read *only* the persisted run JSON, so they can never disagree with the
saved numbers.

---

## 2. Agentic-harness comparison (live, via MCP)

Here the *same model* plays through a full agent runtime instead of the thin
one-shot harness. The agent joins a live battle and drives it with the pokearena
MCP tools against the server-side expectimax opponent (which plays a tuned team
from `data/ai-teams.json`).

### Prereqs

```sh
make run          # bring up the stack (gateway + ai-service + datastores) on :8080
make mcp          # build bin/pokearena-mcp (the MCP adapter the agent drives)
```

- **Claude Code** picks up the MCP server from the `--mcp-config` the scripts
  generate — nothing else to configure.
- **Antigravity** (`agy`) reads MCP servers from
  `~/.gemini/antigravity-cli/mcp_config.json`. Register pokearena once:

  ```json
  {
    "mcpServers": {
      "pokearena": {
        "command": "/ABSOLUTE/PATH/poke-sys-design/bin/pokearena-mcp",
        "args": [],
        "env": { "POKEARENA_GATEWAY_URL": "ws://localhost:8080" }
      }
    }
  }
  ```

### One battle

```sh
scripts/bench/play-live.sh claude sonnet Genesis demo1
scripts/bench/play-live.sh agy "Gemini 3.1 Pro (High)" Genesis demo2
```

Args: `<claude|agy|codex> <model> <team-name> <label> [outdir] [entrant-id]`. The team name is one
from `data/benchmark-teams.json` (Genesis, Spectrum, Keystone, Bruiser, Bastion,
Blitz). It prints the authoritative winner read back from the gateway.

### A whole run, one command (`cmd/bench-run`)

This is the entry point for a real benchmark: every entrant, every team, any
number of games, from one config.

```sh
cp scripts/bench/bench.example.json bench.json   # edit entrants / teams / games
go run ./cmd/bench-run -config bench.json -out runs/$(date +%F) -dry-run
go run ./cmd/bench-run -config bench.json -out runs/$(date +%F)
```

Then the standings, straight from the result files:

```sh
go run ./cmd/bench-report -bench-run runs/$(date +%F)
```

```
entrant                   games     W     L  unfin     win% 95% CI               med s
gemini-cli/3.1-pro           30    19     9      2    67.9% [49%, 82%]             184
claude-code/opus             30    17    11      2    60.7% [42%, 77%]             402
codex/gpt-5                  30    12    16      2    42.9% [26%, 61%]             233
```

Four properties matter more than the convenience, and each exists because of a
way a long run goes wrong:

- **Resumable.** The plan is a pure function of the config and every game writes
  its own result file, named from its coordinates. Re-running the same command
  skips what finished and plays the rest — there is no central ledger to lose,
  and no `-resume` flag to remember. A laptop that slept overnight is a
  non-event.
- **Balanced if interrupted.** Games are interleaved across entrants, so
  stopping halfway leaves every entrant with the *same* number of games instead
  of the first one complete and the last with none. A partial run is still a
  comparison.
- **Failures do not enter the dataset.** A game whose harness errors or times
  out writes no result file, so the next run retries it. A wrong CLI flag costs
  you a retry, not a poisoned batch.
- **Attribution is in Postgres.** The entrant id is sent as the battle's trainer
  name, so "which agent played this battle" is a database fact. The previous
  generation of this tooling kept that mapping in `/tmp` and lost a full batch
  when the directory was cleared.

Scaling up is just editing `games_per_team` and re-running: existing games are
kept, only the new ones are played.

`concurrency` should stay low (2–3). Every live battle also runs a server-side
expectimax opponent, and some agent CLIs stall under load.

### A single-entrant batch (older, still useful for a quick check)

```sh
# <claude|agy> <model> <team> <N> <concurrency> <tag>
scripts/bench/run-batch.sh claude sonnet Genesis 20 3 claude-sonnet
scripts/bench/run-batch.sh agy "Gemini 3.1 Pro (High)" Genesis 20 2 agy-gemini
```

Pools the N outcomes and prints the win rate with a Wilson 95% interval — the
same statistic `cmd/bench` reports, so agentic and thin-harness numbers line up.
Keep concurrency at 2-3: each live battle also runs an expectimax opponent
server-side, so the box saturates quickly.

Model strings: for Claude Code use aliases (`haiku`, `sonnet`, `opus`) or a full
id. For Antigravity use the display names from `agy models` (e.g.
`"Gemini 3.1 Pro (High)"`, `"Claude Sonnet 4.6 (Thinking)"`).

### Matched comparison

For a clean "harness matters" cut, hold the model fixed and vary the harness:

- **Thin harness:** `bin/bench -agents expectimax -llm 'sonnet=claude-sonnet-4-6' -teams <one-team-lib> -games 10`
- **Claude Code:** `scripts/bench/run-batch.sh claude sonnet Genesis 20 3 cc-sonnet`
- **Antigravity:** `scripts/bench/run-batch.sh agy "Claude Sonnet 4.6 (Thinking)" Genesis 20 2 agy-sonnet`

All three face the same tuned expectimax opponent; the only thing that changes is
the harness. (Caveat: the thin arm via `cmd/bench` is a mirror match, while the
live arms are non-mirror against the AI's tuned pool — comparable strength, noted
in [benchmark.md](benchmark.md).)

### The full report (both arms, with watchable model replays)

One command folds the baseline round-robin and the live agentic runs into the
standard report — leaderboard, Elo, head-to-head matrix, per-team, momentum,
rosters — with a watchable replay for every model:

```sh
scripts/bench/build-report.sh   # /tmp/pk-agentic + runs/arm1-baseline.jsonl -> reports/benchmark.html
scripts/bench/build-report.sh <agentic-dir> <baseline-trace> <out-html>
```

A live model's game can't be re-simulated from a seed, so its replay is rebuilt
from the persisted turns. Each result line records its battle id (`bid=<id>`, see
[`run-batch.sh`](../scripts/bench/run-batch.sh)), so the builder finds a won
battle per model and reconstructs it — no hand-picked ids. It is idempotent (a
replay already under `<agentic-dir>/replays` is reused) and logs any model with
no recorded won battle, whose matrix cell stays replayless rather than empty.

The stack must be up when a replay still needs reconstructing (Postgres holds the
turns); a re-run with the replays already present is offline.

---

## Reproducibility notes

- Deterministic runs are byte-identical for the same agents + teams + seeds; the
  run record's header pins dataset, engine revision, ruleset, and config.
- LLM and agentic runs are **not** deterministic (the models aren't seeded); that
  is expected and is what the confidence intervals are for.
- Token cost is always measured from real usage, never estimated.
