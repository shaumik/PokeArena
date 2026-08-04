# Re-sweeping expectimax on the trained library

Library v2 shipped in the [previous round](2026-08-04T03-40-library-v2-trained-spreads.md).
The thing I did not do there — and should have, in the same breath — was ask
which published numbers had just gone stale underneath it.

They had. `docs/benchmark.md` §6 carries the only load-bearing measurements in
the whole document: the expectimax depth sweep, **50% (d1) → 40% (d2) → 38%
(d3)**, which is the evidence for the doc's most important limitation ("an
oracle that plays worse with more compute cannot define optimal"). Every one
of those was measured on v1 neutral teams. Nothing was wrong with them; they
had simply stopped describing what ships.

## What the re-sweep found

240 games per depth — 6 teams × 20 seeds × 2 orientations, expectimax vs
heuristic:

| Depth | v1 | v2 | v2 Wilson 95% |
|---|---:|---:|---|
| 1 | 50% | 54.4% | [48.1, 60.6] |
| 2 | 40% | 42.9% | [36.8, 49.2] |
| 3 | 38% | 42.1% | [36.0, 48.4] |

**The shape survived.** Expectimax is 2–4 points stronger at every depth on
trained teams, but the ordering (d1 > d2 ≈ d3) is intact and the slope is the
same size — 12.3 points d1→d3 on v2 against 12 on v1. d2 and d3 still sit
inside each other's intervals; the d1→d2 drop is still the real one, and its
intervals still graze rather than separate cleanly.

That is the outcome I wanted and did not assume. §6's conclusion — expectimax
is not a valid per-move oracle on this format — rests on the *non-monotonicity*,
not on the specific percentages, and the non-monotonicity is a property of the
search's opponent model rather than of the teams it plays. If the slope had
flattened on v2 I would have had to reopen the whole section. It didn't.

One genuine change: at depth 1 expectimax now edges *ahead* of the heuristic
(54.4%) where on neutral teams it drew level. The default bench depth is 2, so
the headline baseline ordering is unaffected — but it is a reminder that
per-depth numbers are library-specific, and the doc now says to quote them with
a `team_library` version attached. Which is exactly what `team_profile` and
`team_library` in the run header were added for last round; nice to have them
earn their keep this fast.

## Two things I had to be careful about

**The v1 numbers I could not re-measure.** §6 also cites the *pre-fix* sweep
(61/46/27) and the "~61% → ~50%" drop from removing the phantom KO. Those
describe a bug that no longer exists, so there is nothing to re-run. Labelled
them as v1-era and unrepeatable rather than quietly leaving them adjacent to
fresh numbers, where a reader would reasonably assume all four measurements
came from the same run.

**My own v1↔v2 turn-count table was mislabelled.** Last round I produced the
"how big is the discontinuity" table by stripping the spread fields off the v2
teams and replaying. I then wrote the column header as "v1 avg turns". It
isn't: Bruiser's Tauros changed a move that round (Fire Blast → Megahorn), so
stripped-v2 and literal-v1 are not the same teams.

Stripping is the *better* comparison — it holds movesets constant so the
column isolates the spread, which is what the table claims to show. The header
was just wrong about what it was showing. Fixed to say "spreads stripped" vs
"as shipped", with a note about why stripping beats replaying literal v1. The
numbers didn't move; only the claim about them did.

## The general lesson

"Update the library" and "update the numbers measured on the library" are the
same task, and I finished the first and stopped. The tell was available: I had
just written a section arguing that a run header must record what the teams
actually do, precisely because results are not portable across libraries — and
then left results from the old library sitting in the same document. Worth
watching for the next time a dataset moves under a published figure.
