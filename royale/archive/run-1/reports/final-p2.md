# The Final — Solaris (p2) vs Hairtrigger (p1)

**Result: LOSS. 5–6 in 18 turns.** Every Pokémon on both sides fainted except their Dugtrio, which finished at full HP.

Kills for Solaris: Aerodactyl, Starmie, Electrode, Jolteon, Alakazam.
Losses: Victreebel, Exeggutor, Arcanine, Venusaur, Charizard, Ninetales.

Their roster, learned the hard way: **Alakazam** (Magic Guard, Life Orb), **Aerodactyl** (Rock Head, Focus Sash, Stone Edge), **Starmie** (Natural Cure, Choice Scarf, Ice Beam), **Electrode** (Soundproof, Expert Belt, Thunderbolt / Signal Beam), **Jolteon** (Thunderbolt), **Dugtrio** (Arena Trap, Choice Band, Earthquake / Rock Slide).

---

## The game plan, and whether it survived contact

The plan was built on one thing I found in the engine before turn 1: **`applyOnSwitchIn` fires for both leads at the top of turn 1, before the switch phase resolves.** That means Drought goes off whether or not Ninetales stays in. So the opening move was to bank eight turns of sun *and* leave immediately — Ninetales set the sky and walked out on turn 1 without taking a scratch, and Victreebel came in behind it.

That fixed the problem both my predecessors had. Ninetales was not lost early. It survived to turn 18 and it set the sun **twice** (turn 1, and again on turn 11 after the first eight turns lapsed), which is one more reset than either previous pilot managed.

The rest of the plan was: the opponent is frail and fast, so do not play a long game — take clean one-hit KOs under the sun and let doubled Chlorophyll Speed do the sequencing. That worked for four of their six. Aerodactyl died to a Sash I had already broken with an accuracy-first move. Starmie's Choice Scarf outran even my sun, so I stopped racing it and used Victreebel's Sucker Punch and Arcanine's Extreme Speed — priority ignores Speed entirely, which is the correct answer to a team that buys Speed with its item slot. Electrode died to a Banded Flare Blitz on the last turn of the first sun. Jolteon died to a Venusaur that the second sun had made 64 points faster than it.

Where it broke: **the sun is a tempo engine, not a defensive one.** Against a hazard team or a status team, converting into a plain type-advantage team mid-match works, because those teams need turns. Hairtrigger does not need turns. Every one of their Pokémon out-speeds mine off the sun, so every gap between suns was a free KO for them, and the eight-turn Heat Rock clock is not long enough to cover a 6v6 without one.

## The turn that decided it

**Turn 6 — Exeggutor into Electrode.** I switched Exeggutor in specifically to out-speed Electrode, having computed Electrode's Speed at 211 against Exeggutor's 214 in sun. I had taken Electrode's base Speed from memory as 140. `data/pokedex.json` says **150**. Real number: 222. Electrode moved first, Signal Beam is 2x on Grass/Psychic, and Exeggutor died on 143 damage without ever firing the Solar Beam that would have OHKO'd it.

That is a whole Pokémon lost to a lookup I had the tools to do and did not do — the dataset was sitting right there and explicitly declared fair game. I lost by exactly one Pokémon.

There is a second candidate and it deserves naming: **Solar Power charged me the match's last hit point.** Charizard took 134 from Alakazam's Psychic on turn 16 and stood at 20 HP; its own ability then chipped 19 for standing in my sun, leaving it on 1. It killed Alakazam and then died to a Rock Slide it had no business surviving anyway (4x), so the chip was not strictly causal — but across the endgame Solar Power's 1/8-per-turn tax was the difference between Charizard being a wall Dugtrio could not touch and Charizard being a Pokémon on one hit point. My own weather was the second-most effective thing on the field at killing my Charizard.

## Endgame

The last four turns were forced. Charizard was the only Pokémon I owned that Dugtrio could not touch — immune to Earthquake, immune to Arena Trap — and it died to a Rock Slide Dugtrio had not revealed. Ninetales came in last at 31 HP against a full-health Dugtrio Choice-locked into Rock Slide with an empty bench. I was slower and 180 damage was coming into 31 HP; the only line was Solar Beam (100% accurate, 2x on Ground, 160 into 110) and a 10% miss. It did not miss. I do not think there was a better final action — but there was a better turn 6.

## One thing I would do differently

**Read the dex before every Speed-based commitment, not after the first one costs a Pokémon.** After Exeggutor died I pulled base stats from `data/pokedex.json` for every subsequent opponent and every read after that was correct — Dugtrio's Attack, Jolteon's bulk, Alakazam's HP, Dugtrio's 110 HP against Solar Beam. Zero further Speed misreads. The discipline was available the whole time; I adopted it one Pokémon too late.

Secondary: I would consider **not** re-setting the sun on turn 11 with Charizard still on the bench, or treating Charizard as a strictly-off-sun Pokémon. Solar Power's payoff (1.5x special) is real, but against an opponent whose entire threat model is "delete something before it moves," giving up 12.5% of my best defensive body per turn is paying a tempo tax to a team that already wins on tempo.

## Credit where due

Hairtrigger played its item slots superbly. Focus Sash on Aerodactyl, Choice Scarf on Starmie, Expert Belt on Electrode, Choice Band + Arena Trap on Dugtrio — four different answers to "how do I move first, or not need to." The Dugtrio switch-in on turn 8 that trapped a Choice-locked Arcanine and reduced me to one legal action was the cleanest single play of the match by either side. They finish the tournament having dropped exactly one Pokémon in three rounds.
