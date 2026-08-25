# Deeper search played worse

*We gave our game-tree search more compute and it got worse at the game. The bug
was that it thought it had already won.*

PokéArena runs 6v6 Pokémon battles on a deterministic engine, and one of the
agents in the pool is a plain fixed-depth expectimax. Search agents are supposed
to be boring: they are the thing you measure other things against. Ours was
going to be the ground truth for a per-move regret metric — score every decision
an LLM makes against what the search would have played, and you get a much
denser signal than win/loss.

Before shipping that metric we ran the obvious sanity check: sweep the search
depth and confirm the agent gets stronger. It did the opposite.

```
depth 1  →  61%
depth 2  →  46%
depth 3  →  27%
```

That is expectimax's win rate against a fixed heuristic opponent, over a
full-library depth sweep. Thirty-four points of decline, with non-overlapping
intervals. Every extra ply of lookahead bought a materially worse player. (Those
three figures are from the v1 team library and are unrepeatable — the bug they
measure is gone.)

An oracle that plays worse the harder it thinks cannot define "optimal," so the
regret metric went in the bin. But the sweep was worth chasing down, because a
monotonicity failure that large is never subtle when you finally see it.

## What it is not

Three explanations come to mind first, and it is worth saying why none of them
account for a cliff this size.

**"The evaluation function is bad."** A weak leaf evaluator does degrade with
depth — you propagate a noisier estimate from further away. But that failure mode
is a slope, not a cliff, and it does not usually reverse the sign of "more search
helps." A 34-point collapse is not a noisy heuristic; it is a heuristic that is
confidently, systematically wrong about something.

**"It is the horizon effect."** Deep search can learn to push a bad outcome just
past the last ply it can see. That is real, and it costs you games. It does not
cost you thirty-four points of win rate, and it does not fall away so cleanly
with every added ply.

**"Pokémon is luck, the numbers are noise."** This is the one the benchmark was
designed to answer in advance. Battles run as mirror matches on a fixed seed set,
both seat orientations per seed, with agents rebuilt per game — the same team on
both sides, the same RNG stream, the policy as the only free variable. If the
dice were driving this, the sweep would not be monotone in depth.

The remaining possibility is the uncomfortable one: the search was optimising
correctly, and the thing it was optimising was not the game.

## The phantom KO

Expectimax needs a state to search from. The agent does not get one — it gets a
**fog-of-war view**, which is the whole point of the benchmark: your own side in
full, the opponent's *active* Pokémon, and nothing else. The opponent's bench is
hidden information and is not in the bytes the agent receives.

So the search did what it had to do: it reconstructed a battle state from the
view. Your six, and the foe's one visible active.

That reconstruction is a perfectly good approximation for damage rolls, speed
order, and type matchups. It is a catastrophe for exactly one question:
**is this state terminal?**

Terminality is decided by "does either side have any Pokémon left." In the
reconstructed state, the foe's side held one Pokémon. Knock it out and the foe's
side is empty. The search read that as *winning the entire game* and scored it
`+1e6` — with five foe Pokémon still sitting on a bench it could not see.

Now the depth behaviour explains itself. At depth 1 the search can barely reach
the fiction; it mostly just picks the biggest number. At depth 2 it can see a
line that reaches the phantom win and will pay a real price to get there. At
depth 3 it can construct a *plan* to reach it — sacrificing material, ignoring
setup, refusing sensible switches — because every one of those costs is rounded
to nothing against a payoff of a million. More compute was not buying better
play. It was buying a more thorough pursuit of a KO that ended nothing.

## The fix

Commit `013f82e`. Terminality is no longer judged against the reconstructed
board; it is judged against **true material** carried in the search context. The
foe's hidden bench is counted as full-HP Pokémon, so a KO is a won game only when
that bench is genuinely empty. Otherwise it scores as what it actually is — a
one-Pokémon material lead, far below a win.

That killed the collapse.

## The honest part

Here is the sentence this whole post exists to carry: **the corrected search is a
weaker player than the buggy one was.**

The phantom KO had been inducing helpful aggression. On this format, on this team
library, chasing the KO of the visible active turns out to be a decent policy —
it just was not a decent *reason*. Removing it dropped expectimax from ~61% to
~50% against the heuristic. That comparison is from the v1 library only and can
never be re-measured; the buggy model no longer exists to run.

We kept the correct model anyway. A baseline that measures what it claims to
measure is worth more than a meta-specific accident that happens to score well,
because the accident does not survive a change of format and you cannot tell
which of your other results it is quietly propping up.

## Where the sweep stands now

The sweep has since been re-measured twice — once when the team library changed
(v1 → v2, EV spreads and natures), and once when the engine did (a damage-
rounding fix that moved every damage figure in the format, and therefore every
game played on it).

Expectimax win rate vs the heuristic, 240 games per depth — 6 teams × 20 seeds ×
2 orientations:

| Depth | v1 teams, pre-fix engine | v2 teams, pre-fix engine | v2 teams, current engine | current Wilson 95% CI |
|---|---:|---:|---:|---|
| 1 | 50% | 54.4% | **48.1%** | [41.9%, 54.4%] |
| 2 | 40% | 42.9% | **36.7%** | [30.8%, 42.9%] |
| 3 | 38% | 42.1% | **42.1%** | [36.0%, 48.4%] |

**Only the last two columns are comparable with each other.** The first is on a
different team library *as well as* a different engine; it is kept for continuity,
not as a control. (All three columns are post-`013f82e` — "pre-fix engine" refers
to the damage-rounding fix, not to the phantom KO.)

Reproduce the current column with:

```
go run ./cmd/bench -agents heuristic,expectimax -depth N -games 20
```

What survived both changes: expectimax is weaker than the heuristic at every
depth past 1, and buying more depth does not buy it back. The d1→d2 drop is the
robust part — **11.4 points** on the current engine against **11.5** on the
pre-fix one. Two runs agreeing to a tenth of a point across an engine change that
moved every damage roll in the format is about as much confirmation as a result
in this document gets, and in both the intervals only graze (41.9–42.9% now,
48–49% before).

**The tail is not a result.** On the pre-fix engine d2 and d3 sat on top of each
other (42.9% and 42.1%); on the corrected damage chain they separate, with d2 the
low point and d3 recovering most of the way back. Their intervals still overlap
across 36.0–42.9%, so this is one run's worth of suggestion. Do not read a "d2 is
specifically bad" story into it — we don't. Correspondingly, the d1→d3 slope
shrank from 12.3 points to 6.0, which is why an earlier claim that the *slope*
was stable has been dropped: what is stable is the sign and the first step, not
the magnitude across all three.

One library-specific note, since these numbers should always be quoted with the
`team_library` version attached: at depth 1 expectimax now edges *ahead* of the
heuristic (54.4%) on the trained teams, where on the neutral v1 teams it drew
level. The default `bench` depth is 2, where it still loses at 42.9%, so the
headline baseline ordering is unaffected. (Both of those figures are the v2
teams, pre-fix engine column.)

## The residual

The gentle slope that remains — d1 > d2 ≈ d3 — has a known cause, and it is the
second defect in the same opponent model. **The simulated foe never switches.**
We do not know its hidden species and will not fabricate them, so the search
plans against an opponent more pinned down than the real one. On a 6v6 format
where switching is central, that is a real distortion. It is no longer
catastrophic, and fixing it properly means modelling unknown switch-ins — a
larger change we have not made.

So expectimax stays in the pool as a legitimate strong baseline opponent, and it
is still not scored against as an optimality reference. The benchmark reports
outcome-based metrics only — win rate, Elo, and confidence intervals — which need
no oracle at all. Reviving per-move regret would first require a search that can
model a foe that switches.

## The generalisable bit

The bug was not in the search. Expectimax was correct; the evaluation was
correct; the depth loop was correct. The bug was at the seam where an
imperfect-information view was cast into a perfect-information state, and
specifically in the one predicate where "what I can see" and "what is true" are
not interchangeable: **terminality**.

If you are searching over a reconstructed state, the reconstruction's job is to
be *wrong in bounded ways*. Getting a damage roll slightly wrong costs you a
fraction of a point. Getting "the game is over" wrong costs you `1e6`, and a
deeper search is simply a more effective machine for finding whatever your
evaluation over-rewards. Audit the terminal predicate against ground truth before
you audit anything else — and treat non-monotonicity in depth as the smoke alarm
it is, because a search that gets worse with compute is telling you it is
optimising something other than the game.

---

Full methodology, scope, and the rest of the limitations:
[docs/benchmark.md](benchmark.md) — §6 is the source for every figure above. How
to run it: [docs/running-the-benchmark.md](running-the-benchmark.md). The
damage-rounding fix referenced above is OPEN-3 in
[docs/engine-findings.md](engine-findings.md). The project itself:
[PokéArena](../README.md).
