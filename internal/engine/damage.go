package engine

import (
	"math"

	"pokearena/internal/domain"
)

// Level is the fixed level every Pokémon battles at. A single level keeps
// matchups fair and makes the demo about types and stats, not grinding.
const Level = 50

// Spread limits. EVs are capped per stat and in total; IVs run 0..31. The
// caps are the modern (Gen 6+) rule Showdown enforces, not knobs — see
// ValidateTeam, which is the only gate that applies them.
const (
	MaxIV        = 31
	MaxEVPerStat = 252
	MaxEVTotal   = 510
)

// statBase is the shared first step of both stat formulas:
//
//	floor((2·Base + IV + floor(EV/4)) · Level / 100)
//
// Go's integer division is truncation toward zero, and every operand here is
// non-negative, so it *is* the floor the games specify.
func statBase(base, iv, ev int) int {
	return (2*base + iv + ev/4) * Level / 100
}

// calcHP derives max HP. Nature never modifies HP.
func calcHP(base, iv, ev int) int {
	return statBase(base, iv, ev) + Level + 10
}

// calcStat derives a non-HP battle stat, applying the nature multiplier last
// as the exact ratio num/den (see domain.Nature.Multiplier).
//
// Order matters: the nature scales the finished stat, *after* the +5, not the
// base-and-EV term. Folding it in earlier is the classic way to be off by one
// on most of the roster.
func calcStat(base, iv, ev, natureNum, natureDen int) int {
	return (statBase(base, iv, ev) + 5) * natureNum / natureDen
}

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
	// A Utility Umbrella holder is out of the rain and out of the sun, so Swift
	// Swim and Chlorophyll don't see the weather that would trigger them.
	weather = weatherFor(p, weather)
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
	// A Utility Umbrella on the *defender* removes rain and sun from this
	// exchange. Defender only: canon's onWeatherModifyDamage reads
	// defender.effectiveWeather(), and the sole attacker-side check in the whole
	// chain is Hydro Steam's, which is not in this dataset. So an umbrella
	// holder still gets its own rain-boosted Surf — it is shielded from the
	// weather, not cut off from it.
	weather = weatherFor(def, weather)
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
	// Mold Breaker: the attacker's moves ignore the target's
	// damage-affecting defensive abilities (immunities, damage reduction,
	// crit blocks, Sturdy). Computed once and consulted at each defender
	// ability gate below.
	breakMold := abilityBreaksMold(atk)
	abilityImmune := false
	if mult, override := abilityTypeMultOverride(def, m.Type); override && !breakMold {
		// Smack Down also lifts Levitate vs Ground — skip the
		// ability override in that case so the chart result stands.
		if m.Type != "ground" || !groundedBySmackDown(def) {
			eff = mult
			abilityImmune = (mult == 0)
		}
	}
	// Air Balloon grants the same Ground immunity an ability would. It sits
	// after the ability override so Mold Breaker — which ignores *abilities*,
	// not items — can't punch through it.
	if mult, override := itemTypeMultOverride(def, m.Type); override {
		eff = mult
	}
	// Magnet Rise / Telekinesis grant Ground immunity (canceled by
	// Smack Down via groundImmuneFromVolatile's internal check).
	if m.Type == "ground" && groundImmuneFromVolatile(def) {
		eff = 0
	}
	// Ring Target is the inverse of every immunity above: the holder has given
	// them all up, so a zero becomes a neutral 1. Last, so it lifts the ability
	// and volatile immunities too — canon, and the reason the item is a
	// liability rather than a niche pick.
	if eff == 0 && itemLiftsOwnImmunities(def) {
		eff = 1
		abilityImmune = false
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
	critStage := critStageBonus(atk) + itemCritStage(atk)
	if m.HasFlag("high-crit") {
		critStage++
	}
	critDenom := critChanceDenom(critStage)
	crit := rng.IntN(critDenom) == 0
	if atk.Volatiles.LaserFocus || isAlwaysCrit(m.ID) {
		crit = true
	}
	if abilityBlocksCrit(def) && !breakMold {
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
	if abilityInfiltrator(atk) {
		smult = 1 // Infiltrator ignores Reflect / Light Screen / Aurora Veil.
	}
	abilDef := 1.0
	if !breakMold {
		abilDef = abilityIncomingDamageMult(def, m, eff)
	}
	abilAtk := abilityOutgoingDamageMult(atk, m, def, weather, eff)
	itemAtk := itemOutgoingDamageMult(atk, m, def, weather, eff)
	// A resist berry only softens a hit that actually lands on the holder. A
	// Substitute takes the blow in its place, so the berry neither reduces nor
	// fires — the same predicate dealDamage consults to decide whether to
	// consume it, so the reduction and the consumption stay in lockstep.
	itemDef := itemIncomingDamageMult(atk, def, m, eff)

	dmg := int(math.Floor(base * stab * eff * critMult * randMult * wmult * tmult * smult * abilDef * abilAtk * itemAtk * itemDef))
	if dmg < 1 {
		dmg = 1
	}
	sturdy := false
	if !breakMold {
		dmg, sturdy = abilitySurviveOHKO(def, dmg)
	}
	return DamageResult{Damage: dmg, Crit: crit, Effectiveness: eff, Sturdy: sturdy}
}

// offensiveDefensiveStats picks the (attacker A, defender D) pair the damage
// formula consumes — Atk/Def for physical, SpA/SpD for special — scaled by
// stat stages and modified by burn on the attacker.
//
// Three things can move which stat gets read, and they compose:
//
//   - The move's own override (m.OverrideOffensiveStat /
//     OverrideDefensiveStat). Body Press is a physical move that attacks off
//     the user's Defense; Psystrike and Psyshock are special moves dealt
//     against the target's Defense.
//   - Wonder Room (pw.WonderRoom != nil), which swaps the target's Def and
//     SpD for everyone. It applies to whatever the defensive stat would
//     otherwise have been, so it flips Psystrike onto the target's SpD.
//   - Nothing else. The move's *category* still decides burn's halving, which
//     screen applies, and the rest of the formula — only the stat read here
//     moves. That split is canonical.
//
// Stages travel with the underlying stat rather than the category, so Body
// Press reads the user's Defense stage and Psystrike the target's. Items that
// buff a stat follow it the same way: an Assault Vest is what's being read
// when a hit lands on the holder's SpD, whatever category sent it there.
//
// ignoreDefensive (Chip Away, Darkest Lariat) zeros only positive defensive
// stages; drops still amplify the attacker's damage. Mirrors canonical
// Showdown semantics: "ignore the buff, not the debuff". The clamp is on the
// one stage actually read this turn, so a Physical mover never touches SpD.
func offensiveDefensiveStats(atk, def *Pokemon, m domain.Move, pw *PseudoWeather) (float64, float64) {
	physical := m.Category == domain.CatPhysical

	offSlug := m.OverrideOffensiveStat
	if offSlug == "" {
		if physical {
			offSlug = "attack"
		} else {
			offSlug = "spatk"
		}
	}
	defSlug := m.OverrideDefensiveStat
	if defSlug == "" {
		if physical {
			defSlug = "defense"
		} else {
			defSlug = "spdef"
		}
	}
	if pw != nil && pw.WonderRoom != nil {
		defSlug = swapDefensiveStat(defSlug)
	}

	// Unaware zeros the opponent's stages entirely (buff and debuff alike),
	// distinct from IgnoreDefensive which only clamps positive defensive
	// stages. The attacker's Unaware blanks the defender's defensive stage;
	// the defender's Unaware blanks the attacker's offensive stage — whichever
	// stat each side is reading, which is how canon covers Body Press.
	atkRaw, atkStage := rawStatAndStage(atk, offSlug)
	if abilityIgnoresStages(def) {
		atkStage = 0
	}
	defRaw, defStage := rawStatAndStage(def, defSlug)
	if m.IgnoreDefensive && defStage > 0 {
		defStage = 0
	}
	if abilityIgnoresStages(atk) {
		defStage = 0
	}

	a := float64(atkRaw) * stageMultiplier(atkStage) * itemStatMult(atk, offSlug)
	d := float64(defRaw) * stageMultiplier(defStage) * itemStatMult(def, defSlug)
	// Burn halves the damage of physical moves. It keys off the category, not
	// the stat, so a burned Body Press user is still halved even though the
	// number being halved is its Defense.
	if physical && atk.Status == StatusBurn {
		a *= 0.5
	}
	return a, d
}

// rawStatAndStage returns the unmodified stat value and its current stage for
// one of the four damage-formula stats.
func rawStatAndStage(p *Pokemon, slug string) (int, int) {
	switch slug {
	case "attack":
		return p.Stats.Atk, p.Stages.Atk
	case "defense":
		return p.Stats.Def, p.Stages.Def
	case "spatk":
		return p.Stats.SpA, p.Stages.SpA
	case "spdef":
		return p.Stats.SpD, p.Stages.SpD
	}
	// Unreachable: the data pipeline rejects any override outside these four
	// (parseStatOverride), and the category defaults only produce these.
	panic("offensiveDefensiveStats: unknown stat slug " + slug)
}

// swapDefensiveStat is Wonder Room's exchange: whatever defensive stat the
// formula was about to read, read the other one instead.
func swapDefensiveStat(slug string) string {
	if slug == "defense" {
		return "spdef"
	}
	return "defense"
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
	// Defender only — see computeDamage for why.
	weather = weatherFor(def, weather)
	breakMold := abilityBreaksMold(atk)
	eff := effectivenessWithLifts(dex, m.Type, def, abilityScrappy(atk))
	if mult, override := abilityTypeMultOverride(def, m.Type); override && !breakMold {
		eff = mult
	}
	if mult, override := itemTypeMultOverride(def, m.Type); override {
		eff = mult
	}
	if eff == 0 && itemLiftsOwnImmunities(def) {
		eff = 1
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
	if abilityInfiltrator(atk) {
		smult = 1 // Infiltrator ignores the defender's screens.
	}
	abilDef := 1.0
	if !breakMold {
		abilDef = abilityIncomingDamageMult(def, m, eff)
	}
	abilAtk := abilityOutgoingDamageMult(atk, m, def, weather, eff)
	itemAtk := itemOutgoingDamageMult(atk, m, def, weather, eff)
	// A resist berry is one-shot, but the estimator has no way to know whether
	// it is still held on the turn it is projecting — it reports the halved
	// figure, which is the correct answer for the next hit and one hit stale
	// after that. Overestimating the target's bulk is the safer error for a
	// switch/move score than ignoring the berry entirely.
	itemDef := itemIncomingDamageMult(atk, def, m, eff)
	dmg := int(base * stab * eff * 0.925 * wmult * tmult * smult * abilDef * abilAtk * itemAtk * itemDef)
	if dmg < 1 {
		dmg = 1
	}
	return dmg
}
