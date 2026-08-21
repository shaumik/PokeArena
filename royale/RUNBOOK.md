# Running the battle royale again

An agent tournament: six themed teams, single elimination, every trainer slot
driven by its own agent process that sees nothing but the engine's fog-of-war
projection, with a third agent refereeing each match and auditing the engine
while it runs.

It is worth re-running for two reasons. It is a genuine end-to-end exercise of
the arena — fog of war, the turn loop, every archetype's mechanics — and the
referees are unusually good at finding engine bugs, because they are reading
real battle logs rather than fixtures written by someone who already believes
the mechanic works. The first run confirmed five defects that the unit suite
had missed for as long as it had existed; the second confirmed four more,
including one — Fake Out with no first-turn restriction — that decided its own
final.

---

## The prompt

Paste this into a fresh session at the repo root. Everything else in this file
is background the agent can consult.

> Run a PokéArena battle royale. Six teams, single elimination, one champion.
>
> Every trainer slot is its own independent agent — a fresh one each round,
> briefed on its roster and on exactly how its predecessor won or lost. Each
> team has a declared archetype (stall, hazard stack, glass cannon, sun, trick
> room, status). Each match gets a third agent as judge: it referees, audits the
> engine against its own source while the match runs, and reports anything that
> does not make sense to me.
>
> Use `cmd/royale` as the match broker and `royale/teams/*.json` as the rosters.
> Read `royale/RUNBOOK.md` first — it has the format, the agent prompt
> templates, and the operating rules that were learned the hard way last time.
>
> When it is over, build the tournament report and publish it as an artifact so
> I can feel the whole event: the bracket, the ladder, a scrubbable turn-by-turn
> replay of every match, and what the referees found.

To vary it, add any of: different archetypes, a bigger bracket, a different turn
cap, or "and re-run the full-game integration suite afterwards."

---

## Before you start

**Rebuild the binary.** This is the single most important line in this file.

```sh
go build -o bin/royale ./cmd/royale/
```

`bin/` is gitignored, so a stale binary is invisible in `git status`. Last time
the whole tournament ran on a build that predated two engine patches, and
nobody noticed until the final's referee proved it behaviorally — 0 occurrences
in 600 seeds on current source against 53 on the parent commit. Rebuild as step
one of every run, and again after any engine change.

Then sanity-check the rosters:

```sh
for t in royale/teams/*.json; do ./bin/royale validate -team "$t"; done
```

---

## The format

Six teams, three Round 1 matches so everyone plays at once, then the most
dominant Round 1 winner takes a bye and the other two fight for the right to
meet it. Five matches, fifteen agents.

| Round | Matches |
|---|---|
| Round 1 | three matches, all six teams, three eliminated |
| Semifinal | the two lesser Round 1 winners |
| Final | the bye holder vs the semifinal winner |

**The bye rule has to be announced before Round 1, not chosen after it:** most
Pokémon remaining, tiebreak fewest turns. Deciding it afterwards is picking a
finalist.

**Turn cap 60**, and past it the match goes to a decision on Pokémon standing,
then total HP. Tell both players the cap exists — a stall team that knows it is
behind on the clock plays differently. It has never actually been reached: the
longest match of the first run ended on turn 39, and of the second on turn 22.

Create a match with:

```sh
./bin/royale new -id r1m1 -round "Round 1 — Match 1" \
  -p1 royale/teams/guillotine-club.json -p2 royale/teams/perish-row.json \
  -seed 2601 -max-turns 60
```

Seeds are arbitrary but write them down — they are what makes the match
reproducible.

---

## Running a match

Launch all three agents for a match **in the same message** so they run
concurrently. They coordinate purely through the match directory; no agent
talks to another.

A player's loop is two commands:

```sh
./bin/royale team -id <match> -slot p1              # once, at the start
./bin/royale view -id <match> -slot p1 -wait        # blocks until it is your turn
./bin/royale act  -id <match> -slot p1 -action move:2 -why "one line, published"
```

The judge watches with the per-match token:

```sh
./bin/royale log -id <match> -token judge-<match>-<seed> -from N -wait
```

Round 1's three matches can run at once — nine agents — but launch each match's
three agents together. A player whose opponent has not started yet will block on
`view -wait` and time out.

### What to put in a player's prompt

- Its identity and creed, in character. Grade it on archetype fidelity, but say
  plainly that winning comes first and deviating is allowed if it says why.
- Its dossier for rounds after the first: what its predecessor did, the turn
  that decided it, and the mistake it named. The pilots consistently applied
  these — the semifinal Apothecary inverted its predecessor's anti-Trick-Room
  read on turn one and it paid off immediately.
- The two-command loop, the turn cap, and that `-why` is published but never
  shown to the opponent.
- The fair-play rules (below).
- **Keep per-turn reasoning short.** A 39-turn match is 80+ tool calls; a pilot
  that writes an essay every turn runs out of room before the match ends.
- Never stop before seeing `BATTLE OVER`.

### What to put in a judge's prompt

- The three jobs: referee/engine auditor, stuck-match watch, commentator.
- **Investigate before reporting.** Every confirmed finding last time came with
  the engine source that proved it. A confident wrong bug report is worse than
  none, and the judges cleared far more suspicions than they confirmed.
- The mechanics specific to that matchup to watch closely — weather chains for a
  sun match, Trick Room duration and priority brackets for a trick room match,
  status immunities and Guts/Hex/Venoshock for a status match.
- Any engine change made since the last match, and ask it to sanity-check that
  fix against live play.
- A fixed report shape, so the results are comparable and the report generator
  can parse them: `## Verdict`, `## The story`, `## Scorecard`, `## MVP`,
  `## Notable turns`, `## BUGS` with a CONFIRMED / NOT-A-BUG / UNCERTAIN verdict
  per finding.

---

## Fair play

Players may use only `team`, `view` and `act`. Reading `royale/battles/**`,
`royale/teams/`, or the `log` / `report` commands is disqualification. The
previous run's teams, battles and reports live under `royale/archive/`, and are
off limits to contestants for the same reason `royale/reports/` is.

Reading the engine source and the dataset **is** fair — that is public
knowledge, like a competitive player consulting a damage calculator, and it
produced some of the best play. One pilot read `protect.go` to confirm the
1/(3^n) stall chain and turned two standoffs into arithmetic.

**Do not let contestants read `royale/reports/`.** This was allowed in the early
rounds and both semifinalists said, unprompted, that it handed them the
opponent's entire roster before turn one. The reports are the tournament's
public record; they are also a complete scouting dossier.

---

## Operating rules learned the hard way

**Never patch the engine mid-match.** Hold every fix until the match ends, then
apply it, rebuild, and note which revision each match ran on. A match that ran
on two revisions no longer replays from its seed.

**Rebuild after every patch.** See above. The discipline of holding fixes is
worthless if the binary never picks them up.

**Watch what the harness leaks.** `view` used to print the opponent's *theme
string*, and a theme describes a roster — the sun team's named Drought,
Chlorophyll, Solar Power and Charizard outright. Fog of war is the arena's whole
premise; audit the projection, not just the engine. The foe now gets a codename
and nothing else — see the codename rule below, which is the same leak's second
life.

**Take the judges' findings seriously, and verify them yourself.** Two referees
independently found the faint-window bug from different matches. One traced it
to an invariant `turn.go` documents in its own comments. Another worked out that
Facade was missing from the engine entirely by comparing damage numbers against
the base-power bands.

**Expect long matches.** Trick room versus status ground to turn 39. Budget for
it and prefer running matches concurrently.

**Give every team a codename, and make it say nothing.** The second run's
harness correctly hid every theme string — and then handed the same information
over in the trainer name. `view` printed "Your opponent is The Low Ceiling —
their roster and archetype are hidden", a sentence that refutes itself, and the
champion said outright that the name told it Trick Room before turn one. Perish
Row, The Caltrops, Guillotine Club, Miasma, Meridian: every one was a tell.

The harness no longer shows the name at all. A team file carries a `codename`
next to its `name`, and the codename is the only identity the other seat is
ever shown — in `team`, in `view`, in the resolved-turn recap, and in the
battle lines themselves, because the engine is handed the codename as the
side's trainer. The six rosters play as Umber, Cobalt, Saffron, Verdigris,
Cinnabar and Indigo: colours, chosen to mean nothing.

What the harness cannot check is whether a codename is *itself* a tell, so that
part stays yours: name teams after nothing — a colour, a city, a number. Two
things it does check: a codename equal to the team name is refused, and a team
file with no codename falls back to a neutral seat label ("Trainer P1") rather
than to the real name, so forgetting is the safe outcome and not the leaky one.
The judge still sees both identities — `log` prints the codename legend in its
header, and the report is built from `meta.json`, which keeps the real names.

**Check the ability actually exists before building a roster on it — and let
`validate` check for you.** Meridian was built around Harvest and its theme
string sold it as a pillar; Harvest was registered inert in `abilities.go` with
no hooks, so the team played the whole tournament a Pokémon and a half short.

`royale validate` now warns on any pick whose ability or item the engine does
not model, naming the slug and why, and `royale validate -strict` exits
non-zero on one — run it over every roster before a bracket. It is derived from
the registry itself (`engine.AbilityInertReason`), so it cannot fall behind the
way a hand-kept list would. Harvest passes now; Weezing's Neutralizing Gas on
The Caltrops does not, which is the pick that cost that team a Pokémon.

**Tell the pilots their dossier is testimony, not documentation.** Meridian's
Round 1 report claimed the sun "dies with" its setter. It does not — weather is
a field state with its own countdown. The semifinal pilot inherited that
sentence, checked it against `setWeatherFromAbility`, found it wrong, and relit
a second full eight turns of sun off a switch its dossier said was pointless. A
pilot that trusted the briefing would have been worse off than one that never
read it. Say so in the prompt: the dossier is what your predecessor *believed*.

---

## The report

```sh
python3 royale/digest.py          # match dirs -> royale/tournament.json
python3 royale/build_report.py    # + royale/tournament-meta.json -> the HTML
```

`royale/tournament-meta.json` is authored by hand — the bracket, standings,
champion and the organizer's notes, since bracket placement is a tournament fact
rather than something derivable from match rows. Then publish
`royale/tournament-report.html` as an artifact.

Two things worth keeping in the report: the agents' own `-why` lines, which are
the best writing in the whole exercise, and an honest account of what the
organizer got wrong. Last time that was the stale binary and the theme-string
leak.

If you change the team colors, re-validate them with `python3
royale/palette_check.py` — WCAG contrast for each colour on its own ground, and
CIE76 ΔE between the two sides of each pairing named in
`tournament-meta.json`'s `colour_pairs`. The two sides of *every* match must be
distinguishable on both light and dark grounds; the first run's assignment
failed because the final turned out to pair yellow against orange, and it was
checked by hand. With no pairs supplied the script checks all fifteen
combinations, which is the safe thing to do before the bracket is known.

Stamp every match with the engine revision it ran on — `tournament-meta.json`
carries a `revisions` map and the match cards render it beside the seed. A
tournament that patches between rounds has to say which build each match used,
or none of the seeds mean anything.

---

## Related

- `cmd/royale` — the match broker
- `internal/engine/fullgame_integration_test.go` — 147 full games with per-turn
  auditing, the standing regression net for anything the referees find
- `docs/engine-findings.md` — what is still open
