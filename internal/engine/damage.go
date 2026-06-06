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
// Rush; Quick Feet when statused). Weather is the effective (Cloud Nine
// honoring) value, not the raw field state.
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
//	dmg = (((2L/5+2)·Power·A/D)/50 + 2) · STAB · Type · Crit · Random · Burn · Weather · Terrain
//
// A/D are the physical or special stats depending on the move's category,
// scaled by stat stages; Burn halves physical attack. Weather modifies the
// damage type multiplier (Sun: Fire+50%/Water-50%, Rain: Water+50%/Fire-50%)
// and the defender's defensive stat (Sandstorm: Rock +50% SpD; Snow: Ice +50%
// Def). Terrain boosts matching-type damage (Electric/Grassy/Psychic ×1.3
// when the attacker is grounded) and halves Dragon vs grounded defenders
// under Misty and Earthquake/Bulldoze vs grounded defenders under Grassy.
// See internal/engine/weather.go and internal/engine/terrain.go.
//
// Moves flagged `fixed-damage-level` (Seismic Toss, Night Shade) short-
// circuit the formula and deal exactly L damage — but the type-immunity
// check still applies (Ghost is immune to Fighting, etc).
func computeDamage(dex *domain.Dex, atk, def *Pokemon, m domain.Move, weather *WeatherState, terrain *TerrainState, rng *RNG) DamageResult {
	if abilitySuppressesWeather(atk) || abilitySuppressesWeather(def) {
		weather = nil
	}
	eff := dex.Effectiveness(m.Type, def.Type1, def.Type2)
	abilityImmune := false
	if mult, override := abilityTypeMultOverride(def, m.Type); override {
		eff = mult
		abilityImmune = (mult == 0)
	}
	if eff == 0 {
		return DamageResult{Effectiveness: 0, AbilityImmune: abilityImmune}
	}
	if m.HasFlag("fixed-damage-level") {
		// Effectiveness reported as 1.0 so the caller doesn't log "super
		// effective" or "resisted" lines on fixed-damage moves.
		return DamageResult{Damage: Level, Effectiveness: 1.0}
	}

	a, d := offensiveDefensiveStats(atk, def, m)
	d *= defenseMult(weather, def, m.Category)

	base := (float64(2*Level)/5.0+2.0)*float64(m.Power)*a/d/50.0 + 2.0

	stab := 1.0
	if m.Type != "" && (m.Type == atk.Type1 || m.Type == atk.Type2) {
		stab = 1.5
	}

	// Critical hit: 1/24 normally, 1/8 for high-crit moves. Battle Armor /
	// Shell Armor on the defender block crits outright.
	critDenom := 24
	if m.HasFlag("high-crit") {
		critDenom = 8
	}
	crit := rng.IntN(critDenom) == 0
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
	abilDef := abilityIncomingDamageMult(def, m, eff)
	abilAtk := abilityOutgoingDamageMult(atk, m, def, weather, eff)

	dmg := int(math.Floor(base * stab * eff * critMult * randMult * wmult * tmult * abilDef * abilAtk))
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
func offensiveDefensiveStats(atk, def *Pokemon, m domain.Move) (float64, float64) {
	var a, d float64
	if m.Category == domain.CatPhysical {
		defStage := def.Stages.Def
		if m.IgnoreDefensive && defStage > 0 {
			defStage = 0
		}
		a = float64(atk.Stats.Atk) * stageMultiplier(atk.Stages.Atk)
		d = float64(def.Stats.Def) * stageMultiplier(defStage)
		if atk.Status == StatusBurn {
			a *= 0.5
		}
	} else {
		defStage := def.Stages.SpD
		if m.IgnoreDefensive && defStage > 0 {
			defStage = 0
		}
		a = float64(atk.Stats.SpA) * stageMultiplier(atk.Stages.SpA)
		d = float64(def.Stats.SpD) * stageMultiplier(defStage)
	}
	return a, d
}

// ExpectedDamage estimates a move's damage with an average roll (0.925) and
// no critical hit. The AI uses it to score actions without consuming RNG.
// It returns 0 for status moves and immune matchups.
func ExpectedDamage(dex *domain.Dex, atk, def *Pokemon, m domain.Move, weather *WeatherState, terrain *TerrainState) int {
	if m.Category == domain.CatStatus {
		return 0
	}
	if abilitySuppressesWeather(atk) || abilitySuppressesWeather(def) {
		weather = nil
	}
	eff := dex.Effectiveness(m.Type, def.Type1, def.Type2)
	if mult, override := abilityTypeMultOverride(def, m.Type); override {
		eff = mult
	}
	if eff == 0 {
		return 0
	}
	if m.HasFlag("fixed-damage-level") {
		return Level
	}
	a, d := offensiveDefensiveStats(atk, def, m)
	d *= defenseMult(weather, def, m.Category)
	base := (float64(2*Level)/5.0+2.0)*float64(m.Power)*a/d/50.0 + 2.0
	stab := 1.0
	if m.Type != "" && (m.Type == atk.Type1 || m.Type == atk.Type2) {
		stab = 1.5
	}
	wmult := damageMultByType(weather, m.Type)
	tmult := terrainDamageMult(terrain, atk, def, m)
	abilDef := abilityIncomingDamageMult(def, m, eff)
	abilAtk := abilityOutgoingDamageMult(atk, m, def, weather, eff)
	dmg := int(base * stab * eff * 0.925 * wmult * tmult * abilDef * abilAtk)
	if dmg < 1 {
		dmg = 1
	}
	return dmg
}
