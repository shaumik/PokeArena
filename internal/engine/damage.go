package engine

import (
	"math"

	"pokearena/internal/domain"
)

// Level is the fixed level every Pokémon battles at. A single level keeps
// matchups fair and makes the demo about types and stats, not grinding.
const Level = 50

// calcStat / calcHP derive battle stats from base stats at Level, assuming
// perfect IVs (31), no EVs, and a neutral nature — the standard fair spread.
func calcStat(base int) int { return (2*base+31)*Level/100 + 5 }
func calcHP(base int) int   { return (2*base+31)*Level/100 + Level + 10 }

// stageMultiplier converts a -6..+6 offensive/defensive/speed stat stage to
// its multiplier. The curve is symmetric around 1.0: (2+s)/2 for s≥0,
// 2/(2-s) for s<0.
func stageMultiplier(stage int) float64 {
	if stage > 6 {
		stage = 6
	}
	if stage < -6 {
		stage = -6
	}
	if stage >= 0 {
		return float64(2+stage) / 2.0
	}
	return 2.0 / float64(2-stage)
}

// accStageMultiplier converts an accuracy or evasion stage to its multiplier.
// The curve is different from the offensive stages: (3+s)/3 for s≥0, 3/(3-s)
// for s<0 — Gen 3+ Pokémon table.
func accStageMultiplier(stage int) float64 {
	if stage > 6 {
		stage = 6
	}
	if stage < -6 {
		stage = -6
	}
	if stage >= 0 {
		return float64(3+stage) / 3.0
	}
	return 3.0 / float64(3-stage)
}

// effectiveSpeed is the speed used for turn order: base speed scaled by its
// stage, halved by paralysis, and modified by any ability speed multiplier
// (weather speed boosters Swift Swim / Chlorophyll / Sand Rush / Slush
// Rush; Quick Feet when statused) and held-item multiplier (Choice Scarf
// ×1.5). Weather is the effective (Cloud Nine honoring) value, not the raw
// field state.
func effectiveSpeed(p *Pokemon, weather *WeatherState) int {
	spd := float64(p.Stats.Spe) * stageMultiplier(p.Stages.Spe)
	if p.Status == StatusParalysis {
		// Quick Feet ignores the paralysis cut (it gets its own ×1.5 on top
		// via SpeedMult). Other status types don't slow the holder.
		if a := abilityOf(p); a == nil || a.Kind != "quick-feet" {
			spd *= 0.5
		}
	}
	spd *= abilitySpeedMult(p, weather)
	spd *= itemSpeedMult(p, weather)
	return int(spd)
}

// DamageResult is the outcome of one damage calculation.
//
// Sturdy is true when the defender's Sturdy ability clamped the hit to leave
// it at 1 HP (a precondition-gated save, not a generic damage mod). The
// caller emits the "X hung on with Sturdy!" log line.
//
// AbilityImmune is true when the zero-damage result came from an ability's
// TypeMultOverride (Levitate, Volt Absorb, etc.). The caller uses this to
// route the immunity bonus hook (heal / boost) and choose a different log
// line than the plain "doesn't affect" message.
type DamageResult struct {
	Damage        int
	Crit          bool
	Effectiveness float64
	Sturdy        bool
	AbilityImmune bool
}

// computeDamage applies the Gen-3+ damage formula:
//
//	dmg = (((2L/5+2)·Power·A/D)/50 + 2) · STAB · Type · Crit · Random · Burn · Weather · Terrain · Screen
//
// A/D are the physical or special stats depending on the move's category,
// scaled by stat stages; Burn halves physical attack. Weather modifies the
// damage type multiplier (Sun: Fire+50%/Water-50%, Rain: Water+50%/Fire-50%)
// and the defender's defensive stat (Sandstorm: Rock +50% SpD; Snow: Ice +50%
// Def). Terrain boosts matching-type damage (Electric/Grassy/Psychic ×1.3
// when the attacker is grounded) and halves Dragon vs grounded defenders
// under Misty and Earthquake/Bulldoze vs grounded defenders under Grassy.
// Screens (defScreens) halve the matching category (Reflect physical,
// Light Screen special, Aurora Veil both) — skipped on a crit. See
// internal/engine/weather.go, terrain.go, screens.go.
//
// Moves flagged `fixed-damage-level` (Seismic Toss, Night Shade) short-
// circuit the formula and deal exactly L damage — but the type-immunity
// check still applies (Ghost is immune to Fighting, etc).
func computeDamage(dex *domain.Dex, atk, def *Pokemon, m domain.Move, weather *WeatherState, terrain *TerrainState, defScreens *SideConditions, pw *PseudoWeather, rng *RNG) DamageResult {
	if abilitySuppressesWeather(atk) || abilitySuppressesWeather(def) {
		weather = nil
	}
	// Foresight / Miracle Eye lift specific type-chart immunities
	// (Ghost vs Normal/Fighting; Dark vs Psychic) via the lift-aware
	// helper. Smack Down additionally lifts the Flying chart immunity
	// to Ground — we substitute "" for Flying so the chart returns 1
	// instead of 0 on that pair. Both lifts happen before the ability
	// TypeMultOverride pass so a foresighted Ghost takes neutral
	// damage from Tackle while Levitate vs Earthquake still resolves
	// as a clean 0 below (Levitate is handled in the override block).
	var eff float64
	if m.Type == "ground" && groundedBySmackDown(def) {
		t1, t2 := def.Type1, def.Type2
		if t1 == "flying" {
			t1 = ""
		}
		if t2 == "flying" {
			t2 = ""
		}
		eff = dex.Effectiveness(m.Type, t1, t2)
	} else {
		eff = effectivenessWithLifts(dex, m.Type, def, abilityScrappy(atk))
	}
	abilityImmune := false
	if mult, override := abilityTypeMultOverride(def, m.Type); override {
		// Smack Down also lifts Levitate vs Ground — skip the
		// ability override in that case so the chart result stands.
		if m.Type != "ground" || !groundedBySmackDown(def) {
			eff = mult
			abilityImmune = (mult == 0)
		}
	}
	// Magnet Rise / Telekinesis grant Ground immunity (canceled by
	// Smack Down via groundImmuneFromVolatile's internal check).
	if m.Type == "ground" && groundImmuneFromVolatile(def) {
		eff = 0
	}
	if eff == 0 {
		return DamageResult{Effectiveness: 0, AbilityImmune: abilityImmune}
	}
	if m.HasFlag("fixed-damage-level") {
		// Effectiveness reported as 1.0 so the caller doesn't log "super
		// effective" or "resisted" lines on fixed-damage moves.
		return DamageResult{Damage: Level, Effectiveness: 1.0}
	}

	a, d := offensiveDefensiveStats(atk, def, m, pw)
	d *= defenseMult(weather, def, m.Category)

	// Charge doubles the base power of an Electric move. The flag is a
	// single-use, cleared after the next damaging move regardless of
	// type (canonical Showdown behavior). Consumption happens in
	// executeMove's tail; computeDamage only reads the flag.
	power := m.Power
	if atk.Volatiles.Charge && m.Type == "electric" {
		power *= 2
	}
	base := (float64(2*Level)/5.0+2.0)*float64(power)*a/d/50.0 + 2.0

	stab := 1.0
	if m.Type != "" && (m.Type == atk.Type1 || m.Type == atk.Type2) {
		stab = 1.5
	}

	// Critical hit: stage-driven. High-crit moves contribute +1 stage;
	// Focus Energy contributes +2 (critStageBonus). Laser Focus is a
	// one-shot guaranteed crit and trumps the stage table. Battle
	// Armor / Shell Armor on the defender still block any crit
	// outright (canonical absolute block).
	critStage := critStageBonus(atk)
	if m.HasFlag("high-crit") {
		critStage++
	}
	critDenom := critChanceDenom(critStage)
	crit := rng.IntN(critDenom) == 0
	if atk.Volatiles.LaserFocus || isAlwaysCrit(m.ID) {
		crit = true
	}
	if abilityBlocksCrit(def) {
		crit = false
	}
	critMult := 1.0
	if crit {
		critMult = 1.5
		// Sniper makes crits ×1.5 instead of the normal ×1.5 — i.e. the crit
		// hit multiplies by 2.25 total. Modeled here so all crit math stays
		// in one place.
		if a := abilityOf(atk); a != nil && a.Kind == "sniper" {
			critMult = 2.25
		}
	}

	randMult := float64(rng.Range(85, 100)) / 100.0
	wmult := damageMultByType(weather, m.Type)
	tmult := terrainDamageMult(terrain, atk, def, m)
	smult := screenDamageMult(defScreens, m, crit)
	abilDef := abilityIncomingDamageMult(def, m, eff)
	abilAtk := abilityOutgoingDamageMult(atk, m, def, weather, eff)
	itemAtk := itemOutgoingDamageMult(atk, m, def, weather, eff)

	dmg := int(math.Floor(base * stab * eff * critMult * randMult * wmult * tmult * smult * abilDef * abilAtk * itemAtk))
	if dmg < 1 {
		dmg = 1
	}
	dmg, sturdy := abilitySurviveOHKO(def, dmg)
	return DamageResult{Damage: dmg, Crit: crit, Effectiveness: eff, Sturdy: sturdy}
}

// offensiveDefensiveStats picks the (attacker A, defender D) pair the damage
// formula consumes — Atk/Def for physical, SpA/SpD for special — scaled by
// stat stages and modified by burn on the attacker.
//
// ignoreDefensive (Chip Away, Darkest Lariat) zeros only positive defensive
// stages; drops still amplify the attacker's damage. Mirrors canonical
// Showdown semantics: "ignore the buff, not the debuff". The clamp is per-
// move and per-category — only the stage actually read this turn is
// affected, so a Physical mover never touches SpD here.
//
// Wonder Room (pw.WonderRoom != nil) swaps which defensive stat the
// formula reads: a physical attack uses the target's SpD (with the
// SpD stage), a special attack uses the target's Def (with the Def
// stage). Stages travel with the underlying stat — Showdown's
// canonical behavior.
func offensiveDefensiveStats(atk, def *Pokemon, m domain.Move, pw *PseudoWeather) (float64, float64) {
	wonder := pw != nil && pw.WonderRoom != nil
	// Unaware zeros the opponent's stages entirely (buff and debuff alike),
	// distinct from IgnoreDefensive which only clamps positive defensive
	// stages. The attacker's Unaware blanks the defender's defensive stage;
	// the defender's Unaware blanks the attacker's offensive stage.
	atkUnaware := abilityIgnoresStages(atk)
	defUnaware := abilityIgnoresStages(def)
	var a, d float64
	if m.Category == domain.CatPhysical {
		defRaw, defStage := def.Stats.Def, def.Stages.Def
		if wonder {
			defRaw, defStage = def.Stats.SpD, def.Stages.SpD
		}
		if m.IgnoreDefensive && defStage > 0 {
			defStage = 0
		}
		if atkUnaware {
			defStage = 0
		}
		atkStage := atk.Stages.Atk
		if defUnaware {
			atkStage = 0
		}
		a = float64(atk.Stats.Atk) * stageMultiplier(atkStage)
		d = float64(defRaw) * stageMultiplier(defStage)
		if atk.Status == StatusBurn {
			a *= 0.5
		}
	} else {
		defRaw, defStage := def.Stats.SpD, def.Stages.SpD
		if wonder {
			defRaw, defStage = def.Stats.Def, def.Stages.Def
		}
		if m.IgnoreDefensive && defStage > 0 {
			defStage = 0
		}
		if atkUnaware {
			defStage = 0
		}
		atkStage := atk.Stages.SpA
		if defUnaware {
			atkStage = 0
		}
		a = float64(atk.Stats.SpA) * stageMultiplier(atkStage)
		d = float64(defRaw) * stageMultiplier(defStage)
	}
	return a, d
}

// ExpectedDamage estimates a move's damage with an average roll (0.925) and
// no critical hit. The AI uses it to score actions without consuming RNG.
// It returns 0 for status moves and immune matchups. defScreens is the
// defender side's screens — the matching-category halving applies (no crit
// at the average roll), so the AI's switch / move scores see screens for
// what they are.
func ExpectedDamage(dex *domain.Dex, atk, def *Pokemon, m domain.Move, weather *WeatherState, terrain *TerrainState, defScreens *SideConditions) int {
	if m.Category == domain.CatStatus {
		return 0
	}
	if abilitySuppressesWeather(atk) || abilitySuppressesWeather(def) {
		weather = nil
	}
	eff := effectivenessWithLifts(dex, m.Type, def, abilityScrappy(atk))
	if mult, override := abilityTypeMultOverride(def, m.Type); override {
		eff = mult
	}
	if eff == 0 {
		return 0
	}
	if m.HasFlag("fixed-damage-level") {
		return Level
	}
	// Pseudo-weather is not threaded into the AI's damage estimator
	// — Wonder Room's stat swap is a rare and short-lived condition,
	// and adding the param to ExpectedDamage would ripple to every
	// View consumer. The AI may misjudge damage by ±50% during the
	// 5 turns Wonder Room is active; acceptable for now.
	a, d := offensiveDefensiveStats(atk, def, m, nil)
	d *= defenseMult(weather, def, m.Category)
	power := m.Power
	if atk.Volatiles.Charge && m.Type == "electric" {
		power *= 2
	}
	base := (float64(2*Level)/5.0+2.0)*float64(power)*a/d/50.0 + 2.0
	stab := 1.0
	if m.Type != "" && (m.Type == atk.Type1 || m.Type == atk.Type2) {
		stab = 1.5
	}
	wmult := damageMultByType(weather, m.Type)
	tmult := terrainDamageMult(terrain, atk, def, m)
	smult := screenDamageMult(defScreens, m, false)
	abilDef := abilityIncomingDamageMult(def, m, eff)
	abilAtk := abilityOutgoingDamageMult(atk, m, def, weather, eff)
	itemAtk := itemOutgoingDamageMult(atk, m, def, weather, eff)
	dmg := int(base * stab * eff * 0.925 * wmult * tmult * smult * abilDef * abilAtk * itemAtk)
	if dmg < 1 {
		dmg = 1
	}
	return dmg
}
