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
ruleset that teams are built *for*.

Two different things get confused here, so they are stated separately: what
the format **permits**, and what the shipped teams **use**.

**What the format permits** (`eval.Ruleset()` — enforced by `engine.ValidateTeam`):

| Parameter | Value |
|---|---|
| Species pool | Gen-1 dex — **80 species** |
| Movepools | **Full modern movepools** — 560 moves; a mon can run any move it legally learns in current data |
| Items | **Any** item in the curated catalog, one per Pokémon |
| Level | **50**, fixed |
| IVs | **0–31** per stat |
| EVs | **252** per stat, **510** total |
| Nature | Any of the **25** |
| Clauses | Species, Item, Evasion, OHKO and Sleep; mirror match for the benchmark |

**What the shipped library uses** (`eval.TeamProfile()` — *counted from the
picks*, not asserted): all 36 picks EV-trained and natured, no custom IVs, no
held items.

That split is deliberate. The old ruleset string described both at once
("IV31/EV0, neutral nature, no items") and went stale twice — once when items
shipped, once when spreads did — because nothing forced it to change. The
profile line is derived from the teams, so it cannot drift; the permissions
line is derived from the engine's own constants, so neither can that.

Both are emitted in every run header, which is what stops a result from being
silently reinterpreted: two runs under an identical `ruleset` can still be
measuring different metagames, and `team_profile` is the line that says so.

**The consequence, stated honestly.** With items unused, EV spreads and Speed
tiers are what separate teams. Investment is concentrated — 252/252/4 — so
offense is fast and walls are genuinely hard to break. Recovery moves remain
unusually valuable. A team that is strong in standard competitive play is not
automatically strong here; the format is its own thing, and the
[team library](#7-the-team-library-what-it-is-and-is-not) was authored
specifically for it.

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
truth on this format** — it was **non-monotonic in depth**, i.e. searching
*deeper* played *worse*. We traced this to two distinct defects in the search's
opponent model:

1. **The phantom KO.** The search reconstructs the battle from fog-of-war, so
   the foe's side held only its visible active Pokémon — the hidden bench was
   invisible. Knocking that active out therefore read as *winning the whole
   game* (`+1e6`), even with five foe Pokémon still waiting. Deeper search
   chased this fiction harder, so more compute bought worse play: a full-library
   depth sweep collapsed from **61% (d1) → 46% (d2) → 27% (d3)**, a 34-point
   cliff with non-overlapping intervals. (Measured on the v1 neutral library,
   and unrepeatable — the bug is gone.)

2. **The blind opponent.** The simulated foe never switches, because we don't
   know its hidden species and can't fabricate them. On a 6v6 format where
   switching is central, the search still plans against a foe more pinned-down
   than the real one.

**We fixed defect (1)** (`013f82e`): terminality is now judged by *true*
material — the foe's hidden bench, carried in the search context, counts as
full-HP Pokémon — so a KO is a won game only when the bench is genuinely empty;
otherwise it scores as a one-Pokémon material lead, far below a win. This
**killed the collapse**.

These are the doc's only load-bearing measurements, so the sweep has been
re-measured three times: once when the teams underneath it changed (Section 7,
v1 → v2), and twice when the engine did — the damage-rounding fix in
`docs/engine-findings.md` (OPEN-3), and then the damage-modifier grouping fix in
`docs/royale-followups.md` (item 5). Each engine change moved every damage
figure in the format and therefore every game played on it.

Expectimax win rate vs the heuristic, 240 games per depth — 6 teams × 20 seeds
× 2 orientations. Reproduce the current column with:

```
go run ./cmd/bench -agents heuristic,expectimax -depth N -games 20
```

| Depth | v1 teams, pre-rounding | v2 teams, pre-rounding | v2 teams, pre-grouping | v2 teams, current engine | current Wilson 95% CI |
|---|---:|---:|---:|---:|---|
| 1 | 50% | 54.4% | 48.1% | **49.2%** | [42.9%, 55.5%] |
| 2 | 40% | 42.9% | 36.7% | **37.5%** | [31.6%, 43.8%] |
| 3 | 38% | 42.1% | 42.1% | **41.2%** | [35.2%, 47.6%] |

Only the last three columns are comparable with each other; the first is on a
different team library as well as a different engine, and is kept for
continuity rather than as a control. Every current figure sits inside the
previous engine's interval, which is the expected result — the grouping fix
changes damage numbers by a point here and there, not the shape of the game.

**The finding this section rests on survived all three changes.** Expectimax is
weaker than the heuristic at every depth past 1, and buying more depth does not
buy it back. The d1→d2 drop is the robust part, and it is now the same number
three times over: **11.7 points** on the current engine, **11.4** before the
grouping fix, **11.5** before the rounding one. Three runs spanning two engine
changes, each of which moved every damage roll in the format, agree inside half
a point — and in all three the d1 and d2 intervals only graze (42.9–43.8% now,
41.9–42.9% before). Deeper search costing real win rate is as close to
established as anything in this document.

**What did move is the tail, and it has now moved twice.** On the earliest v2
engine d2 and d3 sat on top of each other (42.9% and 42.1%); on the corrected
damage chain they separated, with d2 the low point and d3 recovering most of the
way back, and the current engine reproduces that shape (37.5% and 41.2%). Their
intervals still overlap heavily — 35.2–43.8% — so this remains a suggestion
rather than a result, and no "d2 is specifically bad" story should be read into
it. The d1→d3 slope has now measured 12.3, 6.0 and 8.0 points across the three
runs, which is why the earlier claim that the *slope* was stable was dropped and
should stay dropped: what is stable is the sign and the first step, not the
magnitude across all three.

Two honest consequences remain:

- The **residual gentle slope** (d1 > d2 ≈ d3) is defect (2), the un-modeled
  foe switching. It is no longer catastrophic, and fixing it fully means
  modeling unknown switch-ins — a larger change we have not made.
- The correct model is **weaker than the buggy one was**: the phantom KO had
  been inducing helpful aggression, and removing it dropped expectimax from
  ~61% to ~50% against the heuristic. (That comparison is v1-only and cannot
  be re-measured — the buggy model no longer exists to run.) We keep the
  correct model anyway — an honest baseline that measures what it claims to is
  worth more than a meta-specific accident.

One thing the trained library did change: at depth 1, expectimax now edges
*ahead* of the heuristic (54.4%) where on neutral teams it drew level. The
default `bench` depth is 2, where it still loses at 42.9%, so the headline
baseline ordering is unaffected — but the per-depth numbers are library-
specific and should be quoted with the `team_library` version attached.

An oracle that plays worse with more compute cannot define "optimal." The fix
removes the *dominant* cause of that, but a residual remains, so we still do
**not** treat expectimax as ground truth for per-move regret.

**What we do instead:** outcome-based metrics only (Section 5) — win rate, Elo,
and confidence intervals, which need no oracle. Expectimax remains in the pool
as a *legitimate strong baseline opponent*; it is simply not scored against as
an optimality reference. Reviving per-move regret would first require an
opponent model in the search that can switch (defect 2).

---

## 7. The team library: what it is and is not

The benchmark ships a curated library of six competitive teams
(`data/benchmark-teams.json`), each authored for this exact ruleset with
verified-legal movepools, spanning styles (legendary offense, special, balanced,
physical, bulky, hyper-offense). Every team is legality-checked at load.

### Library v2 — the training spreads

**v2 results are not comparable with v1 results.** The format did not change;
the teams did. v1 ran everything at EV 0 / IV 31 / neutral because that was
all the engine supported. v2 gives every pick a nature and an EV spread, and
the library version in each run header (`team_library`) is what tells the two
apart.

The size of the break, measured by replaying the v2 teams through the same
heuristic mirror twice — once as shipped, once with the spread fields stripped
back to the defaults — across 60 seeds per team. Stripping rather than
replaying literal v1 is deliberate: it holds the movesets constant so the
column below isolates the spread, and Bruiser's Tauros changed a move this
round (see the curation rules).

Reproduce with `go run ./cmd/spread-impact`. That harness is the measurement —
this table is its output, not a transcription of one. It was added when the
damage-rounding fix (`docs/engine-findings.md`, OPEN-3) moved every number here
and there was no committed way to re-derive them, which Section 8 says there
must be. Re-derived a second time for the damage-modifier grouping fix
(`docs/royale-followups.md`, item 5), which moved every figure again for the
same reason: it changes damage numbers across the whole format.

| Team | spreads stripped | as shipped | games with a different outcome or length |
|---|---:|---:|---:|
| Genesis | 33.5 | 26.4 | 54 / 60 |
| Spectrum | 35.3 | 33.4 | 54 / 60 |
| Keystone | 35.4 | 34.5 | 57 / 60 |
| Bruiser | 17.6 | 16.8 | 49 / 60 |
| Bastion | 57.8 | 77.0 | 59 / 60 |
| Blitz | 24.0 | 22.0 | 59 / 60 |

Offense got faster and the wall team got markedly harder to break — 252 HP
plus 252 in the relevant defence is a real investment, and Bastion's games run
~33% longer for it. Worst case across 720 games is 94 turns against a
20,000-decision safety cap, so the longer games cost wall-clock, not
termination.

Curation rules the spreads follow, each enforced by a test rather than by
care:

- **252 / 252 / 4**, always. At L50 with 31 IVs all three investments are
  worth at least one point (the `2·Base + IV` term is odd, so the extra
  `floor(4/4)` tips the division), and 508 fits the 510 budget.
- **Speed is bought only where it can be won.** Mons under base 65 Speed take
  bulk instead — Snorlax at base 30 will not outrun anything, so the EVs go
  where they change an outcome.
- **No nature lowers a stat its holder attacks with.** A Timid physical
  attacker is a 10% penalty on the mon's entire job, and it is perfectly legal
  — nothing in the engine objects. `TestTeamLibrary_NaturesDoNotHurt` does.
  It found one on the first run (a Jolly Tauros still carrying Fire Blast off
  base 40 Sp.Atk); the move was cut for Megahorn rather than the guard
  weakened. Fixed-damage moves are exempt, which is why Chansey can run Bold
  without paying for it — Seismic Toss does not read Attack.

`data/ai-teams.json` carries the same teams and the same spreads, so the
opponent a player faces in `mode=live` is the same strength as the one the
benchmark measures.

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
  team library, team profile, contestants, depth, seeds, and config. A trace can never be
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
