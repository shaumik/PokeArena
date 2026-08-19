# sf1 — Solaris vs The Apothecary

## Result

**Solaris wins 4–0 on Pokémon remaining, on turn 19 of a 60-turn cap.**

Survivors: Charizard (154/154, untouched), Exeggutor (94/171, paralysed), Arcanine (67/166), Venusaur (33/156). Losses: Ninetales, Victreebel.

The Apothecary was swept to zero. Kills went Jynx (Arcanine, T2), Gengar (Victreebel, T9), Golbat (Ninetales, T7), Zapdos (Exeggutor, T14), Arbok (Charizard, T17), Raticate (Charizard, T18). They took Ninetales and Victreebel — both deliberately spent.

Their Round 1 win took 39 turns. This took 18.

## The game plan, and whether it survived contact

It did not survive contact. It was replaced before turn one, on purpose.

Scouting `royale/reports/r1m3-judge.md` — public record, and the single most valuable thing I did all match — gave me their whole roster before a piece moved: Jynx, Raticate, Arbok, Gengar, Golbat, Zapdos. Two facts fell straight out of the type chart:

1. **Nothing on their roster resists Fire.** Four of the six *resist Grass*, three of them at 0.25x.
2. **Their status axis was structurally half-dead against this team.** My three Fire-types cannot be burned; my two Poison-types cannot be poisoned or Toxic'd. Will-O-Wisp and Toxic — the moves that beat my predecessor — had at most three legal targets each.

So I stopped playing the Grass team that won Round 1 and played the Fire team. Grass STAB was used exactly zero times in eighteen turns.

Two engine facts I verified up front and both decided turns:
- `effects.go:530` — Poison/Steel refuse poison, Fire refuses burn. Confirmed the immunity read was real, not canon-by-assumption.
- `switching.go:48` — switching out **zeroes `SleepTurns`**. Sleep in this engine costs one turn if you pivot, not three. That turned Jynx's turn-0 Lovely Kiss from a disaster into a tempo loss, and it is why I switched Ninetales out on turn 1 rather than sitting there.

That last one cut both ways and I should have seen it sooner: it is also why my Sleep Powder on Gengar bought nothing — they pivoted Gengar out, its counter reset, and it came back awake two turns later.

**The one thing I got wrong about my own deck:** on turn 5 I switched Ninetales in to "reset" the sun at 3 turns remaining. Drought is a switch-in trigger and is a **no-op while sun is already up** — the counter went 3→2 and no weather line printed. I burned a turn and learned the reset only works once the sky has actually gone out. I used that correctly on turn 10 and got the full 8 turns off Heat Rock.

## The turn that decided it

**Turn 14 — Exeggutor's Psychic into Zapdos.**

Zapdos was the match. It was their Round 1 MVP for exactly one reason: Roost heals ~99 and nothing I own hits it super-effectively, so it wins any grind. On turn 13 I measured the problem honestly — Psychic did 93, Roost heals 99 — and concluded I could not win that race. So I stopped trying to win it and instead played to *force* it, driving Zapdos low enough that Roosting became its only non-losing move, because a turn it spends healing is a turn it deals no damage.

Turn 14 paid off twice over. Psychic rolled 107 **and** proc'd its Sp. Def drop, which broke the arithmetic permanently: at −1 SpD the same move became ~160, more than a full Roost. Zapdos, on ~21 HP, could no longer heal out of range no matter what it did. It spent its last action on a Toxic that **failed** — Exeggutor was already paralysed by their own Thunder Wave, and one status excludes another. Their signature move, blanked by their own.

That is the whole match: their win condition died on turn 14 and they had no second one.

The honourable mention is **turn 16**, the cheapest good decision I made. Raticate sat on 16 HP burning 8 a turn from its own Flame Orb — already dead at the end of turn 17 by arithmetic. So I refused to trade anything real for it and fed it my 1-HP Victreebel instead. Charizard entered at a full 154 and finished the game without ever dropping below it.

## The turn that cost me

**Turn 11 — leaving a paralysed Ninetales in front of Arbok.**

The reasoning was that an already-statused Pokémon is status-proof, so Ninetales was the right body to absorb Glare turns and keep paralysis off Charizard. That logic was sound. The error was in what I assumed Arbok *was*: I read Intimidate + Rocky Helmet + Glare + Venoshock as a defensive status spreader with base-65 special attack and no real damage, and wrote as much in my turn-10 reasoning. It had **Earthquake**. Ground is 2x on Fire, it did exactly 116 into 116 HP, and Ninetales died without firing.

That killed my only Drought. Everything after turn 12 was played with no possibility of weather ever returning — I won the last six turns as a type-advantage team, not a sun team, which is precisely the failure mode my predecessor described. I got away with it because I was already 5–3 up.

## One thing I would do differently

**Lead into the scouting report, not around it.** I knew from `r1m3` that Arbok was the only Ground-type on a roster facing three Fire-types, and I still parked a Fire-type in front of it on a 25%-fail paralysis and called Arbok harmless. The report told me Arbok died on turn 5 of its previous match *without ever attacking* — that is not evidence it cannot attack, it is an absence of evidence, and I treated the two as the same thing. The correct turn-11 play was Exeggutor, which resists Ground 0.5x and OHKOs Arbok with 2x Psychic; it costs nothing and Ninetales lives to set an eighth sun.

Concretely: when a scouting source shows a Pokémon with two unknown move slots, assume the slot that beats me is in one of them, and pick the switch-in that is safe against *that*.

## Notes for whoever pilots this next

- **Read `royale/reports/` first.** It is legal, it is a primary source, and it is worth more than any single turn of play.
- **Drought only fires on switch-in and only when the sky is already out.** Do not "top up" the sun; you will lose the turn for nothing. Let it lapse, then pivot Ninetales in.
- **Switching out zeroes the sleep counter.** Sleep costs one turn if you pivot. It also means sleep *you* inflict is worth almost nothing against an opponent willing to pivot — do not pay a turn for it expecting three.
- **Fire-types cannot be burned; Poison-types cannot be Toxic'd.** Against any status deck, count how many legal targets their status moves actually have before respecting them.
- Facade is bugged in this engine (flat 70 BP, no doubling off the user's own burn — see the r1m3 BUGS section). Guts still works. A Flame Orb Raticate is much less scary than it reads, and its self-burn is a clock you can simply wait out.
