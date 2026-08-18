# THE FINAL — Hairtrigger (p1) vs Solaris (p2)

**Result: WIN. 1–0 on Pokémon remaining, in 18 turns.**
Dugtrio survived untouched at 111/111. Aerodactyl, Starmie, Electrode, Jolteon and Alakazam were all spent getting it there.

Solaris' roster, as it revealed itself: **Ninetales** (Drought / Heat Rock), **Victreebel** (Chlorophyll, Focus Sash, Jolly max Atk), **Arcanine** (Intimidate, Choice Band), **Exeggutor** (Leftovers), **Charizard** (Solar Power, Heavy-Duty Boots), **Venusaur** (Life Orb, Chlorophyll).

---

## The game plan, and whether it survived contact

It did not survive turn zero.

The plan I walked in with was the obvious one: Dugtrio traps Ninetales, Band Earthquake deletes it before it moves, the sun never comes on, and the fastest team in the arena does what it does. I had checked the engine and confirmed the two facts that made it work — `setWeatherFromAbility` returns early if the same weather is already up, so Drought cannot refresh itself, and Drought's default is five turns.

Then the first `view` showed **Heat Rock**. Eight turns, not five. That single item inverted the entire plan. Killing Ninetales no longer shortened the sun in front of me; it only denied the *next* one. Sun was going to be up for eight of the first eighteen turns whatever I did, and in it every Chlorophyll body on their side outran my whole roster:

| Mine (Speed) | Theirs in sun |
|---|---|
| Starmie (Scarf) 274 | Venusaur 290 |
| Electrode 211 | Victreebel 268 |
| Jolteon / Aerodactyl 200 | Exeggutor 234 |
| Alakazam / Dugtrio 189 | |

So the creed — *delete it before it ever moves* — was simply unavailable for the first third of the game. The revised plan, formed on turn zero, was: **survive the sun, do not trade badly, and win the back half of the game outright**, because off the sun I outspeed everything they own. That plan held. I lost five Pokémon and won anyway, because every one of the five was spent buying a specific thing rather than lost in a fair fight.

What each one bought:
- **Aerodactyl** — the only body with a Focus Sash, sent to eat the opening Chlorophyll hit. Took Victreebel to 21%.
- **Starmie** — the only thing on the team faster than a sun-boosted Victreebel (274 vs 268). Killed it, then died to Sucker Punch.
- **Electrode** — killed Exeggutor with a 4x Expert Belt Signal Beam, then baited Arcanine's Choice lock.
- **Jolteon** — 2x Specs Thunderbolt off Charizard, took Ninetales to 19%, then deliberately died so Alakazam could enter over a body.
- **Alakazam** — killed Venusaur through Sleep Powder, then put Charizard into Solar Power range.

## The turn that decided it — turn 7

Arcanine was the real crisis, not the sun. Choice Band, Intimidate, and **Extreme Speed**: +2 priority, ~160–220 damage, which one-shots all four of my remaining Pokémon. Against a +2 priority move, being the fastest team in the arena is worth exactly nothing. If Arcanine stayed locked into Extreme Speed it beat my entire team single-handed, one Pokémon per turn, and none of them would ever get a move off. My only counter — Dugtrio's Band Earthquake, a 2x OHKO — could never go first.

So on turn 7 I did not try to win the turn. I put Electrode in front of it and clicked Thunderbolt, playing for **which move Arcanine would lock into**. It chose Flare Blitz.

That was the game. Flare Blitz is priority zero. The moment it was locked in, Arcanine went from unanswerable to dead: Dugtrio outspeeds it 189 to 162, Arena Trap meant it could never switch out to reset the lock, and the recoil had already taken a third of it. It also crit and killed Electrode on that same turn — and it still lost, because the information was worth more than the Pokémon.

The sun faded on the very same turn. From turn 8 onward I was the faster team again and never gave the lead back.

Two smaller reads that carried real weight, both taken off damage numbers the way my predecessor took Unaware off a 41:
- Exeggutor's Leftovers ticked for **exactly 10**. That prices its max HP at 170 — zero HP investment — which told me it was an all-out attacker and that Signal Beam at 4x would erase it in one. It did.
- Venusaur's Life Orb recoil was **exactly 15**, pricing it at 155 max and confirming no bulk. That let me calculate, precisely, that Alakazam lives through one Giga Drain or Sludge Bomb and kills back.

## The one thing I would do differently

**Turn 1 with Aerodactyl.** I clicked Stone Edge into a Victreebel at full health, then clicked it again at 21%, and Aerodactyl died to a Power Whip on the turn its second Stone Edge would have finished the job. The correct move was **Dragon Dance on turn 1**. Aerodactyl was at full HP with a live Focus Sash — it was guaranteed to survive that turn no matter what hit it, which is precisely the turn you spend on setup rather than on damage. At +1 it sits at 400 Speed, which is faster than *every* Chlorophyll sweeper on their roster in full sun, and +1 Stone Edge is 186 — an outright OHKO on Victreebel rather than an 80% chip. I had identified before turn one that Dragon Dance was the only answer my team owned to the sun speed tier, and then never pressed it. I spent the one guaranteed-survival turn in the game on a coin-flip 80% accuracy roll instead.

I also nearly threw the match on turn 16 by considering Earthquake into a 1%-HP Charizard, which would have done zero and lost me Dugtrio and the tournament. Rock Slide over Stone Edge there — 90% over 80% — was the right kind of small discipline at match point.

---

*"A wasted turn is a lost Pokémon." Five of six went down. Not one of them was wasted.*
