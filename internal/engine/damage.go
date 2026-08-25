package engine

import (
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
// Sturdy used to be reported here. It is not any more: it is a survival effect
// and belongs in dealDamage's precedence chain beside Endure and Focus Sash,
// which is where canon puts it (one onDamage event, Endure -10, Sturdy -30,
// Focus Sash -40). computeDamage no longer clamps for it, so the field is gone
// and dealDamage decides.
//
// AbilityImmune is true when the zero-damage result came from an ability's
// TypeMultOverride (Levitate, Volt Absorb, etc.). The caller uses this to
// route the immunity bonus hook (heal / boost) and choose a different log
// line than the plain "doesn't affect" message.
type DamageResult struct {
	Damage        int
	Crit          bool
	Effectiveness float64
	AbilityImmune bool
}

// typeEffectiveness is the whole immunity-and-effectiveness question for one
// move against one defender: the type chart, the lifts that negate parts of
// it, the defender's ability and item overrides, and — for Ground moves —
// groundedness. abilityImmune reports that the zero came from an ability, so
// the caller can let that ability describe itself in the log.
//
// It exists as one function because computeDamage and ExpectedDamage were
// answering it separately and had already drifted apart on Ring Target.
func typeEffectiveness(dex *domain.Dex, atk, def *Pokemon, m domain.Move, pw *PseudoWeather) (eff float64, abilityImmune bool) {
	// Mold Breaker: the attacker's moves ignore the target's damage-affecting
	// defensive abilities (immunities, damage reduction, crit blocks, Sturdy).
	breakMold := abilityBreaksMoldAgainst(atk, def)
	if m.Type == "ground" {
		// A Ground move's immunity is not a type-chart question at all —
		// canon routes it through Pokemon#isGrounded, which is what puts
		// Gravity, Ingrain, Smack Down and an Iron Ball in the same answer as
		// the Flying type, Levitate, Magnet Rise and an Air Balloon. See
		// groundedness in terrain.go; this is its only negateImmunity caller.
		switch groundedness(def, pw, itemLiftsOwnImmunities(def)) {
		case airborneByAbility:
			// Levitate. An ability immunity, so it reports as one — and a
			// mold-breaking attacker walks through it.
			if !breakMold {
				return 0, true
			}
		case airborne:
			return 0, false
		}
		return groundEffectiveness(dex, def, pw), false
	}
	// Foresight / Miracle Eye / Ring Target lift type-chart immunities (Ghost
	// vs Normal/Fighting; Dark vs Psychic; and, for Ring Target, all of them)
	// per defending type inside the lift-aware helper, so the surviving half
	// of a dual typing still decides the matchup.
	eff = effectivenessWithLifts(dex, m.Type, def, abilityScrappy(atk))
	if mult, override := abilityTypeMultOverride(def, m.Type); override && !breakMold {
		eff = mult
		abilityImmune = (mult == 0)
	}
	// An item-granted type immunity sits after the ability override so Mold
	// Breaker — which ignores *abilities*, not items — can't punch through it.
	// Air Balloon's Ground leg is handled by groundedness above, so nothing
	// reaches this on a Ground move today.
	if mult, override := itemTypeMultOverride(def, m.Type); override {
		eff = mult
		abilityImmune = abilityImmune && mult == 0
	}
	return eff, abilityImmune
}

// groundEffectiveness is the type-chart half of a Ground-type move against a
// defender that groundedness has already ruled grounded. It is not
// dex.Effectiveness, for two reasons.
//
// The Flying immunity is gone. Canon sums effectiveness in steps and decides
// immunity separately, so a Flying type that is on the ground contributes
// nothing to the product rather than zeroing it — Earthquake on a
// Gravity-bound Aerodactyl is the Rock half's 2x, not 0 and not 1.
//
// Iron Ball flattens the whole matchup. That is the item's own rule rather
// than a consequence of grounding: upstream's onEffectiveness returns 0 for
// *every* defending type when the holder is Flying, so Ground comes out
// exactly neutral on Rock/Flying and on Bug/Flying alike. It stands down when
// the holder is grounded for a reason that does not need the ball — Ingrain,
// Smack Down or Gravity — which is why that same Aerodactyl reads super
// effective once Gravity is up.
func groundEffectiveness(dex *domain.Dex, def *Pokemon, pw *PseudoWeather) float64 {
	t1, t2 := roostTypes(def)
	flying := t1 == "flying" || t2 == "flying"
	if flying && itemGrounds(def) &&
		!def.Volatiles.Ingrain && !def.Volatiles.SmackDown && (pw == nil || pw.Gravity == nil) {
		return 1
	}
	if t1 == "flying" {
		t1 = ""
	}
	if t2 == "flying" {
		t2 = ""
	}
	return dex.Effectiveness("ground", t1, t2)
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
	// Mold Breaker: the attacker's moves ignore the target's
	// damage-affecting defensive abilities (immunities, damage reduction,
	// crit blocks, Sturdy). Computed once and consulted at each defender
	// ability gate below.
	// Mold Breaker: the attacker's moves ignore the target's
	// damage-affecting defensive abilities (immunities, damage reduction,
	// crit blocks, Sturdy). Computed once and consulted at each defender
	// ability gate below; typeEffectiveness makes its own read of the same
	// flag for the immunity gates.
	breakMold := abilityBreaksMoldAgainst(atk, def)
	eff, abilityImmune := typeEffectiveness(dex, atk, def, m, pw)
	if eff == 0 {
		// `ignore-immunity` is derived from Showdown's ignoreImmunity, which is
		// true by default for every status move and, in this dataset, for
		// exactly one damaging move: Bide. Upstream's release is a synthesized
		// move carrying the flag, so a Ghost cannot wall the stored damage.
		//
		// Scoped to the fixed-damage family deliberately. Those are the only
		// moves whose amount does not read `eff` at all, so letting one past a
		// zero is well defined; letting a formula move past would ask the
		// formula to multiply by nothing.
		if dmg, ok := fixedDamageAmount(atk, def, m); ok && m.HasFlag("ignore-immunity") {
			return DamageResult{Damage: dmg, Effectiveness: 1.0}
		}
		return DamageResult{Effectiveness: 0, AbilityImmune: abilityImmune}
	}
	if m.HasFlag("fixed-damage-level") {
		// Effectiveness reported as 1.0 so the caller doesn't log "super
		// effective" or "resisted" lines on fixed-damage moves.
		return DamageResult{Damage: Level, Effectiveness: 1.0}
	}
	// The rest of Showdown's getDamage prologue: damageCallback moves and the
	// static `damage: <n>` pair. Same position as the flag above and for the
	// same two reasons — below the immunity gate, so a Ghost still walls Super
	// Fang, and above every roll, so none of these moves draws from the RNG.
	// See fixedDamageAmount in callbackmoves.go.
	if dmg, ok := fixedDamageAmount(atk, def, m); ok {
		return DamageResult{Damage: dmg, Effectiveness: 1.0}
	}
	if m.ID == "psywave" {
		return DamageResult{Damage: psywaveDamage(rng), Effectiveness: 1.0}
	}

	a, d := offensiveDefensiveStats(atk, def, m, pw)
	d *= defenseMult(weather, def, m.Category)

	// Charge doubles the base power of an Electric move. It is single-use, and
	// what spends it is any Electric move other than Charge itself — category
	// included, so a Thunder Wave takes it. This comment used to say "cleared
	// after the next damaging move regardless of type (canonical Showdown
	// behavior)", which is the Gen 8 rule and was stated here as if it were
	// current; Gen 9's charge condition clears from onAfterMove keyed on
	// `move.type === 'Electric' && move.id !== 'charge'`. Consumption happens in
	// executeMove's deferred tail; computeDamage only reads the flag.
	power := m.Power
	if atk.Volatiles.Charge && m.Type == "electric" {
		power *= 2
	}
	// Terrain is a base-power modifier, so it is read before the formula runs.
	tmult := terrainDamageMult(terrain, pw, atk, def, m)

	// Showdown's base-damage expression, integer and truncated at every step:
	//
	//   tr(tr(tr(tr(2*L/5 + 2) * bp * A) / D) / 50)
	//
	// The stats truncate first because Showdown's stat modifiers (stages, the
	// sandstorm Sp. Def boost, burn) produce integers before the formula ever
	// sees them. Carrying them as floats into the division is where the old
	// single-floor version accumulated the fraction that pushed rolls past the
	// cartridge maximum.
	ai, di := int(a), int(d)
	if ai < 1 {
		ai = 1
	}
	if di < 1 {
		di = 1
	}
	// Base-power group. Terrain lives here rather than in the final modifiers:
	// canon's terrains hook onBasePower (Electric Terrain x1.3 on Electric
	// moves, Grassy x0.5 on Earthquake), so they round against base power, not
	// against the finished damage figure.
	bp := applyMod(power, toMod(tmult))
	if bp < 1 {
		bp = 1
	}
	base := (2*Level/5 + 2) * bp * ai / di / 50

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

	// Kept as the same draw, consumed as an integer percent: Showdown's
	// randomizer is tr(tr(dmg * roll) / 100), not a float multiply.
	randRoll := rng.Range(85, 100)
	wmult := damageMultByType(weather, m.Type)
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

	// The modifier chain, in Showdown's order and with its rounding. Each step
	// truncates, so the intermediate figure is always a whole number of HP —
	// which is what makes a roll above the cartridge maximum impossible rather
	// than merely unlikely.
	dmg := base + 2

	// Weather is its own group, ahead of the crit multiplier.
	dmg = applyMod(dmg, toMod(wmult))

	// Crit is a bare truncated multiply, not a 4096 modifier.
	if critMult != 1 {
		dmg = int(float64(dmg) * critMult)
	}

	// Randomizer: tr(tr(dmg * r) / 100) with r in 85..100. It is still the same
	// rng.Range(85, 100) draw it always was, so the RNG stream is byte-identical
	// to before this change — only the arithmetic downstream of the draw moved.
	dmg = dmg * randRoll / 100

	// STAB, then the type chart as integer doublings and halvings.
	dmg = applyMod(dmg, toMod(stab))
	dmg = applyTypeEffectiveness(dmg, eff)

	// Final-modifier group. Showdown chains every ModifyDamage handler into one
	// modifier and applies it once, so screens and a resist berry on the same
	// hit round together rather than one after the other.
	//
	// Fidelity gap worth naming: this engine exposes ability and item damage
	// influence as a single lumped multiplier per side (OutgoingDamageMult /
	// IncomingDamageMult), so all four land in this group. Canon splits them —
	// Technician and the type-boost items are base-power handlers, Huge Power
	// modifies Attack — and separating them means reworking the hook interface
	// per ability, not reordering this function. The defensive ones that
	// dominate real damage (Multiscale, Solid Rock, Filter, the resist berries,
	// Life Orb, Expert Belt) are genuinely final-group in canon, so this is the
	// least-wrong single home for the lump.
	final := modScale
	final = chainMod(final, toMod(smult))
	final = chainMod(final, toMod(abilDef))
	final = chainMod(final, toMod(abilAtk))
	final = chainMod(final, toMod(itemAtk))
	final = chainMod(final, toMod(itemDef))
	dmg = applyMod(dmg, final)

	if dmg < 1 {
		dmg = 1
	}
	// Sturdy is deliberately NOT clamped here. It is a survival effect, and
	// dealDamage already orders those against each other — Endure first, then
	// the sash, with a comment on why a sash is not burned on a hit Endure
	// already saved. Clamping at the end of this function put Sturdy upstream of
	// that chain, so by the time dealDamage ran, the damage was already capped
	// at HP-1 and Endure's `dmg >= def.HP` test was false: the Pokemon survived
	// either way but announced the wrong effect. Canon settles all three in one
	// onDamage event, in priority order — Endure at -10, Sturdy at -30, Focus
	// Sash at -40 — which is exactly the chain in dealDamage.
	return DamageResult{Damage: dmg, Crit: crit, Effectiveness: eff}
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
	// Wonder Room swaps the raw defensive stats, and only those. Upstream does
	// it inside Pokemon#calculateStat ahead of everything else: the stored Def
	// and SpD trade places, while the stat *stage* and the stat-modifying
	// *item* stay attached to the stat the move's category actually named.
	//
	// This engine used to swap the slug and then read all three off the
	// swapped name, which came out exactly inverted — Defense Curl protected
	// against special hits and an Assault Vest against physical ones. Measured
	// against a physical hit under Wonder Room: 48 plain, 44 after Defense
	// Curl, 32 with an Assault Vest, where canon wants 48, 32, 48.
	//
	// The one thing that does move by name is a move's *offensive* override.
	// The condition's onModifyMove flips Body Press's `def` to `spd`, so under
	// Wonder Room Body Press reads the raw Defense — which the swap has put in
	// the spd slot — with the Sp. Def stage. That is what the swap of offSlug
	// below expresses, and it is why the raw read has to go through
	// rawStatUnderWonderRoom rather than being pre-swapped here.
	wonderRoom := pw != nil && pw.WonderRoom != nil
	if wonderRoom {
		offSlug = swapDefensiveStat(offSlug)
	}

	// Unaware zeros the opponent's stages entirely (buff and debuff alike),
	// distinct from IgnoreDefensive which only clamps positive defensive
	// stages. The attacker's Unaware blanks the defender's defensive stage;
	// the defender's Unaware blanks the attacker's offensive stage — whichever
	// stat each side is reading, which is how canon covers Body Press.
	//
	// The defender's Unaware is a defensive ability like any other, so a
	// mold-breaking attacker ignores it. The attacker's own is untouched —
	// Mold Breaker suppresses the target's abilities, never its own.
	_, atkStage := rawStatAndStage(atk, offSlug)
	atkRaw := rawStatUnderWonderRoom(atk, offSlug, wonderRoom)
	if abilityIgnoresStages(def) && !abilityBreaksMoldAgainst(atk, def) {
		atkStage = 0
	}
	_, defStage := rawStatAndStage(def, defSlug)
	defRaw := rawStatUnderWonderRoom(def, defSlug, wonderRoom)
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
	if burnHalvesAttack(atk, m) {
		a *= 0.5
	}
	return a, d
}

// burnHalvesAttack reports whether burn's physical-damage halve applies to
// this attack. Facade is the canon exception: it ignores the drop outright and
// doubles its base power off the burn instead (see statusDoublingMoves).
//
// Guts consults this same predicate rather than testing the burn itself,
// because Guts compensates for the halve by multiplying it back out — if the
// two disagreed about when the halve happened, a burned Guts Facade would be
// multiplied back out of a reduction that was never applied.
func burnHalvesAttack(atk *Pokemon, m domain.Move) bool {
	return m.Category == domain.CatPhysical && atk.Status == StatusBurn && m.ID != "facade"
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

// swapDefensiveStat is Wonder Room's exchange: Def becomes SpD and SpD becomes
// Def. Anything else is left alone — a move whose offensive stat is Attack or
// Sp. Atk is not a defensive stat and Wonder Room has nothing to say about it.
// (This used to return "defense" for every non-"defense" input, which was
// harmless while the only caller passed a defensive slug and wrong the moment
// the offensive override started coming through here.)
func swapDefensiveStat(slug string) string {
	switch slug {
	case "defense":
		return "spdef"
	case "spdef":
		return "defense"
	}
	return slug
}

// rawStatUnderWonderRoom returns the raw stat the damage formula should read
// for slug. Under Wonder Room the two defensive stores trade places, so a read
// of "defense" gets the stored Sp. Def and vice versa; the offensive stats are
// untouched. Split out from rawStatAndStage because the swap applies to the
// stored number alone — see the note in offensiveDefensiveStats.
func rawStatUnderWonderRoom(p *Pokemon, slug string, wonderRoom bool) int {
	if wonderRoom {
		slug = swapDefensiveStat(slug)
	}
	raw, _ := rawStatAndStage(p, slug)
	return raw
}

// downloadDefensiveScores returns the two figures Download compares when it
// picks an offense: the target's Defense and Special Defense as canon reads
// them, which is the raw stat with the stat *stage* applied and no item or
// ability modifiers — upstream's getStat(stat, unboosted=false,
// unmodified=true).
//
// Wonder Room's part in this is the strange one, and upstream comments on it
// in place: getStat renames the stat *after* reading the raw value, so
// Download keeps looking at the unswapped raw numbers while picking up the
// other stat's stage. A +2 Sp. Def under Wonder Room therefore reads to
// Download as a Defense buff. Which is the whole content of the upstream case
// named "should be ignored by Download when determining raw stats, but not
// stat stage changes".
func downloadDefensiveScores(foe *Pokemon, pw *PseudoWeather) (defScore, spdScore float64) {
	defRaw, defStage := rawStatAndStage(foe, "defense")
	spdRaw, spdStage := rawStatAndStage(foe, "spdef")
	if pw != nil && pw.WonderRoom != nil {
		defStage, spdStage = spdStage, defStage
	}
	return float64(defRaw) * stageMultiplier(defStage),
		float64(spdRaw) * stageMultiplier(spdStage)
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
	breakMold := abilityBreaksMoldAgainst(atk, def)
	// Pseudo-weather is not threaded into the AI's estimator (see the note
	// on the stat read below), so Gravity is invisible here: the AI still
	// reads a Flying-type as immune to Ground while Gravity is up.
	eff, _ := typeEffectiveness(dex, atk, def, m, nil)
	if eff == 0 {
		return 0
	}
	if m.HasFlag("fixed-damage-level") {
		return Level
	}
	if dmg, ok := fixedDamageAmount(atk, def, m); ok {
		return dmg
	}
	if m.ID == "psywave" {
		// The estimator is deliberately RNG-free, so it answers with the
		// midpoint of the 50..150 spread rather than drawing one.
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
	// Terrain is a base-power modifier, so it is read before the formula runs.
	tmult := terrainDamageMult(terrain, nil, atk, def, m)

	// Showdown's base-damage expression, integer and truncated at every step:
	//
	//   tr(tr(tr(tr(2*L/5 + 2) * bp * A) / D) / 50)
	//
	// The stats truncate first because Showdown's stat modifiers (stages, the
	// sandstorm Sp. Def boost, burn) produce integers before the formula ever
	// sees them. Carrying them as floats into the division is where the old
	// single-floor version accumulated the fraction that pushed rolls past the
	// cartridge maximum.
	ai, di := int(a), int(d)
	if ai < 1 {
		ai = 1
	}
	if di < 1 {
		di = 1
	}
	// Base-power group. Terrain lives here rather than in the final modifiers:
	// canon's terrains hook onBasePower (Electric Terrain x1.3 on Electric
	// moves, Grassy x0.5 on Earthquake), so they round against base power, not
	// against the finished damage figure.
	bp := applyMod(power, toMod(tmult))
	if bp < 1 {
		bp = 1
	}
	base := (2*Level/5 + 2) * bp * ai / di / 50
	stab := 1.0
	if m.Type != "" && (m.Type == atk.Type1 || m.Type == atk.Type2) {
		stab = 1.5
	}
	wmult := damageMultByType(weather, m.Type)
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
	// Same chain, same order, same rounding as computeDamage — an estimator
	// that rounds differently from the engine it is estimating is worse than
	// no estimator, because the error is systematic rather than noisy. The
	// only difference is the roll: 92.5%, the midpoint of 85..100, applied as
	// tr(dmg * 925 / 1000) so it truncates like a real roll would.
	dmg := base + 2
	dmg = applyMod(dmg, toMod(wmult))
	dmg = dmg * 925 / 1000
	dmg = applyMod(dmg, toMod(stab))
	dmg = applyTypeEffectiveness(dmg, eff)

	final := modScale
	final = chainMod(final, toMod(smult))
	final = chainMod(final, toMod(abilDef))
	final = chainMod(final, toMod(abilAtk))
	final = chainMod(final, toMod(itemAtk))
	final = chainMod(final, toMod(itemDef))
	dmg = applyMod(dmg, final)

	if dmg < 1 {
		dmg = 1
	}
	return dmg
}

// --- Showdown's fixed-point modifier chain ---
//
// Showdown does not carry damage as a real number. It carries an integer and
// truncates it at every documented boundary, and it expresses every multiplier
// as a fraction over 4096. That is not an implementation detail: the
// truncations are load-bearing, and a formula that keeps full precision to the
// end and floors once lands one or two points high often enough to cross a KO
// threshold. This engine used to do exactly that — a single math.Floor over the
// whole product — and produced rolls above the cartridge maximum (an Air Slash
// rolled 86 on a Gengar whose maximum is 85).
//
// modScale is the denominator. A multiplier of 1.5 is 6144; ×0.5 is 2048.
const modScale = 4096

// toMod converts a float multiplier into Showdown's 4096-denominator fixed
// point, rounding to nearest.
//
// Nearest rather than truncating, deliberately. Showdown's handlers pass exact
// fractions — Electric Terrain is `chainModify([5325, 4096])`, not
// `modify(x, 1.3)` — and 1.3 × 4096 is 5324.8, so truncating lands on 5324 and
// is one unit light against every published constant that is not an exact
// multiple of 4096. Rounding reproduces them: 1.3 → 5325, 1.1 → 4506,
// 1.2 → 4915, 0.9 → 3686. The exact ones (1.5 → 6144, 0.75 → 3072, 0.5 → 2048)
// are unaffected either way.
func toMod(f float64) int {
	return int(f*modScale + 0.5)
}

// chainMod composes two modifiers the way Showdown's chainModify does: the
// product is taken in 4096 space and rounded half-up at 2048 before being
// carried on. Chaining and then applying once is *not* the same as applying
// twice, which is why the final-modifier group below chains first.
func chainMod(a, b int) int {
	return (a*b + 2048) >> 12
}

// applyMod applies a modifier to an integer damage figure the way Showdown's
// modify does:
//
//	tr((tr(value * num) + 2048 - 1) / 4096)
//
// The bias is 2047 — half of 4096, minus one — which rounds half *down*. Not
// 4095: that would round almost everything up and put an off-by-one in every
// damage roll in the game. The two constants are easy to transpose because
// both are "one less than a power of two" and both look plausible next to a
// >> 12, so the reference test spells the arithmetic out rather than sharing
// this function.
func applyMod(v, mod int) int {
	return (v*mod + 2048 - 1) >> 12
}

// applyTypeEffectiveness applies the type multiplier the way Showdown does:
// as repeated integer doublings and halvings, each truncated, rather than one
// multiply by 0.25/0.5/2/4. Halving 99 twice gives 24, not 24.75 floored to
// 24 — the same answer here, but not at every input, and the difference
// compounds through the modifiers that follow.
//
// Abilities and items can override the chart with a factor that is not a power
// of two (there are none in the current dataset, but the hook allows it), so
// whatever is left after the power-of-two steps is applied as an ordinary
// modifier rather than silently dropped.
func applyTypeEffectiveness(dmg int, eff float64) int {
	for eff >= 2 {
		dmg *= 2
		eff /= 2
	}
	for eff > 0 && eff <= 0.5 {
		dmg /= 2
		eff *= 2
	}
	if eff != 1 {
		dmg = applyMod(dmg, toMod(eff))
	}
	return dmg
}
