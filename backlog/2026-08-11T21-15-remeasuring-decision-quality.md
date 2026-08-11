# Re-measuring decision quality, and what wouldn't re-measure

PR #109 had been open since 17 July and was 46 commits behind. The plan was
"rebase it and re-measure on library v2." The rebase was the easy half; the
re-measure turned out to be two different questions with two different answers,
and separating them is most of what this round produced.

## The rebase was free, which is worth noting

Eight commits, 1367 lines, 46 commits of drift — and exactly one file
overlapped with anything `main` had touched (`internal/ai/expectimax.go`, where
main changed the replace-phase handling and the PR appended `ScoreActions`).
Cherry-picked clean, built, and the whole suite went green with no edits.

I had assumed a branch that stale would need real work, and it didn't, because
the PR is almost entirely *additive* — a new package file, a new command, a new
report section. That is a property of how it was scoped, not luck. Worth
remembering the next time a branch's age is used as an argument for abandoning
it: age is a proxy for conflict risk, and a purely additive branch barely
accumulates any.

## The published table can't be re-measured, and saying so is the deliverable

`docs/decision-quality.md` carried a four-model table — Gemini / Opus / Sonnet /
Haiku, blunder rate and median regret — measured in July on v1 neutral teams.
Library v2 landed 4 August. So those numbers describe a format that no longer
ships, which is precisely the failure the
[v2 re-sweep entry](2026-08-04T17-51-resweeping-expectimax-on-v2.md) was written
up to prevent, recurring in a different document three weeks later.

The depth sweep could be re-measured because it runs offline. This one can't:
the battles lived in a Postgres on the author's machine and their model
attribution in `/tmp/pk-agentic-v2`, and the handoff doc records that `/tmp`
being wiped had *already* forced one re-run. Both are gone now. Reproducing the
table means paying for a fresh batch across four vendors — a decision about
money, not an afternoon of compute.

So the table is labelled v1-era and unrepeatable, with the reason, next to the
v2 numbers rather than instead of them. Same treatment the pre-fix depth numbers
got in August. The thing I want to avoid is a reader assuming one run produced
everything on the page.

## What *could* be measured, and the bias it exposed

The metric itself had never been shown to rank correctly. It was validated on
"does recovery reproduce the stored state" — a data-integrity property — and
then pointed straight at four models, where nobody knows the true ordering. If
the metric were subtly inverted, nothing in the pipeline would have said so.

`cmd/decision-sim` closes that: it plays deterministic policies offline and
writes the same export shape the live path persists, so the whole pipeline runs
from a checkout with no gateway, no database, and no API spend. 72 games on v2:

| policy | win rate | blunder rate | median regret |
|---|---:|---:|---:|
| expectimax d2 | 44% | 3% | 0 |
| expectimax d1 | 61% | 11% | 12 |
| heuristic | 56% | 21% | 111 |
| random | 0% | 39% | 192 |

Blunder rate is monotone in policy strength. Good — that's the soundness check,
and it's now a test rather than a hope.

But look at the first two rows. **Expectimax d2 blunders least and wins least.**
It is not the best player in the table; it is the player most *similar to the
oracle*, which is expectimax d3. Its 68% match rate against the heuristic's 27%
is the same fact stated louder.

That is a real limitation and it was hiding in plain sight. The doc's central
fairness argument is that every policy is scored from `ai.MakeView` — the
identical fog-of-war projection — so the oracle is "a better player looking at
the same information, not an omniscient one." That argument is correct and it is
about *information*. It says nothing about *algorithm*, and algorithm turns out
to matter: agreement with an expectimax oracle partly measures being an
expectimax.

The uncomfortable part is what that does to the doc's headline finding. "Gemini
blunders least but wins less than Opus" is structurally identical to "d2 blunders
least but wins least," and in the case where I know the cause, the cause is
kinship with the yardstick. That doesn't explain the model result away — no LLM
is running expectimax — but it means the finding cannot be read as
"Gemini reasons more cleanly" without an argument that its style isn't simply
closer to the oracle's. Separating those needs a second oracle of a different
family, and raising the depth won't do it: a deeper oracle is a *more*
expectimax-shaped one.

## A cross-check I didn't plan and am glad I ran

Expectimax d2 came out at 44% against the heuristic. `docs/benchmark.md` §6 puts
it at 42.9% [36.8, 49.2] on v2. Those are the same number, produced by two
entirely separate code paths — the `bench` match runner there, `CaptureStored`
plus a fresh export/score pipeline here.

I ran it as a sanity check on my harness. It is better than that: it is evidence
the offline capture reproduces live-shaped battles faithfully, which is the
assumption the whole `decision-sim` path rests on. Without it, "the metric works
offline" would have been an assertion about code I had just written.

## Threshold calibration, briefly

`BlunderThreshold = 300` was flagged in the original handoff as a first guess to
calibrate once the data existed. On v2: random's *median* regret is 192, below
the bar. So 300 is not "notices sloppiness" — it is a severe-tail cut, and even a
policy choosing uniformly at random sits under it half the time. That is
defensible for a headline metric, and it is exactly why median regret belongs in
the table next to it rather than behind it. Left the constant alone; the number
is fine once you know what it means, and now the doc says.

## The ranking is now a test, not a table

I first wrote this entry with "wire the ordering into CI" as the open item, then
noticed the objection to that: the monotonicity above is the metric's *only*
soundness property, and leaving it as a number in a document means it is checked
whenever someone re-reads the document. It is the thing most likely to break
silently if the oracle or the value function moves.

`TestScoreDecisions_RanksAWorsePolicyAsBlunderingMore` pins it — random against
heuristic, one team, a depth-2 oracle, 0.8 seconds. Coarse on purpose: the
property is the *direction* of the ranking, and the widest available gap is the
one least likely to flake. The four-policy sweep stays in `decision-sim` where
it can afford the nine minutes.

## Still open

A second oracle of a different family. Everything above says the yardstick's
algorithm is a confound, and nothing in the repo can currently measure how big
that confound is — the honest statement is "this bias exists and is unquantified,"
which is weaker than I'd like to leave a caveat.
