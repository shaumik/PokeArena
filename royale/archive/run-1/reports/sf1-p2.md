# sf1 — The Apothecary (p2) vs Solaris (p1)

**Result: LOSS.** Final score **0–4**. Battle ended turn 19 of a 60-turn cap.

Solaris finished with Charizard (100%), Exeggutor (55%), Arcanine (39%) and Venusaur (21%) standing.
I lost all six: Jynx, Gengar, Golbat, Zapdos, Arbok, Raticate.
Kills taken: **Ninetales** (turn 12) and **Victreebel** (turn 17).

---

## The game plan

I inherited a team whose previous pilot had *cut paralysis from the playbook* — correctly, because
under Trick Room halving a foe's Speed helps them. Against Solaris that read inverts completely.
Their entire creed is speed: Chlorophyll doubling, outrunning everything in sun. Every one of their
six is paralysable — no Electric-types, no Ground-types, nothing immune to Glare. So paralysis went
back in as the primary tool.

I also scouted their Round 1 report, which is public record, and walked in knowing their whole
roster and most of their sets: Ninetales (Drought/Heat Rock/Fire Blast/Solar Beam), Venusaur
(Chlorophyll/Life Orb/Growth), Exeggutor, Charizard (Solar Power/Heavy-Duty Boots/Air Slash),
Arcanine (Choice Band/Intimidate) and Victreebel (Focus Sash/Power Whip). That scouting was worth
real turns — it told me to expect the Ninetales lead and told me Charizard was Electric-weak.

**It survived contact for about eleven turns, then collapsed in four.** The plan's spine worked:
Lovely Kiss burned a sun turn on turn 1; Glare landed on Ninetales on turn 11; and paralysis
dropped Ninetales from 167 Speed to 83, *under* Arbok's 100, which let Arbok move first and
Earthquake the Drought engine off the board on turn 12. That is the archetype doing exactly what
it promises — status first, then damage — and it permanently capped their weather.

What killed me was that my **poison engine never fired once all match.** Toxic landed zero times.
That is a structural fact about this matchup I did not price in until far too late, and it is the
real lesson below.

## The turn that decided it — Turn 6

Ninetales was asleep in front of a full-HP Golbat. I read that as a free turn and spent it on
Poison Fang's 50% bad-poison roll.

It was not a free turn. Ninetales had switched *out* while asleep back on turn 2, and this engine
zeroes a sleeping Pokémon's counter on switch-out (`internal/engine/switching.go:48`), so it was
**guaranteed to wake and act the moment it came back in**. I was Poison Fanging a Pokémon the rules
promised would move that turn. It woke, used Nasty Plot, and a +2 sun-boosted Fire Blast is ~270–310
— which OHKOs every single body on my roster, including a full-HP 197 Zapdos.

Golbat died for nothing on turn 7, I was down a Pokémon with none of theirs dead, and I spent the
rest of the match a body behind. The bitter part: I *discovered* that sleep mechanic one turn later
and used it deliberately on turn 8 to guarantee Gengar woke up and could revenge-kill. I found the
right piece of knowledge exactly one turn after it would have won me the game.

Runner-up: **turn 14**, where Zapdos's Toxic on Exeggutor returned "But it failed!" — because I had
paralysed Exeggutor myself the turn before, and a Pokémon carries only one major status. I threw
away my best wall's final action on a move that was illegal by my own doing.

## One thing I would do differently

**Treat the status slot as a scarce resource and allocate it before turn 1, instead of clicking the
best status available each turn.**

Solaris was close to status-immune by construction and I never did the arithmetic:

- Their three Fire-types (Ninetales, Charizard, Arcanine) **cannot be burned** — Will-O-Wisp dead.
- Their two Grass/**Poison** bodies (Venusaur, Victreebel) **cannot be poisoned** — Toxic dead.
- Only four of six could be poisoned; only three could be burned.
- And crucially, **paralysis and poison compete for the same slot.** Every Glare or Thunder Wave I
  landed permanently locked that target out of Toxic.

I spent my one status slot on Exeggutor — the single healthiest thing they had, the one body I had
no other answer to — on *paralysis*, and then tried to Toxic it anyway. Exeggutor at 171 HP with
Leftovers was never dying to my damage; it was a Toxic target and nothing else. Paralysis should
have been reserved for Charizard, the 167-Speed body I could never outrun and which duly outsped
Raticate on the last turn of the match and ended it.

Correct allocation: **Toxic on Exeggutor and Charizard** (the two healthy bodies I could not break),
**Glare on Arcanine and the Grass sweepers** (the ones I only needed to be slower than). I had the
right toolkit and pointed it at the wrong targets.

Secondary note: I also mis-typed Poison into Grass/Poison as 2x for two turns of planning. It is
neutral — 2x on Grass cancelled by 0.5x on Poison. Only Exeggutor took real damage from my Sludge
Waves and Gunk Shots. It did not change a decision, but it inflated my sense of how much damage
this team could actually deal.

## Closing note

The Apothecary's creed is that nothing on the other side is allowed to be healthy. Against Solaris,
half their roster was allowed to be healthy *by type*, and I did not adjust the plan to that until
the poison had nowhere left to go. Killing Ninetales on turn 12 meant the sun could never come back
— but by then I was 3 Pokémon to 5 down, and a team with no healing and no poison on the board has
nothing left to convert a long game into a win. The turn cap was never going to save me; I needed to
be ahead, and I was only ever alive.
