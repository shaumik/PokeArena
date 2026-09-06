# Handoff — Decision-Quality Eval (per-model reasoning scoring)

> **Historical. 2026-08-11.** The work this hands off is done and landed; the
> remaining items below (finish the batch, build the aggregation, wire the
> report, open the PR) are all closed. Read it as a design-and-gotcha record,
> not a task list.
>
> Two of its environment facts are now actively misleading. The `/tmp/pk-agentic-v2`
> attribution files and the local Postgres it names **no longer exist**, which is
> why the per-model table in [decision-quality.md](decision-quality.md) is
> labelled v1-era and unrepeatable. And the metric no longer *needs* any of that
> infrastructure to run: `cmd/decision-sim` produces the same export shape from
> deterministic offline games, which is how the v2 calibration was measured.
>
> The "no Claude co-author trailers" convention recorded at the bottom is also
> stale — the repo's history carries them.

Status as of 2026-07-17. Branch: `feat/decision-quality-eval` (based on the
now-merged `feat/replay-samples`; PR #108 already merged to `main`).

> **UPDATE 2026-07-17: the aggregation shipped.** The "What REMAINS" item 2
> (the per-model table) is done — `eval.AggregateByModel`,
> `decision-eval -manifest`, and `scripts/bench/decision-report.sh`. On the
> fresh batch, blunder rate tracks win rate (Opus < Gemini < Sonnet < Haiku).
> Opus stayed thin (n=2): its live games wall out under load, so its row is
> indicative only. Report-section wiring (item 3) is still open as a follow-up.
> The rest of this doc is the original design/gotcha reference — still accurate.

## Goal

Score how well each model *chose*, not just whether it won. For every free
decision in a recorded live battle, compare the action the model played to what
a stronger expectimax "oracle" would have played **from the identical
fog-of-war view**, and measure the **regret** (value the choice gave up). Roll
up into a per-model table: blunder rate, median regret, match rate — the
reasoning-quality view that win/loss hides. Zero new API spend was the original
constraint; we ended up re-running a fresh attributed batch (see below) because
the old attribution was wiped.

## What is DONE (committed on this branch)

Two commits, both green (build + lint + tests). **They may be unpushed — run
`git push -u origin feat/decision-quality-eval` first thing.**

- `4bccd5e eval: decision-quality scoring — recover actions, measure regret vs oracle`
- `3dde37b eval: recover faint turns too — decision coverage to ~full`

Files:
- **`internal/eval/decisionquality.go`** — the core.
  - `recoverActions(dex, prevRaw, want)` — re-simulates a turn (enumerate
    `engine.LegalActionsDex` for both sides × `engine.ResolveTurn`) and returns
    the action pair that reproduces the stored next state **byte-for-byte** (both
    marshaled through `engine.BattleState`, so jsonb key-order washes out). The
    engine is deterministic from the stored `RNGState`, so the match is exact and
    doubles as a data-validity check.
  - `settle(dex, st, wantJSON)` — drives a resolved state through forced
    replacements (searching the replacement picks, cascades recurse) so **faint
    turns are recovered too**. This took coverage from ~40–65% to ~100%.
  - `ScoreDecisions(dex, oracle, modelSide, turns) ([]DecisionScore, skipped, err)`
    — walks stored turns, recovers each free choice on `modelSide`, and scores it.
  - `DecisionScore{Turn, Side, Chosen, Best, Agree, Regret, Blunder}`.
  - `Oracle` interface (`ScoreActions(v ai.View) []ai.ActionValue`).
  - `BlunderThreshold = 300.0` (regret in oracle eval points; ~one Pokémon =
    1000, so 300 ≈ giving up ~0.3 of a Pokémon). Tunable.
- **`internal/ai/expectimax.go`** — added `ActionValue` + `ScoreActions(v View)
  []ActionValue` (additive; exposes the per-action maximin values `searchRoot`
  already computes; `Decide` unchanged; ties break toward the first legal action
  exactly like `Decide`, so the top-valued action equals `Decide`'s pick).
- **`cmd/decision-eval/main.go`** — spike CLI. Reads a battle JSON export
  (`{seed, winner, turns:[{state, log}]}`, the same shape `db-replay` consumes)
  on stdin/`-in`, prints per-decision + a summary line. Flags: `-side` (0 = the
  model seat), `-depth` (oracle depth, default 3), `-quiet`.
- **`internal/eval/decisionquality_test.go`** — deterministic tests:
  `TestScoreDecisions_RecoversActionsAndRegret` (Snorlax mirror, stub oracle) and
  `TestRecoverActions_FaintTurns` (heuristic mirror that KOs → exercises the
  replacement recovery).

### Validated

- Recovery re-simulates exactly on real battles.
- Regret >> binary agreement: equal-value alternatives score regret 0 even when
  they "disagree" with the oracle (that's why we don't use match rate as the
  headline). Missed-lethal shows up as regret ≈ 1e6 (winValue) — real but
  off-scale.
- End-to-end on a freshly-played battle on the redeployed engine: play → store →
  `bid=` attribution → export → `decision-eval` scores it. Example: a Haiku game
  that WON but missed a lethal (turn 15 regret ≈ 998k).

## What is IN FLIGHT

**A fresh attributed batch is running in the background** (started 2026-07-16
20:57, my bg task id `b1q49nvj7`, driver `/tmp/run-attributed-batch.sh`, log
`/tmp/attributed-batch.log`).

- 3 teams (Genesis, Keystone, Spectrum) × 4 games = 12 games/model, order
  Haiku → Sonnet → Gemini → Opus, conc 3.
- **Done: Haiku, Sonnet, Gemini (36 games, all attributed).** Opus was running
  last and **some Opus games abandon/time out** (known Opus slowness) — it may
  finish short. Consider topping Opus up to a clean 12 with
  `scripts/bench/run-batch.sh claude opus <team> <N> 2 cc-opus-<team>` (lower
  conc; note `run-batch.sh` truncates `results.txt`, so use a per-game append or
  a fresh tag if you don't want to lose the games already there).
- Attribution lives in **`/tmp/pk-agentic-v2/<key>-<team>/results.txt`**, one
  line per game: `g<N> winner=<0|1|-1> -> ... bid=<uuid>`. Keys: `cc-haiku`,
  `cc-sonnet`, `cc-opus`, `agy-gemini` (match `eval.ModelDisplay`).
  `/tmp` is not durable here (see gotchas) — if you need these to survive,
  copy them somewhere safe or re-derive.

Preliminary win rates (context only, tiny n): Haiku ~8% (1/12), Sonnet ~25%
(3/12), Gemini ~67% (8/12), Opus TBD.

## What REMAINS (the actual task)

1. **Finish / top up the batch** (esp. Opus). Every game must have a
   `status=completed` battle in Postgres with turns; unfinished (`winner=-1`,
   open/abandoned) games can't be scored — either rerun them or exclude.
2. **Build the aggregation** (the main missing piece). For each model:
   - Read `bid=` lines from `/tmp/pk-agentic-v2/<key>-*/results.txt` → set of
     `battle_id`s (skip `winner=-1`).
   - For each battle, export from Postgres and run `ScoreDecisions(side 0)`.
   - Aggregate: **blunder rate** (headline), **median regret** (NOT mean —
     winsorize/clip the ~1e6 missed-lethals or they dominate), **match rate**,
     decisions scored, and the win rate for context.
   - Emit a per-model table. Suggested home: extend `cmd/decision-eval` with an
     aggregate mode (accept a dir of exports or a manifest + model label, output
     JSON stats), or a thin `scripts/bench/decision-report.sh` that loops the
     bids and pipes to a `-json` mode. **Add a test** (repo rule: every new
     ability ships a test in the same commit).
3. **(Optional) Wire the table into the HTML report** (`internal/eval/report.go`
   — new section), then republish to both sites (gh-pages of `shaumik/PokeArena`
   and `main` of `shaumik/shaumik`) via the git-plumbing publish flow. The
   sprite/sample work already lives in `main`.
4. **Open the PR** for `feat/decision-quality-eval` → `main`.

## Data & attribution facts

- Postgres container `pk-bench-postgres-1`, DSN
  `postgres://pokearena:pokearena@postgres:5432/pokearena`. Access:
  `docker exec pk-bench-postgres-1 psql -U pokearena -d pokearena ...`.
- `battle_turns(battle_id, turn_no, log jsonb, state_digest jsonb)`. `state_digest`
  IS a marshaled `engine.BattleState` — `json.Unmarshal` straight back (see
  `eval.ReplayFromStored`). Stored states are post-turn; phases seen are only
  `choosing` and `ended` (replacements are folded into the turn), so the
  pre-decision state for turn N is the stored state at turn N-1.
- **Postgres has NO model identity** — `p1_name` is always "Agent", `p2_name`
  "AI". Model attribution ONLY comes from the `bid=`→model mapping in the run
  dirs. The previous mapping (`/tmp/pk-agentic`) was wiped, which is why we
  re-ran.
- Export shape for one battle (feeds `decision-eval -in`):
  ```sql
  select json_build_object('seed', b.seed, 'winner', b.winner,
    'turns', (select json_agg(json_build_object('state', t.state_digest, 'log', t.log)
              order by t.turn_no) from battle_turns t where t.battle_id=b.id))
  from battles b where b.id='<uuid>'
  ```

## Environment gotchas (important)

- **Worktrees get WIPED by concurrent-agent churn.** My original
  `/private/tmp/pk-bench` got gutted (lost `.git` + source), and `/tmp/pk-agentic`
  was emptied. **Push commits promptly; don't trust `/tmp` or a worktree to
  persist.**
- **Do NOT edit the origin repo root** `/Users/shaumikmondal/programming/poke-sys-design`
  — a concurrent agent works there (branch `worktree-terminal-ux`, the TUI /
  PR #96 sprite work). Read-only is fine.
- Current worktrees: **`/private/tmp/pk-deval`** = this branch
  (`feat/decision-quality-eval`); **`/private/tmp/pk-deploy`** = detached
  `origin/main`, used to build/redeploy.
- **Stack was rebuilt & redeployed on `origin/main`** (image `pokearena:local`
  = `b7fd69bef36f`, from commit `36e11e2`). Postgres volume `pk-bench_pgdata`
  preserved (361 completed historical battles + the new batch). Recreate with
  `docker compose -f /private/tmp/pk-deploy/docker-compose.yml -p pk-bench up -d`.
  **Never `down -v`** (nukes the data).
- **Pre-commit hook** (`.githooks/pre-commit`, build + lint mirroring CI) is in
  `main`. Enable in a new worktree with `make hooks`. Bypass one commit with
  `PRECOMMIT_SKIP=1`.
- Repo conventions (from the owner): **no Claude co-author trailers** in commits
  or PRs; **commit often** (small, green units); **always ship a test** with each
  new mechanic in the same commit; verify with the existing CLI before claiming
  results.

## Live-battle harness (for re-running / topping up)

- Gateway `http://localhost:8080` (ws `ws://localhost:8080`). MCP client
  `bin/pokearena-mcp` (build: `make mcp`). Both `claude` and `agy` (Gemini /
  Antigravity) CLIs are installed and authenticated; `agy` MCP config at
  `~/.gemini/antigravity-cli/mcp_config.json`.
- One game: `scripts/bench/play-live.sh <claude|agy> <model> <team> <label> <outdir>`
  (model = `haiku|sonnet|opus` or `"Gemini 3.1 Pro (High)"`). Prints the
  authoritative winner with `bid=`.
- A batch: `scripts/bench/run-batch.sh <harness> <model> <team> <N> <conc> <tag>`
  → writes `$POKEARENA_OUT/<tag>/results.txt` (**truncates** it first). Keep
  conc ≤ 3 (each live game also runs a server-side expectimax). Opus is slow and
  abandons under load — use conc 2.

## Design notes / open choices

- Oracle depth: spike used **depth 3** (same as the strongest baseline
  contestant). Offline you can afford **depth 4** for a stronger, fairer
  reference — worth trying; it will lower everyone's blunder rate but rank them
  more credibly.
- `BlunderThreshold = 300` is a first guess; calibrate against the regret
  distribution once you have all models.
- Regret aggregation MUST be robust to winValue-scale missed-lethals (median or
  winsorized mean, not raw mean).
- Fog fairness is handled by construction: `ai.MakeView(state, side)` is the same
  projection every agent (LLMs and expectimax) decides from, and expectimax
  reconstructs from the View alone — the oracle never sees hidden info.
