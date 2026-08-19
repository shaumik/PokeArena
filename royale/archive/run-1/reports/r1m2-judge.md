# r1m2 — Spike Cartel vs Solaris

## Verdict

**Solaris wins, 3–0 in Pokémon remaining, on turn 18.** Attrition that started as a
turn-1 execution: Spike Cartel lost Omastar before it ever moved and never recovered
the tempo. Solaris finished with Venusaur (78%), Exeggutor (61%) and Charizard (34%)
still on the bench; Spike Cartel lost all six. Not a clean sweep — Solaris paid
Ninetales and Arcanine and gave Victreebel away — but never trailed.

## The story

The match was decided on turn 1 and everyone knew it: Ninetales led, Drought lit the
sky, and a one-turn Solar Beam went 4x into Omastar for exactly its 177 max HP —
the hazard setter died *mid-cast*, and Spike Cartel spent the remaining seventeen
turns fighting on a bare floor it had built an entire roster to carpet. The Cartel's
answer was correct in principle: Golbat's Toxic on turn 2 put the weather engine on a
five-turn clock and guaranteed the sun would only ever be lit once. What it could not
answer was that Ninetales, already dying, converted its last four turns into
sun-boosted Fire Blasts that took Golbat to 38%, Muk to 29% and Rhydon to 51% before
the poison collected — a one-for-two-and-a-half trade the Cartel never made back.
The turn Spike Cartel actually threw the match away was **turn 4**: Muk stayed in to
Knock Off the Heat Rock, but weather duration is fixed at set time, so the eight turns
were already banked and the item removal bought literally nothing while costing Muk
its life. From turn 9 the sun was gone and Solaris simply out-statted a two-body
Cartel with plain STAB attacks.

## Scorecard

- **Spike Cartel**: **No — it never played its archetype at all.** The board reads
  `hazards none` on all 26 resolutions. Stealth Rock died on the cast; Cloyster, which
  carries both Spikes and Toxic Spikes, entered on turn 14 and chose Icicle Spear
  instead; Rhydon's Dragon Tail and Golbat's Whirlwind therefore had nothing to phaze
  foes *into*. It became a generic bulky-offense team by turn 6 and said so out loud
  on turn 13 ("No time for theme now").
  - **Best decision — turn 2, Golbat's Toxic on Ninetales.** Killing the Drought setter
    permanently capped the sun at a single cycle, which is why Charizard's Solar Power
    never fired once all match.
  - **Worst decision — turn 3→4, Muk in on Knock Off.** Two turns and a Pokémon spent
    removing a Heat Rock whose eight turns were already paid out. The correct play was
    a pivot; the Cartel instead fed Muk to a Fire Blast it knew was coming.
- **Solaris**: **Yes, for eight turns, then it stopped needing to.** Drought fired on
  the lead, Solar Beam skipped its charge, Fire Blast ran at 1.5x for four consecutive
  turns, Chlorophyll Venusaur ran at 264 speed and outran everything, and Growth banked
  +2/+2 off the sun's last ray on turn 8. The cash-out was real. It then won the back
  half entirely off-sun — Solar Power was pure roster decoration (Charizard entered on
  turn 9, one turn after the sun expired).
  - **Best decision — turn 1, Solar Beam over Fire Blast.** Reading Omastar as the
    hazard lead and taking the 4x line instead of the STAB line deleted the opposing
    game plan on the first action of the match.
  - **Worst decision — turn 13, switching Arcanine into a live Rhydon.** A Fire-type
    sent in against a Pokémon whose Stone Edge had been telegraphed for two turns;
    Intimidate did not matter, and Arcanine died for its full 166 HP without moving.
    Solaris admitted it in its own next line ("Costly lesson").

## MVP

**Ninetales.** It set the weather, deleted the enemy hazard setter before that setter
got a single action, and then — already on a toxic clock it could not escape — spent
every one of its last four turns on sun-boosted Fire Blasts, chipping three separate
Cartel bodies (Golbat 100→38, Muk 100→29, Rhydon 100→51) before dying to its own
poison on turn 5. Its Heat Rock sun outlived it by three full turns, which is what
Venusaur's turn-8 +2 Growth was paid for with.

## Notable turns

- **turn 1** — Drought → "took in sunlight" → Solar Beam, all in one action. 177 damage
  into Omastar's 177 max HP. Stealth Rock never resolves; the Cartel's archetype dies
  in the first three log lines.
- **turn 5** — Ninetales' fourth Fire Blast lands on the freshly-switched Rhydon, then
  toxic (tick 4) collects the last 16 HP. The trade is complete: one Ninetales for
  Omastar plus most of three other health bars.
- **turn 8** — The sun runs out exactly as Venusaur banks +2 Atk / +2 SpA off Growth's
  sun clause, while Golbat Roosts back 91. Both sides get what they wanted; the sky
  goes dark for good.
- **turn 9** — Golbat's Whirlwind erases the +2 Growth one turn after it was set and
  drags Charizard in. Spike Cartel's single best turn, and the only one where the
  phazing half of the archetype actually did work.
- **turn 15** — Rocky Helmet's 26 chip breaks Victreebel off full HP, which quietly
  disarms its Focus Sash; Cloyster's Skill Link Icicle Spear then only needs 3 of its
  5 hits (48 + 44 + 38 = exactly 130) to finish it.

## BUGS

Six things looked wrong enough to chase. All six resolved to correct mechanics. The
match ran clean.

**1. Air Slash dealt 86 to Gengar (turn 11) — above the Showdown maximum roll.**
- *Saw*: `Charizard used Air Slash! / Gengar took 86 damage.` (no crit line).
- *Expected*: Charizard SpA 161, Gengar SpD 95, Air Slash 75 BP, STAB 1.5, neutral.
  Showdown's integer chain gives `floor(floor(floor(22·75·161/95)/50)+2) = 57`,
  `×1.5 STAB → 85`, so max roll is 85, not 86.
- *Checked*: `internal/engine/damage.go:275` computes
  `base := (float64(2*Level)/5.0+2.0)*float64(power)*a/d/50.0 + 2.0` as an unrounded
  float and applies a single `math.Floor` after every modifier
  (`dmg := int(math.Floor(base * stab * eff * critMult * randMult * ...))`). Carrying
  the fraction: `57.926 × 1.5 = 86.889 → 86`. This is deliberate and documented in the
  function comment as the "Gen-3+ damage formula"; it is not Showdown's intermediate-
  flooring chain, so engine rolls can sit 1–2 points above the cartridge maximum. It
  is internally consistent and affected no KO threshold in this match, but the
  organiser should know it is a fidelity deviation, not a per-hit accident.
- *Verdict*: **NOT-A-BUG** (by design). Flagged for awareness only.

**2. Several damage numbers fell below the 0.85 minimum roll.**
- *Saw*: turn 12 `Gengar took 58 damage` (min roll 73); turn 15 third Icicle Spear hit
  `Victreebel took 38 damage` (min roll 43); turn 5 `Ninetales is hurt by its toxic!
  (-16)` where tick 4 of toxic on 149 max HP should be 37.
- *Expected*: values inside the 85–100% band / the stated residual fraction.
- *Checked*: in every case the number equals the target's *exact remaining HP*
  (Gengar 58, Victreebel 38, Ninetales 16). The engine logs damage actually applied,
  clamped to remaining HP, the same convention that produced turn 1's `Omastar took
  177 damage` (177 = Omastar's full max HP against a ~480 raw hit). Consistent
  throughout.
- *Verdict*: **NOT-A-BUG.**

**3. Skill Link Cloyster's Icicle Spear hit only 3 times (turn 15).**
- *Saw*: three hits, then `Hit 3 time(s)!`, with Cloyster running `skill-link`.
- *Expected*: Skill Link forces the maximum 5 strikes.
- *Checked*: `internal/engine/abilities.go:1176` sets `MaxesMultihit: true` for
  skill-link, and the strike loop at `internal/engine/turn.go:785` breaks on
  `s.Active(1-side).HP <= 0`. Victreebel was at 130 after Rocky Helmet and took
  48+44+38 = 130; the loop terminated on the KO, not on a hit-count roll.
- *Verdict*: **NOT-A-BUG.**

**4. Victreebel's Focus Sash did not save it from the multi-hit KO (turn 15).**
- *Saw*: Victreebel fainted with a Focus Sash equipped.
- *Expected*: a Sash holder survives at 1 HP.
- *Checked*: Focus Sash requires full HP. Victreebel had already taken 26 from
  Cloyster's Rocky Helmet (156/6 = 26) when it used contact-flagged Power Whip on the
  same turn, so it entered the Icicle Spear sequence at 130/156. The Sash was disarmed
  by the helmet before the spears landed.
- *Verdict*: **NOT-A-BUG** (and the single most elegant thing that happened in the match).

**5. Cursed Body fired on the hit that killed Gengar (turn 12).**
- *Saw*: `Gengar took 58 damage. / Gengar's Cursed Body disabled Charizard's Air Slash!
  / Gengar fainted!` — with the Disable persisting on Charizard after Gengar was gone.
- *Expected*: possibly nothing, if a fainting Pokémon's on-hit ability is suppressed.
- *Checked*: `internal/engine/abilities.go:953-973`. The `OnHit` hook gates on
  `hitSub`, a 30% roll, and the *attacker* being alive/not-already-disabled; it does not
  gate on the defender surviving. This matches canon — damage-triggered abilities
  (Rough Skin, Static, Cursed Body) do activate on the KO blow, and Disable's timer
  runs on the attacker independently of the disabler.
- *Verdict*: **NOT-A-BUG.**

**6. Giga Drain healed nothing and printed no drain line (turn 6), but did on turn 16.**
- *Saw*: turn 6 `Muk took 30 damage.` then straight to `Venusaur was hurt by its Life
  Orb! (-15)`, no restore line; turn 16 `Cloyster took 50 damage. / Venusaur restored
  25 HP.`
- *Expected*: a drain line on both.
- *Checked*: Venusaur was at 156/156 on turn 6, so the 15 HP drain was a no-op and the
  engine elides the message when the heal amount is zero. HP accounting is right in
  both cases (turn 6: 156 − 15 Life Orb = 141 = 90%; turn 16: 111 + 25 − 15 = 121 = 78%),
  and the drain correctly uses *damage actually dealt* (50, the clamped value) rather
  than the uncapped roll.
- *Verdict*: **NOT-A-BUG** (cosmetic elision only).

**Weather audit, all clean.** Drought fired on Ninetales' lead switch-in
(`abilities.go:386` → `setWeatherFromAbility`). Heat Rock extended the duration to 8
(`items_field.go:70`, `extendedFieldTurns = 8`) and the sun ticked down on exactly
turns 1–8, faded at end of turn 8 (`residuals.go:201`) — correctly *not* shortened
retroactively when Muk knocked the Heat Rock off on turn 4, and correctly surviving
Ninetales' death on turn 5. Solar Beam skipped its charge only while the sun was up
(turn 1; `turn.go:1081`). Fire Blast ran at 1.5× on turns 2–5 and at 1× thereafter,
verified against the stat lines. Chlorophyll doubled Venusaur to 264 Speed on turns
6–8 and dropped it back to 132 from turn 9 (it still outran Cloyster's 90 — no
anomaly). Growth gave +2/+2 in sun on turn 8 (`callbackmoves.go:309`). Solar Power
never had an opportunity to fire, correctly: Charizard entered on turn 9, one turn
after the sun expired, so there was neither a 1.5× boost nor a 1/8 chip to check.
Speed order, hazard non-application (nothing was ever set), status residuals,
Sitrus/Leftovers/Black Sludge/Rocky Helmet/Life Orb triggers and Intimidate all
reconciled to the stat blocks at level 50.
