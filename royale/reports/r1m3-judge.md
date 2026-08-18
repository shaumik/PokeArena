# r1m3 — Deep Room vs The Apothecary

## Verdict

**The Apothecary wins, 2–0 on Pokémon remaining, on turn 39.** Deep Room was swept to zero; The Apothecary finished with Jynx (1%) and Raticate (88%, burned) still standing. It ended by attrition, not a sweep in the clean sense — of Deep Room's six, three died to residual status damage or to a hit that status had already halved the health of, and the last four bodies all fell to the same Zapdos. Turn 39 of a 300-turn cap, so no judge's decision was needed. Kills: The Apothecary took all six of Deep Room's; Deep Room took four of theirs (Arbok, Gengar, Golbat, and Zapdos to Poison Touch residual on the final turn).

## The story

Deep Room won the opening exchange outright: it read Jynx as a Lovely Kiss lead, pivoted to Insomnia Hypno on turn 1, and the sleep half of The Apothecary's archetype never landed a single time in thirty-nine turns. It then got four separate Trick Rooms up (turns 2, 7, 17 and 31) and cashed three of them — Rhydon killed Arbok from 40 Speed, Machamp took Zapdos to 40% with No Guard Stone Edges, Snorlax moved before a 163-Speed Raticate with a Choice Band. What it could not do was stop the *other* half. Zapdos threw four Toxics (turns 3, 9, 27, 31) and every single one landed on a Pokémon that had no way to remove it: Rhydon, Machamp, Snorlax and Hypno all died carrying the poison Zapdos put there. **The turning point is turn 27**, when Zapdos took a Toxic instead of an attack against a freshly-switched Choice Band Snorlax — Deep Room's real win condition — and then won a three-turn Roost-versus-Crunch war by exactly three HP a turn while the poison did the killing. Snorlax fled at 22% on turn 29 and died to its own toxic tick on turn 35 having converted nothing. The five turns of speed inversion kept arriving on schedule; the poison never stopped.

## Scorecard

- **Deep Room**: yes — it played the trick-room archetype about as literally as it can be played. Four rooms set, three of them converted into damage from a Pokémon that would otherwise have moved last, and it never once abandoned the plan to just click a fast attacker. **Best decision: turn 0**, switching to Hypno into a telegraphed Lovely Kiss. Insomnia blanked the lead, and The Apothecary never attempted sleep again for the rest of the match — one switch deleted a quarter of the opposing gameplan. **Worst decision: turn 9**, bringing Machamp in against a healthy Zapdos. Machamp holds an Assault Vest: no recovery, no status move, no answer to poison. It was Toxic'd on entry, landed three Stone Edges, and then sat on the bench at 16% for twenty-five turns before dying on turn 38 without ever getting an action off. Deep Room's best attacker was spent on three hits.
- **The Apothecary**: yes, and it adapted when the archetype misfired. Four Toxics, a Will-O-Wisp, a Confuse Ray, a Flame Orb self-burn and a correctly-timed Hex. Sleep was the one prescription it could never write. **Best decision: turn 27**, Toxic on the Choice Band Snorlax rather than attacking it — it correctly valued the clock over the 55 damage and then had the discipline to Roost three turns straight while losing the raw exchange. **Worst decision: turn 4**, switching Arbok into Rhydon. Arbok got its Intimidate off and was then a Poison-type standing in front of a 200-Attack Ground-type; it died on turn 5 without acting, the fastest death of the match, and Glare and Venoshock never appeared in the game at all. Honourable mention to turn 17's Raticate/Flame Orb setup — but that one is the engine's fault (see BUGS), and the agent detected the discrepancy from the damage numbers by turn 20 and abandoned the plan, which is excellent play.

## MVP

**Zapdos.** Four Toxics, four bodies: Rhydon (softened to 63% and finished by Jynx), Machamp (chipped from 100% to 16% by poison alone and never recovered), Snorlax (died to its own toxic tick on turn 35), Hypno (ticked down and Discharged out on turn 34). On top of that it landed the direct KO on Hypno, Muk, Machamp *and* Slowbro — the last four Pokémon Deep Room owned — and it did it while winning a Roost war at 3 HP a turn against a Choice Band. It finally died on turn 39 to Muk's Poison Touch residual, in the same end-of-turn phase in which its Discharge killed Slowbro and won the match.

## Notable turns

- **turn 1** — Hypno's Insomnia turns Lovely Kiss into "But it failed!". The Apothecary never attempts sleep again for the remaining 38 turns; the whole sleep axis of the archetype is deleted by one switch.
- **turn 12** — Golbat Roosts, sheds its Flying type, and Machamp's Stone Edge falls from 127 damage to 60. The room's best attacker loses its own matchup to a defensive move, and Deep Room pulls it out for good the next turn.
- **turns 18–20** — Raticate's Facade lands for 53 and 109 instead of the ~110 and ~220 both players expected. The Apothecary reads the numbers back mid-match ("Facade is NOT doubling off my burn here") and pivots off the plan on turn 20. Both agents had built a line on a mechanic the engine does not implement.
- **turn 27** — Zapdos Toxics the Choice Band Snorlax on the switch, then Roosts for 99 against Crunches of 96 and 87 for three turns. Deep Room's win condition never converts and dies of the poison eight turns later.
- **turn 39** — Zapdos, on 3 HP and badly poisoned by Muk's Poison Touch, Discharges the last Slowbro for exactly its remaining 140 HP, then faints to its own poison in the same residual phase. The match ends with both actives on the floor.

## BUGS

### 1. Facade does not double against the user's own status — CONFIRMED

**What I saw.** Turn 18: `Raticate used Facade! / Slowbro took 53 damage.` Turn 19: `Raticate used Facade! / Snorlax took 109 damage.` Raticate is burned (Flame Orb, turn 17) and has Guts.

**What I expected.** Facade's defining property is that its base power doubles from 70 to 140 when the user is burned, poisoned or paralysed. Both agents wrote it into their reasoning ("Facade doubles to 140"; "a doubled Facade OHKOs Hypno, Muk…").

**What the engine says.** `internal/engine/callbackmoves.go:257` defines `statusDoublingMoves` with exactly two entries, `hex` and `venoshock`; `applyCallbackPower` (line 282) is the only place base power is rewritten dynamically. `grep -rn "facade" --include=*.go internal/` returns **nothing** — Facade is not referenced anywhere in the engine. `data/moves.json` carries it as a flat 70-BP Normal physical move with no marker, and the upstream dump `tools/data-sync/upstream/moves.json` has no callback field either, so nothing downstream could pick it up.

**Arithmetic confirming it.** Raticate Atk 133, burned (×0.5 in `offensiveDefensiveStats`), Guts ×3.0 (`abilities.go:1226` — 1.5 boost × 2.0 burn-cancel), STAB ×1.5.
- vs Slowbro (Def 178) at 70 BP: 51–60 damage. At 140 BP: 95–112. **Observed 53.**
- vs Snorlax (Def 86) at 70 BP: 98–116. At 140 BP: 197–232. **Observed 109.**

Both land squarely in the 70-BP band. Guts is firing correctly (without it the numbers would be ~17–20 and ~33–39); it is specifically the status-doubling that is absent. Note the second one matters: at canon power that Facade takes a 267-HP Snorlax to roughly 35–70 HP instead of 158, which changes the turn-19 through turn-27 sequence materially. **Verdict: CONFIRMED.** Fix is a one-line addition to `statusDoublingMoves` — `"facade": func(atk)` keyed on the *attacker's* status rather than the defender's, which means the map's `func(def *Pokemon) bool` signature needs widening to take the attacker too.

### 2. A secondary status lands on a target the same hit reduced to 0 HP — CONFIRMED (minor)

**What I saw.** Turn 39: `Zapdos used Discharge! / Slowbro took 140 damage. / It's super effective! / Slowbro was paralyzed! / Slowbro fainted!` Slowbro was on exactly 140 HP, so the damage was clamped to leave it at 0 before Discharge's 30% paralysis secondary rolled.

**What I expected.** Showdown's `Pokemon#setStatus` opens with `if (!this.hp) return false;` — a Pokémon at 0 HP cannot be statused, so the secondary should be refused and no paralysis line should print.

**What the engine says.** `turn.go:864–880` deliberately keeps a "faint window": a killed Pokémon has `HP == 0` but `Fainted == false` until after `applyDamageEffects` runs, and the file comment explicitly says every site inside that window "must test the HP, not the flag". `applyDamageEffects` (`effects.go:227`) and `inflictStatus` (`effects.go:503`) both test `p.Fainted`, not `p.HP <= 0`, so a 0-HP target is still a legal status recipient.

**Severity.** Cosmetic in this match — `faint()` clears the status immediately after. But the guard is genuinely load-bearing elsewhere in the same window: a dying target with Synchronize would bounce the status onto the attacker via `applySynchronize`, and `applyItemStatusCure` would burn a Lum/Chesto Berry on a corpse. **Verdict: CONFIRMED**, low severity. Fix is changing the `Fainted` checks in `applyDamageEffects`/`inflictStatus` to the existing `isDown()` predicate the file comment already points at.

### 3. Toxic counter is not reset when the Pokémon switches out — NOT-A-BUG (documented intentional deviation, but it decided real turns)

**What I saw.** Machamp was Toxic'd on turn 9, ticked 12 / 24 / 36 / 49, and switched out on turn 13 with the counter at 5. It sat on the bench for 24 turns, returned on turn 37, and Deep Room's own reasoning read "Machamp has a 61-point toxic tick against 31 HP — it gets exactly one action in this match, ever" (5/16 × 197 = 61). Same story for Snorlax, pulled at 22% on turn 29 with the counter at 4 and killed by a 59-point clamped tick on turn 35.

**What I expected.** In every generation from 3 onward, Toxic's counter resets when the badly-poisoned Pokémon leaves the field; switching out is the standard way to reset the clock to 1/16.

**What the engine says.** `doSwitchWithCarry` (`switching.go:44–56`) resets `Stages`, `Volatiles` and `SleepTurns` on the outgoing Pokémon but not `ToxicCounter`. Every writer to that field is accounted for: `faint`, `clearStatus`, Healing Wish, and the initial set to 1 — none of them is a switch. And `docs/battle-state.md:179` states the rule outright: *"Both reset to zero when the status is cleared or when the Pokémon switches out (for SleepTurns). ToxicCounter resets only when the Toxic status itself is cleared."*

**Verdict: NOT-A-BUG** — the engine is behaving as designed and documented. I raise it anyway because it is a real divergence from canon that shaped this specific match: it is why Machamp could never be rehabilitated by a pivot and why Snorlax died on the bench clock. Both agents played around it correctly, so it did not create an unfair asymmetry here, but the organiser should know the deviation is live and load-bearing.

### Everything else checked clean

I audited every damage number, residual and ordering decision across all 39 turns against the formula in `damage.go` and the datasets. All of the following matched to the exact roll:

- **Trick Room duration.** Four rooms, set turns 2, 7, 17 and 31, each expiring at the end of turns 6, 11, 21 and 35 respectively. `TurnsLeft` starts at 5 and `tickPseudoWeather` decrements after residuals on the setting turn, giving exactly five active turns each time. No early or late expiry.
- **Trick Room ordering.** Inversion verified on turns 4, 5, 6, 10, 11, 19, 20, 21, 32, 33, 34, 35 (Rhydon at 40 Speed before Arbok at 100 and Jynx at 161; Machamp at 54 before Zapdos at 120; Snorlax at 31 before Raticate at 163 and Gengar at 178). Order flipped back correctly on every turn the room was down (12, 14, 15, 23, 24, 36–39). `goesFirst` (`turn.go:351`) inverts only the speed comparison, after the priority and item checks.
- **Priority still beats the room.** Turn 7: Jynx's Ice Beam (priority 0) resolved before Hypno's Trick Room (priority −7 in `data/moves.json`) even though Jynx was the faster Pokémon and would have moved second on speed. Switches also resolved ahead of the room on turns 8, 17, 31.
- **Sleep Clause.** Never exercised — Lovely Kiss failed to Insomnia on turn 1 and was never selected again. `sleepClauseBlocks` (`clauses.go`) is wired into the foe-induced path `inflictStatusFrom` and looks correct on inspection, but this match provides no live evidence either way.
- **Guts.** Turn 18/19 Facade numbers match the ×3.0 multiplier exactly (1.5 status boost × 2.0 to cancel the burn halve baked into `computeDamage`). Guts is ignoring the burn's Attack cut correctly.
- **Hex.** Turn 24: 141 into an unstatused Slowbro. That is the undoubled 65 BP figure (the 130-BP band would be 275–324). Correctly refused to double against a clean target.
- **Venoshock** never appeared (Arbok died turn 5). **Dynamic Punch** never resolved (Machamp was KO'd before acting on turn 38).
- **No Guard.** Machamp's 80%-accurate Stone Edge landed 3/3 (turns 10, 11, 12) and `resolveAccuracy` (`turn.go:1160`) short-circuits before the accuracy roll for either combatant holding it.
- **Intimidate** fired exactly once, on Arbok's turn-4 entry, and the −1 is visible in the turn-5 Earthquake (167 damage matches Atk 200 × 2/3).
- **Static** never fired, correctly: every hit Zapdos took was Stone Edge or Seismic Toss — Stone Edge carries no `contact` flag. **Poison Touch** fired on turn 36 (Ice Punch, contact, 30%), poisoning Zapdos, and the 197/8 = 24 residual was exact.
- **Paralysis.** Body Slam paralysed Golbat turn 20; Golbat's Speed went from 110 to 55 (`effectiveSpeed` halve) and it stayed slower than everything for the rest of its life. No full-paralysis roll ever came up in the handful of turns it had.
- **Burn.** Raticate 8/turn off 131, Muk 13/turn off 212 — both 1/16 exactly. Muk's burned Ice Punch and Knock Off both show the ×0.5 physical cut.
- **Toxic escalation.** Rhydon 13/26/39 off 212; Machamp 12/24/36/49 off 197; Snorlax 16/33/50/59(clamped) off 267; Hypno 12/24/36 off 192. Every value is `MaxHP × n / 16` floored.
- **Type chart.** Normal→Ghost 0 (turn 21), Ghost→Normal 0 (turn 25), Ground→Poison 2, Rock→Flying 2, Bug→Psychic 2, Dark→Ghost 2, Ice→Flying 2, Electric→Water 2, Poison→Poison 0.5. All agree with `data/typechart.json`.
- **Items.** Focus Sash clamped Megahorn to 140 on a 141-HP Jynx; Sitrus fired at the 50% threshold for both Golbat (+45) and Hypno (+48); Leftovers +12 on Zapdos every turn; Black Sludge +13 on Poison-type Muk; Life Orb ×1.3 with 13 recoil; Assault Vest ×1.5 SpD (Discharge 45 into Machamp); Choice Band ×1.5 Atk *and* the lock, which is why a Body-Slam-locked Snorlax was helpless against Gengar on turn 21; Rocky Helmet never triggered because nothing made contact with Arbok.
- **Abilities.** Regenerator +41 (capped) and +67 (=202/3) on Slowbro's two switch-outs; Cursed Body disabled Knock Off on turn 23; Thick Fat irrelevant to every hit Snorlax took; Infiltrator never mattered (no screens).
- **Roost.** Both users' 50% heals used `math.Round` correctly (Golbat 91→82 capped, Zapdos 98.5→99), and the Flying-type suppression is live — Machamp's Stone Edge dropped from 127 to 60 across turns 11 and 12 as `roostTypes` stripped Golbat's Flying half.
- **Seismic Toss** dealt a flat 50 three times into a Flying-type that resists Fighting, which is correct: `fixed-damage-level` short-circuits the formula but keeps the immunity check.
- **Confusion.** Turn 14 self-hit of 29 matches the 40-BP typeless calculation off Muk's own Atk/Def. Turn 15's flinch was checked before the confusion tick and consumed the turn without decrementing the confusion counter — which is canonical ordering.
- **Engine test suite** passes clean: `go test ./internal/engine/` → `ok, 7.4s`.

No Pokémon moved twice, failed to move without a stated cause, or moved out of the order the speed rules dictate. No HP changed without a logged reason.
