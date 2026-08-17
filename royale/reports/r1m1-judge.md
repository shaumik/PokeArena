# r1m1 — Hairtrigger vs The Bulwark

## Verdict

**Hairtrigger wins 6–0 in 18 turns — a clean sweep.** The Bulwark never took a single Pokémon off the board; Hairtrigger finished with all six alive (Alakazam at 12% and paralysed, Jolteon at 61%, the other four untouched at 100%). The match ended on Turn 18 when Jolteon's Thunderbolt critically struck Slowbro, the last wall standing. The turn cap (120) was never remotely in play.

## The story

The Bulwark actually won the opening. Chansey's Turn 1 Thunder Wave was the best move either side made all match — it stripped Alakazam of the speed that is the entire premise of a glass cannon, and it collected on Turn 6 with a full-paralysis freeze that left Alakazam at 12%. The Turn 2 pivot to Unaware Clefable was a genuinely excellent read, blanking a +2 Nasty Plot as if it had never happened. Then on Turn 5 the stall team stopped stalling: Chansey stayed in to throw Seismic Toss with Soft-Boiled sitting unused in its pocket, and died to a 70%-accurate Focus Blast — the agent's own next message opens "I got greedy and Chansey paid for it."

**The turning point is Turn 8.** Hairtrigger pivoted the dying Alakazam out for Choice Specs Jolteon, and the Bulwark discovered it had built a six-Pokémon wall with no Ground type, no Electric immunity, and three water-types weak to Electric. From that moment Jolteon was locked into a single move it never needed to change, and Thunderbolt became a guillotine: Weezing, Vaporeon, Tentacruel, Clefable and Slowbro all died to the same button in eleven turns. The Bulwark spent its last six turns cycling Protect and switching bodies into a queue, correctly identifying that it was buying turns it had no way to convert. It was a teambuilding loss executed by a pilot who played it about as well as it could be played.

## Scorecard

- **Hairtrigger** — *Yes, it played the archetype, and it played it honestly.* It never once picked a defensive or stalling option, it pivoted the instant a Pokémon stopped being a threat rather than feeding it, and it won by deleting things before they moved. **Best decision: Turn 7**, switching Alakazam out at 16 HP instead of squeezing one more attack out of it — that turn cost nothing and brought in the Pokémon that won the match. **Worst decision: Turn 2**, Focus Blast into a Fairy. Fighting is resisted by Fairy (0.5×) and Unaware had already made the Nasty Plot worthless; neutral Psychic hit for 91 the very next turn versus Focus Blast's 41. The agent diagnosed its own error one turn later.
- **The Bulwark** — *Mostly yes, with one decisive lapse.* Toxic, Protect, recovery, pivot-to-the-right-resist — the shape was right all game, and its endgame triage (spend the bodies that cannot anchor a stall first, keep Slowbro benched) was disciplined. **Best decision: Turn 2**, switching to Unaware Clefable to eat the Nasty Plot. It correctly identified that the opponent's boost existed only on their side of the screen, and it cost Hairtrigger an entire turn. Honourable mention to Turn 9, where it worked out that Weezing was the *only* body on the roster that survives a Specs Thunderbolt and deliberately made it the lightning rod. **Worst decision: Turn 5**, violating its own stated creed ("never trade, always heal"). Chansey at 64% with Soft-Boiled available chose Seismic Toss and died. Healing there forces Hairtrigger to land two more 70%-accuracy Focus Blasts, and Chansey is the one Pokémon on the roster that walls Alakazam indefinitely.

## MVP

**Jolteon (Hairtrigger).** Five of the six KOs, from Turn 9 to Turn 18, without ever switching out and without ever being permitted to select a second move — Choice Specs locked it into Thunderbolt on Turn 8 and it simply never needed anything else. It came in at 61% after eating a Scald on the switch and finished the match at exactly 61%, having taken no further damage at all. The Bulwark's entire remaining roster after Chansey fell was three water-types, a Poison type and a Fairy, none of them Ground, none of them able to switch out of an Electric STAB. Jolteon didn't outplay the wall; it invalidated it.

## Notable turns

- **Turn 1** — Chansey's Thunder Wave lands on Alakazam. The single best move the Bulwark made; it cashes five turns later and permanently removes Hairtrigger's lead from the match.
- **Turn 2** — The Bulwark pivots to Unaware Clefable, deleting a +2 Nasty Plot without spending anything. Hairtrigger's Focus Blast into the Fairy resist does 41.
- **Turn 5** — Chansey declines to heal and throws Seismic Toss instead. Focus Blast (70% accuracy) connects for the KO. The stall team's anchor dies with its recovery move unused.
- **Turn 6** — Full paralysis. Alakazam can't move, eats a second Scald, and drops to 12%. The Turn 1 Thunder Wave finally collects.
- **Turn 8** — **The turning point.** Jolteon enters on Choice Specs and the Bulwark's Electric-blank roster is exposed. Every subsequent switch is a body fed into the same move.
- **Turn 17** — Clefable's third consecutive Protect fails the 33% stall roll and it dies where it stands, leaving Slowbro alone.

## BUGS

### 1. Secondary effects fire on a target the same hit reduced to 0 HP — CONFIRMED

**What I saw (Turn 17):**

```
| Jolteon used Thunderbolt!
| Clefable took 70 damage.
| Clefable was paralyzed!
| Clefable fainted!
```

Clefable was on exactly 70 HP. Thunderbolt's 10% paralysis secondary applied *after* the damage had already taken it to zero, and *before* the faint was announced.

**What I expected:** In canonical Pokémon mechanics a secondary effect cannot apply to a target that faints from the attack that carried it. Showdown skips the secondary once the target's HP reaches 0.

**What the source says:** `internal/engine/effects.go`, `applyDamageEffects`. The function guards `m.Self` with `!atk.Fainted` and `m.Primary` with `!def.Fainted`, but the `m.Secondaries` loop has **no defender guard at all** — only `if sec.Self && atk.Fainted { continue }`. More importantly, all three of those guards read the wrong field. `internal/engine/turn.go:864-880` documents a deliberate "faint window": `dealDamage` sets `def.HP -= dmg` but never calls `faint()`, which is only reached at turn.go:888 — *after* `applyDamageEffects` runs at turn.go:809. The comment is explicit about the consequence:

> Everything above this point, from the damage loop down, runs while a killed Pokémon still has `Fainted == false` and `HP == 0`. Anything added in that stretch that asks "is this Pokémon out of the fight?" must test the HP, not the flag — `isDown()` in items_moves.go is that predicate, and three separate bugs came from sites that checked `Fainted` alone.

`isDown()` (`items_moves.go:66`, `p == nil || p.Fainted || p.HP <= 0`) is used at nine sites in `items_moves.go` and at **zero** sites in `effects.go`. `applyDamageEffects` is inside the faint window and checks the flag, so its guards are dead code by construction.

**Reproduction** (I ran this as a temporary test and removed it afterwards; the repo is unmodified):

```go
// Jolteon (135) Thunderbolt vs Clefable (36) set to 1 HP, abilities/items cleared,
// 400 seeds. Every hit is lethal.
// Result: lethal hits 400/400; paralysis applied to a 0-HP target in 42/400 (~10%,
// exactly Thunderbolt's secondary rate).
// e.g. seed 8: "Jolteon used Thunderbolt! | Clefable took 1 damage. |
//               Clefable was paralyzed! | Clefable fainted!"
```

**Severity — low today, latent tomorrow.** In the current engine this is log noise only: `faint()` (`internal/engine/state.go:13-30`) clears `Status`, `SleepTurns`, `ToxicCounter` and `Volatiles`, so nothing the secondary writes survives. But two things make it worth fixing rather than documenting. First, it is a correctness trap for anything added later that persists through a faint or reads status mid-window. Second, the natural fix is one line per guard and makes the file obey the invariant the engine already wrote down: replace `!def.Fainted` / `atk.Fainted` with `!isDown(def)` / `isDown(atk)`, and add an `isDown(def)` skip for non-self secondaries.

### 2. Damage figures larger than the target's remaining HP — NOT-A-BUG

Turns 5, 12, 13, 15, 17 and 18 all log a damage number exactly equal to the target's remaining HP (Chansey 227, Weezing 46, Vaporeon 237, Tentacruel 94, Clefable 70, Slowbro 202), which reads like a suspiciously precise coincidence six times over. `turn.go:1272` clamps `if dmg > def.HP { dmg = def.HP }` before the log line is emitted, so the engine reports damage *dealt*, not damage *rolled*. Every one of these was a large overkill roll displayed truthfully. Correct-but-surprising; the underlying rolls all sat inside their computed ranges.

### 3. Turn 2 — Focus Blast for only 41 into Clefable — NOT-A-BUG

At +2 with a Life Orb this looked far too low. Two things stack: Fighting is resisted by Fairy (0.5×, confirmed in `data/typechart.json`), and Clefable's Unaware (`abilities.go:1178`, `IgnoresOpponentStages: true`) blanks the +2 entirely. Recomputing with A = 187 unboosted against Clefable's 156 SpD gives a 36–42 range. 41 is a high roll inside it. The engine was right and the attacker's read was wrong — which the agent worked out itself one turn later.

### 4. Turns 10 and 11 — two consecutive Protects both succeeded — NOT-A-BUG

`internal/engine/protect.go` implements the canonical stall chain: `protectChance` returns 100 / 33 / 11 / 4 / 1 percent by consecutive-use count, the counter increments only on success, and a failed roll resets it to 0. Weezing hit a legitimate 33% on its second attempt and then correctly declined the 11% third. The chain resetting when Clefable switched out on Turn 15 and returned on Turn 16 at a fresh 100% is also correct — `ProtectCounter` lives in `Volatiles`, which is wiped on switch. Turn 17's "But it failed!" is the same system working.

### 5. Cosmetic — `"X protected itself!"` is emitted by two different events

`protect.go:48` logs it when the shield goes up; `turn.go:694` logs the same string when the shield repels an incoming move. So a successful Protect turn prints the line twice (Turns 10, 11, 14, 16), which reads like a double-fire. Both events are real and correctly ordered — this is a log-wording collision, not a mechanics defect. Worth distinguishing the second string ("X protected itself!" vs "Jolteon's attack was blocked!") purely for readability.

### Everything else checked clean

I verified every HP transition in the match against a reimplementation of the engine's own formulas (`damage.go` stat/damage math, `data/pokedex.json` bases, `data/natures.json`, `data/typechart.json`, `data/moves.json`). Specifically confirmed correct: all sixteen damage rolls inside their computed min–max ranges; STAB, type effectiveness and the Choice Specs / Life Orb ×1.5 / ×1.3 multipliers; Leftovers 1/16 (+22 on Chansey), Black Sludge 1/16 on a Poison holder (+10 on Weezing, three times), Sitrus Berry 25% at the half-HP threshold (+46 on Tentacruel); Magic Guard correctly suppressing Life Orb recoil on Alakazam every turn it attacked; Regenerator correctly *not* healing Slowbro on two switch-outs at full HP; paralysis halving Alakazam's speed (189→94, still ahead of Chansey's 70 and Clefable's 80) and the 25% full-paralysis rate (`turn.go:1457`); Protect's +4 priority letting Weezing and Clefable move before a 200-speed Jolteon; speed order in every one of the 18 turns; switches resolving before moves; the Choice lock holding Jolteon on Thunderbolt for eleven straight turns with PP correctly decrementing; and all four build-time format clauses (Species, Item, Evasion, OHKO) satisfied by both rosters. No hazards, weather, terrain or screens were set all match, so those paths went untested here. `go test ./internal/engine/` passes clean (9.1s).
