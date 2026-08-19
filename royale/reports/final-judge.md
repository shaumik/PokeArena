# The Final — Guillotine Club vs The Low Ceiling
**Referee report** · match `final` · seed 3607 · turn cap 60 · 21 turns played · 29 resolutions

## Verdict

**Guillotine Club wins, 3–0.** Persian, Raichu and an untouched Dodrio survive; The Low Ceiling is wiped.

The Low Ceiling built both of its Trick Rooms exactly as designed, got both of them up on schedule, and lost anyway — because the room it built inverts speed and does not touch the priority bracket, and the fastest team in the tournament brought a cat holding a +3 move. Guillotine Club's stated creed ("one free turn is a kill") turned out to be literally true in the only direction that mattered: they spent five turns *not* attacking, and that is what won it.

The audit found **one genuine, match-deciding engine defect** (Fake Out has no first-turn-out restriction — see BUGS), and cleared everything else it touched. All three of the newly landed patches behave correctly; the Trace fix in particular got a clean live exercise and passed.

## The story

Mr. Mime walked out first and never got to be a Pokémon — only an architect. Pinsir's X-Scissor took 140 off it on turn 1 and the Mime, seven hit points from the grave, still stood up and twisted the dimensions before it died. That is the whole Low Ceiling thesis in one turn: I don't need to live, I need the ceiling to exist.

It worked. Marowak came in under the room and the room made a 45-Speed bone-club the fastest thing in the building. It squashed Pinsir on turn 3. Hitmonlee came in on turn 5 deliberately eating an Earthquake to break its own Focus Sash — a beautiful piece of play, arming Unburden off the corpse of its own item — and got exactly one turn out of it, a 105-damage High Jump Kick, before Double-Edge collected it on turn 6. Two rooms' worth of Guillotine Club was already dead. Through turn 6 the Low Ceiling was winning the final.

Then Persian came back out.

Persian had already thrown one Fake Out on turn 4 and one on turn 7 — both perfectly legal, both after a fresh switch-in, both landing ahead of Trick Room because priority +3 sits above the bracket the room inverts. It U-turned into Scyther, Scyther traded two banded Aerial Aces into Cloyster and died to Skill Link's Icicle Spear (two hits, 48 and 44, exactly Scyther's remaining 92), and Persian returned on turn 10 to slap a 20-HP Cloyster off the board on turn 11.

Porygon entered, Traced Technician, and Persian knocked its Sitrus Berry into the sea. On turn 12 Porygon built Room Two — the good one, the one with four live bodies behind it. Persian answered by punching Machamp with Play Rough on turn 13, and Machamp, burning off its own Flame Orb with Guts loaded, came out at 10 HP with Close Combat aimed at a cat.

It never swung. Turn 14: Fake Out. Turn 15: Fake Out. Turn 16: Fake Out. Persian had been standing on the field since turn 10 and had already attacked three times with other moves, and the engine let it throw a +3 flinch move every single turn anyway. Machamp died without acting. Omastar was frozen through the last two ticks of Room Two, eating 18 and 20 damage a turn while its Hydro Pumps evaporated. Room Two produced zero kills. Both trainers independently worked out what had happened — p1 on turn 13, p2 on turn 13 as well, one of them delighted and one of them writing "I read the engine after it cost me a Pokémon instead of before turn one."

After that it was tidying. Persian U-turned out at full health — it finished the tournament final without ever being damaged once — and handed the mouse a normal-speed board. Raichu drowned Marowak with Surf, punched Focus Blast through the Lightning Rod Porygon had just Traced off it, and Thunderbolted Omastar for exactly its remaining 112. Dodrio never left the bench.

Six blades, three of which never bled, and the sharpest one was a slap.

## Scorecard

| Guillotine Club | Fate | KOs |
|---|---|---|
| Pinsir (Moxie / Life Orb) | Fainted T3 — Marowak Double-Edge | 1 (Mr. Mime) |
| Scyther (Technician / Choice Band) | Fainted T10 — Icicle Spear | 0 (137 chip into Cloyster) |
| Dodrio (Early Bird / Choice Scarf) | **Never entered** | 0 |
| Persian (Technician / Silk Scarf) | **Survived, 100% HP, never damaged** | 2 (Cloyster, Machamp) |
| Raichu (Lightning Rod / Expert Belt) | **Survived, 100% HP** | 3 (Marowak, Porygon, Omastar) |
| Hitmonlee (Unburden / Focus Sash) | Fainted T6 — Double-Edge | 0 (105 into Marowak) |

| The Low Ceiling | Fate | KOs |
|---|---|---|
| Mr. Mime (Filter / Mental Herb) | Fainted T2 — set Trick Room #1 first | 0 |
| Porygon (Trace / Sitrus) | Fainted T20 — set Trick Room #2 | 0 |
| Marowak (Rock Head / Thick Club) | Fainted T19 — Surf | 2 (Pinsir, Hitmonlee) |
| Cloyster (Skill Link / King's Rock) | Fainted T11 — Fake Out | 1 (Scyther) |
| Machamp (Guts / Flame Orb) | Fainted T14 — Fake Out, never acted after T8 | 0 |
| Omastar (Shell Armor / Weakness Policy) | Fainted T21 — Thunderbolt | 0 |

Trick Room uptime: turns 1–5 and 12–16 (five ticks each, setting turn inclusive, both correct). Kills scored *under* Trick Room: Low Ceiling 2 (both in Room One), Guillotine Club 2 (both Fake Outs, in Room Two).

## MVP

**Persian.** Two KOs, one Knock Off, five turns of denial, and it finished the final on 100% HP having never taken a point of damage. It is also the instrument of the bug, which complicates the trophy but not the arithmetic: it flinched a Guts Machamp to death and locked an Omastar out of the last two turns of the only Trick Room that had a team behind it.

Honourable mention to **Marowak**, which killed two Pokémon in four turns off a doubled Attack and never took recoil for it, and to **Mr. Mime**, which died on turn 2 having already done its entire job.

**Best decision of the match:** Guillotine Club, turn 4 → 5, sending Hitmonlee in *to* an Earthquake to break its own Focus Sash and arm Unburden. It bought one turn and cost a Pokémon, and it was still right.

**Worst:** The Low Ceiling, turn 12, spending Porygon's free window on Room Two without having checked what priority does inside it.

## Notable turns

- **Turn 1** — Mr. Mime survives X-Scissor on 7 HP (Bug into Psychic/Fairy nets to 1×, so Filter never engages) and sets Room One before dying to the identical move on turn 2.
- **Turn 5** — Hitmonlee switches in to eat Earthquake on purpose. Focus Sash holds at 1 HP, Unburden arms, and Trick Room expires in the same log block — exactly five turns after it was set.
- **Turn 6** — 1 HP and 305 Speed. High Jump Kick takes 105 off Marowak; Double-Edge takes the last hit point back.
- **Turn 8** — Persian U-turns for 18 into Machamp. Technician correctly *declines* to boost a 70-BP move (a boost would have shown ~24–28); the same Persian's 40-BP Fake Out is boosted. Clean live proof of the ≤60 gate.
- **Turn 8** — Machamp's Close Combat lands 54 on a Bug/Flying at 0.25×. Backing that number out gives Attack 200 × Guts 1.5 with **no** burn halving — Guts is applied once and the burn cut is not double-counted.
- **Turn 10** — Skill Link's Icicle Spear stops at two hits because Scyther dies on the second (48 + 44 = 92, Scyther's exact remaining HP). Correct: the multi-hit loop terminates on faint, and King's Rock does not roll a flinch into a corpse.
- **Turn 12** — Knock Off removes Porygon's Sitrus Berry at 53 damage. Backed out, that requires the Gen-6 ×1.5 item-removal boost; without it the ceiling is 40.
- **Turns 14–16** — the match. Three consecutive Fake Outs from a Pokémon that had been on the field since turn 10. See BUGS.
- **Turn 19** — Porygon re-enters and Traces **Lightning Rod**, not the Technician it copied on its first entry. The patched revert works.
- **Turn 20** — Raichu fires Focus Blast (70% accuracy) into a Porygon holding a freshly-Traced Lightning Rod, hits, and kills it before it can build Room Three.

## BUGS

**CONFIRMED — Fake Out has no first-turn-out restriction; it is usable every turn, forever.**
Persian entered on the turn-10 replacement and then used Fake Out on turn 11, Knock Off on turn 12, Play Rough on turn 13, and Fake Out again on turns 14, 15 and 16 — its fifth, sixth and seventh consecutive turns on the field. Each one dealt damage and applied the 100% flinch (turns 15 and 16 both log "Omastar flinched and couldn't move!"). Canon (Showdown `moves.ts`, Fake Out `onTry`) fails the move unless it is the user's first action since entering.

Proof from source, which is a proof by *absence* and so needs three parts:

1. The move is pure data with no restriction flag: `data/moves.json:8399`-region entry for `fake-out` is exactly `{power:40, accuracy:100, pp:10, priority:3, flags:["contact"], secondaries:[{chance:100, volatile:"flinch"}]}`.
2. There is no field that could carry the restriction. `internal/domain/domain.go:231` `type Move struct` has no first-turn / once-per-entry member (it has `MinHits`, `OHKO`, `ThawsTarget`, `IgnoreEvasion`, `IgnoreDefensive`, stat overrides — nothing here).
3. There is no engine code for it and no state it could read. `grep -ri fake internal/engine/` returns **zero** hits. `internal/engine/turn.go:420` `executeMove` gates on recharge, `canAct`, choice lock, charging and locked moves — never on "how long have I been out". The `Volatiles` struct (`internal/engine/battle.go`, ~line 200 onward) has `MovedThisTurn`, `MovedLast`, `LastMoveID`, `Unburden` and forty other fields, but nothing counting turns or actions since switch-in, so the gate could not be written today without new state. `internal/engine/callbackmoves.go` special-cases only `hex`, `venoshock` and `facade` by ID.

Impact: decisive. Two of Guillotine Club's five KOs came off illegal Fake Outs, and the second Trick Room — the one with four live Pokémon behind it — produced no kills at all because of them. Both trainers independently diagnosed it mid-match (`royale/reports/final-p1.md:13`, `royale/reports/final-p2.md:13`). Same defect would apply to First Impression if it were ever in a roster.

---

**NEW-FIX VERIFICATION — Trace reverts on switch-out: CONFIRMED, live and in source.**
Porygon entered twice. Entry 1 (turn 11 replacement, resolution #16) against Persian: *"Porygon's Trace copied Persian's Technician!"* It switched out on turn 13 (resolution #18) and re-entered on turn 19 (resolution #26) against Raichu: *"Porygon's Trace copied Raichu's Lightning Rod!"* — a **new** ability, not a lock to the first copy. Source matches: `internal/engine/abilities.go:396-399` stores the original into `p.BaseAbility` before overwriting, and `internal/engine/switching.go:50-53` restores `out.Ability = out.BaseAbility` and clears the slot, sited before `out.Stages`/`out.Volatiles` are reset and after `applyOnSwitchOut`, so no later hook observes the borrowed ability. `AbilityRevealed` is deliberately not cleared (`switching.go:46-49` comment), matching the stated intent. The fix holds.

**NEW-FIX VERIFICATION — Effect Spore respects powder immunity: source-verified only, NOT exercised live.**
Neither finalist carries Effect Spore (rosters: Moxie / Technician / Early Bird / Technician / Lightning Rod / Unburden vs Filter / Trace / Rock Head / Skill Link / Guts / Shell Armor), so I am explicitly **not** claiming a live pass. In source the fix is correct and correctly sited: `internal/engine/items.go:1050-1064` `powderImmuneBy` holds all three immunities (Grass type, Overcoat with a `nil` mold-breaker so Mold Breaker cannot punch through an ability rider, and any `BlocksPowder` item), and `internal/engine/abilities.go:854-871` calls it **after** both draws — `rng.Chance(30)` on line 855 and `rng.IntN(3)` on line 862 — so the RNG stream is unchanged whether or not the target is immune. That is the property that matters for replay determinism and it is satisfied.

**NEW-FIX VERIFICATION — five abilities regrouped from "hook-free but fully functional" to "recognized but inert": CONFIRMED documentation-only.**
`internal/engine/abilities.go:265-292` now lists `harvest`, `unnerve`, `neutralizing-gas`, `forewarn`, `illuminate`, `run-away`, `healer` under "recognized but inert", each with the reason it is blocked; `abilities.go:314-332` narrows "hook-free but fully functional" to `sticky-hold` and `klutz` and names the incident that motivated the split. Neutralizing Gas is registered as `{Kind: "neutralizing-gas"}` with no hooks and is honestly documented as needing a state-aware `abilityOf` (line 278-280); it is also excluded from `abilityTraceable` at `abilities.go:1643-1646`. Every entry carries only `Kind`, so all dispatchers no-op exactly as before — zero behavioural change, which is what was claimed. `TestInertAbilitiesAreFiledAsInert` guards the two lists against drifting apart. None of these abilities appears on either finalist, so again: source-verified, not live-exercised.

---

**NOT-A-BUG — Trick Room duration and re-set semantics.** Room One set turn 1, "The twisted dimensions returned to normal!" at the end of turn 5. Room Two set turn 12, ended end of turn 16. Five ticks each, setting turn inclusive. `internal/engine/pseudoweather.go:59` `defaultPseudoWeatherTurns = 5`, set at `:98`, decremented once per turn at `:161-167`. Trick-Room-into-Trick-Room correctly *clears* rather than refreshing (`pseudoweather.go:89-97`) — untested live, both setters were dead or one-shot, but the code is right and is distinguished from Gravity, which correctly does fail instead (`:141-147`).

**NOT-A-BUG — Trick Room vs the priority bracket.** Checked every turn of both rooms. `internal/engine/turn.go:337-366` `goesFirst` compares priority first and returns before ever consulting `trickRoomActive`, which only inverts the *speed* comparison at `:362-365` on the fully modified figure from `effectiveSpeed × sideSpeedMult`. Live: Persian's +3 Fake Out beat Marowak (turn 4), Machamp's Close Combat (turn 14), Cloyster's +1 Ice Shard (turn 11) and Omastar's Hydro Pump (turns 15, 16), all under an active room. Trick Room itself is priority −7 in `data/moves.json` and resolved last in both of its own turns (turn 1 after Pinsir, turn 12 after Persian's Knock Off). Speed ties still break on RNG under the room, which is correct.

**NOT-A-BUG — Technician's ≤60 gate.** `internal/engine/abilities.go:548-556`: `if m.Power > 0 && m.Power <= 60 { return 1.5 }`. Live confirmation in both directions on the same Pokémon on adjacent turns: Fake Out (40) boosted, Aerial Ace (60) boosted on Scyther, U-turn (70) not boosted (turn 8, 18 damage into Machamp — a boost would have put it at 24–28), Knock Off (65) not boosted, Play Rough (90) not boosted.

**NOT-A-BUG — damage formula and rounding at every modifier boundary.** I back-computed six hits by hand against the level-50 stat lines and the Showdown chain. Every one landed inside its legal roll band, and two landed on values that are *only* reachable with Showdown's exact round-half-down: turn 4 Fake Out into Marowak, 40 damage, needs `applyMod(22, 7373) = 40` where a round-half-up would give 41; turn 7 Fake Out into Machamp, 59, needs `applyMod(33, 7373) = 59` where round-half-up gives 60. `internal/engine/damage.go:637` `applyMod(v, mod) = (v*mod + 2048 - 1) >> 12` is precisely Showdown's `tr((value*numerator + 2048 - 1)/4096)`; `chainMod` at `:622` uses the +2048 round-half-up that canon uses for *chaining*, which is the correct asymmetry. Type effectiveness is applied as integer doublings/halvings (`:651-664`) before the final modifier group, also canon.

**NOT-A-BUG — the lumped ability/item modifier group.** `internal/engine/damage.go:333-345` documents openly that Technician and the type-boost items are final-group multipliers here where canon makes them base-power handlers. This is a self-declared fidelity gap with a written rationale, not an undocumented defect, and every number I checked in this match was reachable under either placement. Naming it for completeness; not filing it.

**NOT-A-BUG — Guts + Flame Orb + Facade, no double-count of the burn.** Machamp's turn-8 Close Combat backs out to Attack 200 × 1.5 with the burn's halving suppressed. A double-count would have produced roughly half the observed 54. Machamp never actually clicked Facade (it got two turns in the whole match and used Close Combat both times), so the Facade-off-own-status path was not exercised live.

**NOT-A-BUG — Rock Head cancels Double-Edge recoil.** Marowak used Double-Edge twice (turns 3, 6) and took no recoil line either time.

**NOT-A-BUG — Life Orb.** Pinsir's 140-damage X-Scissor into Mr. Mime requires the ×1.3, and the recoil was 14 on a 141 HP Pinsir (1/10, truncated) both times.

**NOT-A-BUG — Moxie.** Exactly one boost, exactly one pair of log lines, on exactly the KO (turn 2). Never fired on a non-KO.

**NOT-A-BUG — Unburden.** `internal/engine/abilities.go:715-735` doubles Speed only while `Volatiles.Unburden && p.Item == ItemNone`, armed in `items.go:533/554` on genuine consumption. Hitmonlee's Focus Sash was genuinely consumed on turn 5 and it moved first on turn 6 — though that is not a decisive test, since Hitmonlee at 152 already outran Marowak's 45 with Trick Room down. Source is correct; live evidence is consistent but not probative. Worth recording the strategic note: Unburden on a Trick Room team would be actively harmful, and this engine would faithfully punish it.

**NOT-A-BUG — Skill Link, King's Rock, multi-hit and the faint window.** Icicle Spear terminated at hit 2 because the target died on hit 2 (48 + 44 = Scyther's exact remaining 92, the second hit clamped to remaining HP). `internal/engine/items_reactive.go` `flinchItem` guards on `def.Fainted || def.HP <= 0` *and* on `abilityBlocksSecondaries` / `itemBlocksSecondaries` before its 10% roll, and the roll is per hit. No flinch was rolled into a corpse.

**NOT-A-BUG — Knock Off.** Removed Porygon's Sitrus Berry and dealt 53, which requires the Gen-6 ×1.5 removal boost (unboosted band tops out at 40). It never got aimed at a Thick Club, Flame Orb or Mental Herb — Marowak and Mr. Mime were never in front of a Knock Off user, and p2's own note on turn 12 observes that an already-triggered Flame Orb is nothing to steal. Those three specific interactions are **untested this match**.

**NOT-A-BUG — Weakness Policy did not fire.** Omastar was hit by exactly one super-effective move all match, the turn-21 Thunderbolt for 112 into 112 HP. `internal/engine/items_reactive.go:118-126` guards `res.Effectiveness <= 1 || def.HP <= 0`, so a lethal SE hit correctly produces no boost. Untested in the surviving case.

**NOT-A-BUG — Fake Out's flinch on a switching target.** Turn 7: Machamp switched in and ate Fake Out with no "flinched" line. Correct — the flinch volatile is set but Machamp's action for the turn was the switch, so there is no move for it to interrupt and nothing to announce. `switching.go` sets `in.Volatiles.MovedThisTurn = true` on entry.

**NOT-A-BUG — no stall.** Every `log -wait` returned promptly; 29 resolutions across 21 turns with no gap requiring a `status` probe. Both sides submitted on every choosing and replace phase.

**NOT OBSERVED / UNTESTED THIS MATCH** (listing so the coverage gap is on the record rather than implied to be clean): Mental Herb (Mr. Mime died turn 2, never taunted or encored); Lightning Rod redirection or absorption (no Electric move was ever aimed at Raichu, and Porygon's Traced copy was killed by Focus Blast before it mattered); Expert Belt's ×1.2 (all three of Raichu's super-effective hits were clamped to the target's remaining HP — 22, 119, 112 — so the multiplier is unmeasurable from this log); Choice locking on Dodrio (never entered) and on Scyther (it chose Aerial Ace twice, consistent with a lock but equally consistent with free choice); sleep, freeze, Toxic, weather, hazards, screens, Protect, and Substitute (none appeared); PP exhaustion (Persian's seven Fake Outs stayed inside 10 PP; `turn.go:1038` decrements, but nothing hit zero).
