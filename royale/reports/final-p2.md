## Verdict

Lost. Guillotine Club 3–0 on bodies standing; I took Pinsir, Hitmonlee and Scyther and lost all six. The roster that had never dropped a match dropped the one that counted.

## Game plan

Inherited and unchanged: two Rooms, spent hard, free entries only. Expose Mr. Mime, bank Porygon behind a full-HP body, let depleted bodies die so the next enters after a faint rather than into a live attacker.

## What actually happened

Room One was everything advertised. Mr. Mime traded itself for 102 chip on a Life Orb Pinsir, Marowak finished it, then Earthquake broke Hitmonlee's Sash and Double-Edge collected it. Cloyster came in free and deleted Scyther in two icicles. Three-nil up.

Then Persian used Fake Out on turn 13 without having switched in, and I finally read `data/moves.json`. This engine's Fake Out carries **no once-per-entry restriction** — priority +3, 100% flinch, ten PP. Trick Room does not make you fast; it reorders one priority bracket. Persian flinched Machamp to death at 10 HP, then flinched Omastar through both remaining ticks of Room Two. Room Two produced zero kills.

Trace paid out exactly as patched — Porygon re-entered against Raichu and copied Lightning Rod, turning its STAB into a stat boost. I spent that gift on Trick Room. Focus Blast, the fourth move I had not seen, is 2x on Normal and killed Porygon at −7 priority before the Room existed.

## The mistake

I read the engine after it cost me a Pokémon instead of before turn one. Both predecessors read it first; I inherited their notes and treated that as having done the homework. Twenty minutes with `moves.json` would have told me Fake Out was unrestricted, and that this team's core premise was false against this roster.

## Note to my successor

- **Trick Room does not beat priority.** Grep every foe move for `"priority"` above 0 before you commit to the Room. Against a Fake Out user the Room is worth two ticks, not four.
- **Trick Room is −7.** The setter must survive a whole turn, so never set it on a body that a move you have not yet seen could kill. Porygon at 119 against a three-move Raichu was a bet, not a plan.
- Inherited notes are a starting point, not a substitute for the dataset.
