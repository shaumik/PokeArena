# The PokéArena battle benchmark — scope, claims, and limitations

This document states exactly what the battle benchmark measures, what it does
**not**, and where the honest limits are. It is deliberately conservative: a
benchmark is only worth citing if its boundaries are drawn before its numbers
are.

If you only read one section, read [What we do not claim](#what-we-do-not-claim).

---

## 1. What this measures

The battle track measures **tactical decision-making under hidden information**:
given a fixed team and a fog-of-war view of the battle, how well does a policy
(an LLM, a search agent, a heuristic) choose its moves and switches over a full
6v6 game?

It does **not** measure team-building, metagame knowledge, or anything a model
was told in advance. Those are a separate axis — see [the Build track](#appendix-what-this-is-not--the-build-track).

---

## 2. Positioning — we are not first, and the domain is not the point

LLMs playing Pokémon is crowded prior art, and we make no novelty claim over it:

- **PokéLLMon** (2024) — human-parity agent on the live Showdown ladder.
- **PokéChamp** (ICML 2025) — 6v6 Gen 9 OU, minimax+LLM, ~1300–1500 Showdown
  Elo, released a 3M-game dataset.
- Additional work: "Reasoning Under Pressure," "LLMs as Pokémon Battle Agents,"
  and several open-source harnesses.

Our differentiator is **not** the domain. It is that we run on our **own
deterministic engine** instead of wrapping Showdown, which buys one thing that
is structurally hard otherwise:

> **Variance-controlled mirror matches.** Same seed, same team, both sides — the
> RNG stream is byte-identical, so the *only* free variable is the policy. This
> directly answers the "Pokémon is just luck" objection: across enough seeds, a
> win rate above 50% in a mirror is evidence about the player, not the dice.

The engine also makes every result **reproducible from a clone** (Section 8),
which most Showdown-wrapping harnesses cannot offer.

---

## 3. The ruleset — a custom format, stated plainly

This is **not** a downloadable competitive format. It is a specific, fixed
ruleset that teams are built *for*:

| Parameter | Value |
|---|---|
| Species pool | Gen-1 dex — **80 species** |
| Movepools | **Full modern movepools** — 538 moves; a mon can run any move it legally learns in current data |
| Items | **None** in battle |
| Level | **50**, fixed |
| IVs / EVs | **31 / 0** across the board |
| Nature | **Neutral** |
| Clauses | Species Clause; mirror match for the benchmark |

The consequence matters and is stated honestly: with no items, no EVs, and a
neutral nature, **base stats dominate**. The meta skews toward offense and
speed, and recovery moves are unusually valuable. A team that is strong in
standard competitive play is not automatically strong here — the format is its
own thing, and the [team library](#7-the-team-library-what-it-is-and-is-not) was
authored specifically for it.

The exact ruleset string is emitted in every run header (`eval.Ruleset()`), so a
result can never be silently reinterpreted under a different format.

---

## 4. What isolates the signal

Four controls keep the measurement pointed at the policy and nothing else:

1. **Mirror matches.** Both sides get the identical team, so team strength
   cancels and only play differs.
2. **Both seat orientations per seed.** Each contestant plays side 0 and side 1
   of the same seed, cancelling any first-mover / seat advantage from the win
   rate.
3. **Fixed seed set.** Seeds are `0..n-1` (`SeedRange`), named not randomized, so
   a published run is reproducible from the command line alone.
4. **Fresh agents per game.** Agents are rebuilt per game from a factory seeded
   by the game seed, so game *N* never depends on state carried from *N-1*.

---

## 5. Metrics, and what each does and does not support

| Metric | What it supports | What it does **not** support |
|---|---|---|
| **Win rate + Wilson 95% CI** | "Does A beat baseline B, and is the gap real given the sample?" | A win rate off a few hundred games is not a precise skill number; read the interval, not the point. |
| **Elo (Bradley-Terry, regularized)** | *Relative* ranking within one round-robin. Order-independent, so it's reproducible. | Cross-run absolute comparison. The scale is anchored, not calibrated to Showdown or to any external ladder. A 1600 here is not a 1600 anywhere else. |

Elo is fit by Bradley-Terry MM iteration (not sequential K-factor), so the
ratings depend only on win/loss counts, not the order games were played — that
is what makes them reproducible. A small number of virtual games against a
neutral anchor regularizes the fit so an all-win or all-lose agent stays at a
finite rating instead of diverging.

---

## 6. The metric we walked back: per-move regret vs expectimax

The original design (issue #101) named **per-move regret against an
expectimax-optimal oracle** the flagship metric: score every decision against
the search-optimal move, not just "did it win."

**We shelved it, and this is the most important limitation in this document.**

During bring-up, fixed-depth expectimax turned out **not to be a valid ground
truth on this format**:

- It is **non-monotonic in depth** — searching *deeper* sometimes played
  *worse* (e.g. a team that won 29 games at depth 1 won 19 at depth 3).
- Root cause: the opponent model in the search is blind — it never switches
  Pokémon in simulation. On a 6v6 format where switching is central, an agent
  optimizing against a foe that never switches is optimizing against the wrong
  game.

An oracle that plays worse with more compute cannot define "optimal," so scoring
regret against it would have manufactured authoritative-looking numbers on a
broken reference. We removed the claim rather than ship it.

**What we do instead:** outcome-based metrics only (Section 5) — win rate, Elo,
and confidence intervals, which need no oracle. Expectimax remains in the pool
as a *strong baseline opponent*, which it legitimately is; it is simply not
treated as ground truth. Reviving per-move regret would first require an
opponent model in the search that can switch.

---

## 7. The team library: what it is and is not

The benchmark ships a curated library of six competitive teams
(`data/benchmark-teams.json`), each authored for this exact ruleset with
verified-legal movepools, spanning styles (legendary offense, special, balanced,
physical, bulky, hyper-offense). Every team is legality-checked at load.

The library deliberately spans styles, so it is **not** internally balanced —
and that is correct:

- The battle benchmark is **mirror-matched**: a team only ever plays *itself*,
  so its strength *against other teams* is irrelevant and cancels out. A diverse
  library tests policy across varied situations, which is the goal.
- `cmd/team-validate` *does* measure cross-team balance (it cross-matches every
  pair under one neutral pilot), and it correctly reports the library as
  imbalanced — some teams beat others lopsidedly. That signal is **advisory for
  the battle track** and **central to the Build track** (Appendix), which is
  why the tool reports and exits 0 rather than gating.

What keeps the battle benchmark honest is a different check: on *every* team, a
better policy beats a worse one (random loses badly on all six). That — not
cross-team parity — is the property a mirror benchmark needs.

---

## 8. Reproducibility and CI-independence

The benchmark's credibility rests on a third party re-deriving the numbers, not
on trusting a pipeline.

- **No CI dependency.** Nothing about a published number requires our
  continuous-integration system. Clone the repo, run `bench` with the same
  agents / teams / seeds, and deterministic contestants produce byte-identical
  games — same winners, same turn counts, same per-decision state hashes.
- **Provenance is pinned per run.** Every run writes a header naming the engine
  revision, dataset sim-version + curation SHA + source gen, level, ruleset,
  team library, contestants, depth, seeds, and config. A trace can never be
  silently reattributed to a different engine or dataset.
- **Runs are persisted, not just printed.** Each run is saved as
  `runs/<run_id>.json` plus an appended line in `runs/index.jsonl`, where
  `run_id` = UTC timestamp + a short hash of the defining config. This is what
  makes a leaderboard-over-time legible and lets two runs be diffed after the
  fact.

The one intentionally non-reproducible element is the LLM contestants themselves
(Section 10) — that non-determinism is handled by the confidence intervals, not
pretended away.

---

## 9. Cost is measured, not estimated

Token usage is captured as structured data from every model call (input, output,
cache-read, cache-write kept separate because they bill at different rates),
carried intact to the results store, and multiplied by a published price table
(`data/model-pricing.json`) only at the end. So **`$/game` is a measured
figure**, and a model whose price is missing is reported as *cost-unknown*, never
as free. Usage is counted even when a decision falls back on a malformed reply —
a flaky model does not get to hide its cost.

---

## 10. LLM non-determinism

LLM contestants are not seeded and not deterministic: the same state can yield
different moves across runs. We do not pretend otherwise. It is handled by:

- Reporting win rates with **Wilson 95% intervals**, so sampling noise is
  visible rather than hidden in a point estimate.
- Recording **legality-fallback rate** in the trace: when a model returns an
  illegal or unparseable action, the driver substitutes the first legal move and
  flags it. That rate is itself a signal about a model's reliability.

---

## What we do not claim

- **Not** the first or a novel LLM Pokémon benchmark (Section 2).
- **Not** a standard/known competitive format — it is a custom ruleset
  (Section 3).
- **Not** a per-move optimality score — we removed the expectimax-oracle claim
  (Section 6).
- **Not** an absolute or externally-calibrated skill rating — Elo here is
  relative within a round-robin (Section 5).
- **Not** a measure of team-building or metagame reasoning — that is the Build
  track, not yet the subject of published numbers (below).

---

## Appendix: what this is not — the Build track

Issue #101 defines a second, orthogonal track: **team-building** ("fix the
battler, vary the team"). A neutral fixed pilot plays every model-built team, so
team win rate under a constant battler measures build quality. `internal/eval`
already contains the harness seed for it (`TeamTournament`), and cross-team
balance — advisory noise for the battle track — becomes the *scoring signal*
there. It is out of scope for the battle-track numbers this document governs.
```
