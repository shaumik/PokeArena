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
// stage, halved by paralysis.
func effectiveSpeed(p *Pokemon) int {
	spd := float64(p.Stats.Spe) * stageMultiplier(p.Stages.Spe)
	if p.Status == StatusParalysis {
		spd *= 0.5
	}
	return int(spd)
}

// DamageResult is the outcome of one damage calculation.
type DamageResult struct {
	Damage        int
	Crit          bool
	Effectiveness float64
}

// computeDamage applies the Gen-3+ damage formula:
//
//	dmg = (((2L/5+2)·Power·A/D)/50 + 2) · STAB · Type · Crit · Random · Burn · Weather
//
// A/D are the physical or special stats depending on the move's category,
// scaled by stat stages; Burn halves physical attack. Weather modifies the
// damage type multiplier (Sun: Fire+50%/Water-50%, Rain: Water+50%/Fire-50%)
// and the defender's defensive stat (Sandstorm: Rock +50% SpD; Snow: Ice +50%
// Def). See internal/engine/weather.go.
//
// Moves flagged `fixed-damage-level` (Seismic Toss, Night Shade) short-
// circuit the formula and deal exactly L damage — but the type-immunity
// check still applies (Ghost is immune to Fighting, etc).
func computeDamage(dex *domain.Dex, atk, def *Pokemon, m domain.Move, weather *WeatherState, rng *RNG) DamageResult {
	eff := dex.Effectiveness(m.Type, def.Type1, def.Type2)
	if eff == 0 {
		return DamageResult{Effectiveness: 0}
	}
	if m.HasFlag("fixed-damage-level") {
		// Effectiveness reported as 1.0 so the caller doesn't log "super
		// effective" or "resisted" lines on fixed-damage moves.
		return DamageResult{Damage: Level, Effectiveness: 1.0}
	}

	var a, d float64
	if m.Category == domain.CatPhysical {
		a = float64(atk.Stats.Atk) * stageMultiplier(atk.Stages.Atk)
		d = float64(def.Stats.Def) * stageMultiplier(def.Stages.Def)
		if atk.Status == StatusBurn {
			a *= 0.5
		}
	} else {
		a = float64(atk.Stats.SpA) * stageMultiplier(atk.Stages.SpA)
		d = float64(def.Stats.SpD) * stageMultiplier(def.Stages.SpD)
	}
	d *= defenseMult(weather, def, m.Category)

	base := (float64(2*Level)/5.0+2.0)*float64(m.Power)*a/d/50.0 + 2.0

	stab := 1.0
	if m.Type != "" && (m.Type == atk.Type1 || m.Type == atk.Type2) {
		stab = 1.5
	}

	// Critical hit: 1/24 normally, 1/8 for high-crit moves.
	critDenom := 24
	if m.HasFlag("high-crit") {
		critDenom = 8
	}
	crit := rng.IntN(critDenom) == 0
	critMult := 1.0
	if crit {
		critMult = 1.5
	}

	randMult := float64(rng.Range(85, 100)) / 100.0
	wmult := damageMultByType(weather, m.Type)

	dmg := int(math.Floor(base * stab * eff * critMult * randMult * wmult))
	if dmg < 1 {
		dmg = 1
	}
	return DamageResult{Damage: dmg, Crit: crit, Effectiveness: eff}
}

// ExpectedDamage estimates a move's damage with an average roll (0.925) and
// no critical hit. The AI uses it to score actions without consuming RNG.
// It returns 0 for status moves and immune matchups.
func ExpectedDamage(dex *domain.Dex, atk, def *Pokemon, m domain.Move, weather *WeatherState) int {
	if m.Category == domain.CatStatus {
		return 0
	}
	eff := dex.Effectiveness(m.Type, def.Type1, def.Type2)
	if eff == 0 {
		return 0
	}
	if m.HasFlag("fixed-damage-level") {
		return Level
	}
	var a, d float64
	if m.Category == domain.CatPhysical {
		a = float64(atk.Stats.Atk) * stageMultiplier(atk.Stages.Atk)
		d = float64(def.Stats.Def) * stageMultiplier(def.Stages.Def)
		if atk.Status == StatusBurn {
			a *= 0.5
		}
	} else {
		a = float64(atk.Stats.SpA) * stageMultiplier(atk.Stages.SpA)
		d = float64(def.Stats.SpD) * stageMultiplier(def.Stages.SpD)
	}
	d *= defenseMult(weather, def, m.Category)
	base := (float64(2*Level)/5.0+2.0)*float64(m.Power)*a/d/50.0 + 2.0
	stab := 1.0
	if m.Type != "" && (m.Type == atk.Type1 || m.Type == atk.Type2) {
		stab = 1.5
	}
	wmult := damageMultByType(weather, m.Type)
	dmg := int(base * stab * eff * 0.925 * wmult)
	if dmg < 1 {
		dmg = 1
	}
	return dmg
}
