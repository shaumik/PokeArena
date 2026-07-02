package engine

import (
	"fmt"

	"pokearena/internal/domain"
)

// AbilityKind identifies a Pokémon's ability by slug (lowercase kebab-case,
// matching domain.Species.Abilities). The empty string means no ability is
// set (older data without the abilities field); empty disables every hook.
//
// The registry shape is a struct of optional hook functions per ability —
// the count grew past where a switch-per-hook stays readable, so each
// ability is now one entry that declares only the hooks it implements.
// Unimplemented abilities are absent from the registry; lookup returns nil
// and every dispatcher no-ops.
type AbilityKind string

const (
	AbilityNone AbilityKind = ""

	// Batch 1 (#30 step 4 first commit).
	AbilityIntimidate AbilityKind = "intimidate"
	AbilitySturdy     AbilityKind = "sturdy"
	AbilityLevitate   AbilityKind = "levitate"
	AbilityThickFat   AbilityKind = "thick-fat"
)

// Ability is the registry record for one ability. Every field is optional;
// only set the hooks the ability actually participates in. Dispatchers (see
// the funcs below the registry) handle nil-checks so call sites stay tight.
//
// Hook timing reference:
//
//	OnSwitchIn         — after the new active is installed (doSwitch) and on turn-1 leads
//	OnSwitchOut        — on the outgoing Pokémon, before stages/volatiles are reset
//	TypeMultOverride   — first thing in computeDamage / ExpectedDamage; replaces the type chart
//	OnImmunityBonus    — fires when TypeMultOverride returned (0, true) for an incoming hit
//	IncomingDamageMult — in computeDamage's multiplier chain (defender)
//	OutgoingDamageMult — in computeDamage's multiplier chain (attacker)
//	SurviveOHKO        — post-formula damage cap (defender side); returns (cappedDamage, fired)
//	AccuracyMult       — applied to the attacker's accuracy roll
//	BlockCrit          — if true, defender takes no crits
//	BlockSecondaries   — if true, attacker's secondaries against this defender can't fire
//	BlockOwnSecondaries — if true, attacker's own secondaries are suppressed (Sheer Force)
//	BlocksStatus       — return true to refuse a status infliction (defender side)
//	BlocksFlinch       — if true, defender immune to flinch
//	OnHit              — fires after damage applies; defender's ability reacts to attacker (contact riders)
//	BlocksStatLowerByFoe / OnStatLoweredByFoe — applyStages consults these on foe-induced drops
//	SpeedMult          — multiplier applied in effectiveSpeed (weather-speed boosters, Quick Feet)
//	SuppressWeather    — true on either active Pokémon makes effectiveWeather return nil
//	BlocksIndirectDamage — Magic Guard: skip residual chip (poison, burn, toxic, sand, recoil)
//	EndOfTurn          — fires after weather residual + weather tick
type Ability struct {
	Kind AbilityKind

	OnSwitchIn  func(s *BattleState, side int, log *[]LogLine)
	OnSwitchOut func(p *Pokemon, side int, log *[]LogLine)

	TypeMultOverride   func(atkType domain.Type) (mult float64, override bool)
	OnImmunityBonus    func(s *BattleState, side int, atkType domain.Type, log *[]LogLine)
	IncomingDamageMult func(m domain.Move, def *Pokemon, typeEff float64) float64
	OutgoingDamageMult func(atk *Pokemon, m domain.Move, def *Pokemon, weather *WeatherState, typeEff float64) float64
	SurviveOHKO        func(def *Pokemon, damage int) (int, bool)

	AccuracyMult        float64
	BlockCrit           bool
	BlockSecondaries    bool
	BlockOwnSecondaries bool

	// BlocksInfatuation / BlocksTaunt refuse the matching volatile outright
	// (Oblivious). Consulted by the Attract and Taunt volatile handlers.
	BlocksInfatuation bool
	BlocksTaunt       bool

	BlocksStatus func(s StatusCond) bool
	// BlocksStatusState is the weather/field-aware status guard (Leaf Guard
	// refuses status under sun). Consulted alongside BlocksStatus; use this
	// variant when the decision needs the battle state, not just the status.
	BlocksStatusState func(s *BattleState, def *Pokemon, st StatusCond) bool
	BlocksFlinch      bool

	// DrainBackfires turns the holder's drained HP into damage on the
	// drainer instead of healing (Liquid Ooze).
	DrainBackfires bool

	// BlocksRecoil makes the holder immune to its own move recoil without
	// touching other indirect damage (Rock Head — narrower than Magic Guard).
	BlocksRecoil bool

	// MaxesMultihit makes the holder's multi-strike moves always hit the
	// maximum number of times (Skill Link — Bullet Seed always hits 5).
	MaxesMultihit bool

	// ExertsPressure makes every foe move that targets this Pokémon cost one
	// extra PP (Pressure). Consulted at PP-payment time in executeMove.
	ExertsPressure bool

	// Synchronizes bounces a foe-inflicted burn / poison / toxic / paralysis
	// back onto the Pokémon that caused it (Synchronize). Consulted by the
	// inflictStatusFrom path; sleep and freeze never bounce.
	Synchronizes bool

	// SecondaryChanceMult scales the holder's added-effect (secondary)
	// chances on damaging moves (Serene Grace = 2). Zero means "unset" and
	// the dispatcher treats it as 1.
	SecondaryChanceMult float64

	// IgnoresOpponentStages makes the damage formula treat the foe's stat
	// stages as zero — both when this Pokémon attacks (foe's defensive
	// boosts ignored) and when it defends (attacker's offensive boosts
	// ignored). Unaware.
	IgnoresOpponentStages bool
	OnFlinched            func(p *Pokemon, side int, log *[]LogLine)
	// OnHit fires on the defender after a damaging hit lands. hitSub is true
	// when a substitute absorbed the blow: contact riders (Static, Flame Body)
	// still fire through a sub, but reactive-defense abilities (Justified,
	// Weak Armor) check !hitSub since their holder wasn't actually struck.
	OnHit func(s *BattleState, defSide int, m domain.Move, hitSub bool, rng *RNG, log *[]LogLine)

	// OnDealDamage fires on the attacker after its damaging move connects with
	// the real target (not a substitute). It lets the attacker's ability add its
	// own rider to its moves — Poison Touch's 30% contact poison — independent
	// of the move's own secondaries (so Shield Dust doesn't suppress it).
	OnDealDamage func(s *BattleState, atkSide int, m domain.Move, rng *RNG, log *[]LogLine)

	BlocksStatLowerByFoe func(stat string) bool
	OnStatLoweredByFoe   func(p *Pokemon, side int, stat string, log *[]LogLine)

	// OnKO fires on the attacker after its damaging move faints the foe
	// (Moxie raises Attack). side is the attacker's side; the dispatcher
	// only calls it while the attacker is still alive.
	OnKO func(s *BattleState, side int, log *[]LogLine)

	// OnCrit fires on the defender when it takes a critical hit that a
	// substitute did not absorb (Anger Point maxes Attack).
	OnCrit func(s *BattleState, defSide int, log *[]LogLine)

	// OnFaint fires on a Pokémon as it faints to an opposing move; atkSide is
	// its killer and m is the finishing move (Aftermath chips a contact
	// attacker for 1/4 of its max HP).
	OnFaint func(s *BattleState, faintedSide, atkSide int, m domain.Move, log *[]LogLine)

	SpeedMult func(p *Pokemon, weather *WeatherState) float64

	SuppressWeather      bool
	BlocksIndirectDamage bool

	EndOfTurn func(s *BattleState, side int, rng *RNG, log *[]LogLine)
}

// abilityRegistry is the lookup table from slug → ability spec. The
// dataset can carry every Showdown ability slug; only those present here
// fire hooks. Adding an ability = adding an entry; integration sites
// don't need to change once the matching hook surface exists.
//
// Populated in init() (not a var literal) because some hook closures call
// back through dispatchers that read this map — a var literal would cycle.
var abilityRegistry map[AbilityKind]*Ability

func init() {
	abilityRegistry = map[AbilityKind]*Ability{
		AbilityIntimidate: {
			Kind: AbilityIntimidate,
			OnSwitchIn: func(s *BattleState, side int, log *[]LogLine) {
				user := s.Active(side)
				foeSide := 1 - side
				foe := s.Active(foeSide)
				if foe.Fainted {
					return
				}
				*log = append(*log, LogLine{
					Type: "ability", Side: side,
					Text: fmt.Sprintf("%s's Intimidate cuts %s's Attack!", user.Name, foe.Name),
				})
				applyStagesFromFoe(foe, foeSide, "attack", -1, s, log)
			},
		},
		AbilitySturdy: {
			Kind: AbilitySturdy,
			SurviveOHKO: func(def *Pokemon, damage int) (int, bool) {
				if def.HP != def.MaxHP || damage < def.HP {
					return damage, false
				}
				return def.HP - 1, true
			},
		},
		"pressure": {
			// Every foe move aimed at the holder costs an extra PP. Announced on
			// entry the way canon does; the PP drain itself is applied at
			// PP-payment time in executeMove via ExertsPressure.
			Kind:           "pressure",
			ExertsPressure: true,
			OnSwitchIn: func(s *BattleState, side int, log *[]LogLine) {
				p := s.Active(side)
				*log = append(*log, LogLine{
					Type: "ability", Side: side,
					Text: fmt.Sprintf("%s is exerting its Pressure!", p.Name),
				})
			},
		},
		"synchronize": {
			// Bounces a foe-inflicted burn / poison / toxic / paralysis back onto
			// the source. The reflection itself lives in inflictStatusFrom.
			Kind:         "synchronize",
			Synchronizes: true,
		},
		AbilityLevitate: {
			Kind: AbilityLevitate,
			TypeMultOverride: func(atkType domain.Type) (float64, bool) {
				if atkType == "ground" {
					return 0, true
				}
				return 1, false
			},
		},
		AbilityThickFat: {
			Kind: AbilityThickFat,
			IncomingDamageMult: func(m domain.Move, def *Pokemon, typeEff float64) float64 {
				if m.Type == "fire" || m.Type == "ice" {
					return 0.5
				}
				return 1
			},
		},

		// --- weather setters: switch in, install field weather (5 turns) ---
		"drought": {Kind: "drought", OnSwitchIn: func(s *BattleState, side int, log *[]LogLine) {
			setWeatherFromAbility(s, side, WeatherSun, log)
		}},
		"drizzle": {Kind: "drizzle", OnSwitchIn: func(s *BattleState, side int, log *[]LogLine) {
			setWeatherFromAbility(s, side, WeatherRain, log)
		}},
		"sand-stream": {Kind: "sand-stream", OnSwitchIn: func(s *BattleState, side int, log *[]LogLine) {
			setWeatherFromAbility(s, side, WeatherSandstorm, log)
		}},
		"snow-warning": {Kind: "snow-warning", OnSwitchIn: func(s *BattleState, side int, log *[]LogLine) {
			setWeatherFromAbility(s, side, WeatherSnow, log)
		}},

		// --- absorb-style type immunities (block a type + bonus on absorb) ---
		"volt-absorb": {
			Kind: "volt-absorb",
			TypeMultOverride: func(t domain.Type) (float64, bool) {
				if t == "electric" {
					return 0, true
				}
				return 1, false
			},
			OnImmunityBonus: func(s *BattleState, side int, t domain.Type, log *[]LogLine) {
				absorbAndHeal(s, side, t, "electric", 0.25, "Volt Absorb", log)
			},
		},
		"water-absorb": {
			Kind: "water-absorb",
			TypeMultOverride: func(t domain.Type) (float64, bool) {
				if t == "water" {
					return 0, true
				}
				return 1, false
			},
			OnImmunityBonus: func(s *BattleState, side int, t domain.Type, log *[]LogLine) {
				absorbAndHeal(s, side, t, "water", 0.25, "Water Absorb", log)
			},
		},
		"flash-fire": {
			Kind: "flash-fire",
			TypeMultOverride: func(t domain.Type) (float64, bool) {
				if t == "fire" {
					return 0, true
				}
				return 1, false
			},
			OnImmunityBonus: func(s *BattleState, side int, t domain.Type, log *[]LogLine) {
				p := s.Active(side)
				if !p.Volatiles.FlashFireCharged {
					p.Volatiles.FlashFireCharged = true
					*log = append(*log, LogLine{
						Type: "ability", Side: side,
						Text: fmt.Sprintf("%s's Flash Fire raised its Fire power!", p.Name),
					})
				}
			},
			OutgoingDamageMult: func(atk *Pokemon, m domain.Move, def *Pokemon, w *WeatherState, typeEff float64) float64 {
				if atk.Volatiles.FlashFireCharged && m.Type == "fire" {
					return 1.5
				}
				return 1
			},
		},
		"lightning-rod": {
			Kind: "lightning-rod",
			TypeMultOverride: func(t domain.Type) (float64, bool) {
				if t == "electric" {
					return 0, true
				}
				return 1, false
			},
			OnImmunityBonus: func(s *BattleState, side int, t domain.Type, log *[]LogLine) {
				absorbAndBoost(s, side, t, "electric", "spatk", "Lightning Rod", log)
			},
		},
		"storm-drain": {
			Kind: "storm-drain",
			TypeMultOverride: func(t domain.Type) (float64, bool) {
				if t == "water" {
					return 0, true
				}
				return 1, false
			},
			OnImmunityBonus: func(s *BattleState, side int, t domain.Type, log *[]LogLine) {
				absorbAndBoost(s, side, t, "water", "spatk", "Storm Drain", log)
			},
		},
		"sap-sipper": {
			Kind: "sap-sipper",
			TypeMultOverride: func(t domain.Type) (float64, bool) {
				if t == "grass" {
					return 0, true
				}
				return 1, false
			},
			OnImmunityBonus: func(s *BattleState, side int, t domain.Type, log *[]LogLine) {
				absorbAndBoost(s, side, t, "grass", "attack", "Sap Sipper", log)
			},
		},
		"motor-drive": {
			Kind: "motor-drive",
			TypeMultOverride: func(t domain.Type) (float64, bool) {
				if t == "electric" {
					return 0, true
				}
				return 1, false
			},
			OnImmunityBonus: func(s *BattleState, side int, t domain.Type, log *[]LogLine) {
				absorbAndBoost(s, side, t, "electric", "speed", "Motor Drive", log)
			},
		},

		// --- damage modifiers (mostly attacker-side outgoing) ---
		// Sniper turns ×1.5 crits into ×2.25. Implemented inline in
		// computeDamage because the multiplier hook can't see crit-ness;
		// this empty registry entry documents the dispatch path.
		"sniper": {Kind: "sniper"},
		"filter": {
			Kind: "filter",
			IncomingDamageMult: func(m domain.Move, def *Pokemon, typeEff float64) float64 {
				if typeEff > 1 {
					return 0.75
				}
				return 1
			},
		},
		"multiscale": {
			Kind: "multiscale",
			IncomingDamageMult: func(m domain.Move, def *Pokemon, typeEff float64) float64 {
				if def.HP == def.MaxHP {
					return 0.5
				}
				return 1
			},
		},
		"technician": {
			Kind: "technician",
			OutgoingDamageMult: func(atk *Pokemon, m domain.Move, def *Pokemon, w *WeatherState, typeEff float64) float64 {
				if m.Power > 0 && m.Power <= 60 {
					return 1.5
				}
				return 1
			},
		},
		"tinted-lens": {
			Kind: "tinted-lens",
			OutgoingDamageMult: func(atk *Pokemon, m domain.Move, def *Pokemon, w *WeatherState, typeEff float64) float64 {
				if typeEff > 0 && typeEff < 1 {
					return 2
				}
				return 1
			},
		},
		"reckless": {
			Kind: "reckless",
			OutgoingDamageMult: func(atk *Pokemon, m domain.Move, def *Pokemon, w *WeatherState, typeEff float64) float64 {
				if m.Self != nil && m.Self.Recoil > 0 {
					return 1.2
				}
				return 1
			},
		},
		"iron-fist": {
			Kind: "iron-fist",
			OutgoingDamageMult: func(atk *Pokemon, m domain.Move, def *Pokemon, w *WeatherState, typeEff float64) float64 {
				if m.HasFlag("punch") {
					return 1.2
				}
				return 1
			},
		},
		"hustle": {
			Kind:         "hustle",
			AccuracyMult: 0.8,
			OutgoingDamageMult: func(atk *Pokemon, m domain.Move, def *Pokemon, w *WeatherState, typeEff float64) float64 {
				if m.Category == domain.CatPhysical {
					return 1.5
				}
				return 1
			},
		},
		"compound-eyes": {
			Kind:         "compound-eyes",
			AccuracyMult: 1.3,
		},
		"analytic": {
			// Fires when the attacker is the last scheduled mover this turn —
			// set by ResolveTurn on the last entry of the ordered-movers slice
			// (also true when the foe switched, since the move resolves alone
			// after the switch). Read here as Volatiles.MovedLast.
			Kind: "analytic",
			OutgoingDamageMult: func(atk *Pokemon, m domain.Move, def *Pokemon, w *WeatherState, typeEff float64) float64 {
				if atk.Volatiles.MovedLast {
					return 1.3
				}
				return 1
			},
		},
		"sheer-force": {
			// Moves with a secondary effect deal 1.3× damage but the
			// secondary is suppressed (paired trade). applyDamageEffects
			// reads BlockOwnSecondaries to skip the m.Secondaries loop;
			// m.Self is untouched so recoil / self-debuffs still apply.
			Kind:                "sheer-force",
			BlockOwnSecondaries: true,
			OutgoingDamageMult: func(atk *Pokemon, m domain.Move, def *Pokemon, w *WeatherState, typeEff float64) float64 {
				if len(m.Secondaries) > 0 {
					return 1.3
				}
				return 1
			},
		},
		"solar-power": {
			Kind: "solar-power",
			OutgoingDamageMult: func(atk *Pokemon, m domain.Move, def *Pokemon, w *WeatherState, typeEff float64) float64 {
				if w != nil && w.Kind == WeatherSun && m.Category == domain.CatSpecial {
					return 1.5
				}
				return 1
			},
			EndOfTurn: func(s *BattleState, side int, _ *RNG, log *[]LogLine) {
				if w := effectiveWeather(s); w != nil && w.Kind == WeatherSun {
					chipFraction(s.Active(side), side, 1.0/8, "Solar Power", log)
				}
			},
		},

		// --- switch-in offense pick: Download ---
		"download": {
			// On entry, raise Atk if the foe's Defense is lower than its
			// Sp. Def, otherwise raise Sp. Atk — pick the offense the foe is
			// worse at. Uses raw defensive stats (foes rarely carry boosts at
			// the moment a fresh mon switches in).
			Kind: "download",
			OnSwitchIn: func(s *BattleState, side int, log *[]LogLine) {
				foe := s.Active(1 - side)
				if foe.Fainted {
					return
				}
				p := s.Active(side)
				stat, label := "spatk", "Sp. Atk"
				if foe.Stats.Def < foe.Stats.SpD {
					stat, label = "attack", "Attack"
				}
				*log = append(*log, LogLine{
					Type: "ability", Side: side,
					Text: fmt.Sprintf("%s's Download raised its %s!", p.Name, label),
				})
				applyStages(p, side, stat, 1, log)
			},
		},

		// --- weather-conditional offense: Sand Force ---
		// ×1.3 to Rock / Ground / Steel moves while a sandstorm rages. (The
		// holder's sand-chip immunity isn't modeled here; Sand Force users are
		// Ground/Rock/Steel types that already ignore sandstorm chip.)
		"sand-force": {
			Kind: "sand-force",
			OutgoingDamageMult: func(atk *Pokemon, m domain.Move, def *Pokemon, w *WeatherState, typeEff float64) float64 {
				if w != nil && w.Kind == WeatherSandstorm &&
					(m.Type == "rock" || m.Type == "ground" || m.Type == "steel") {
					return 1.3
				}
				return 1
			},
		},

		// --- pinch abilities: ×1.5 to a fixed move type at ≤ 1/3 HP ---
		"blaze":    {Kind: "blaze", OutgoingDamageMult: pinchBoost("fire")},
		"torrent":  {Kind: "torrent", OutgoingDamageMult: pinchBoost("water")},
		"overgrow": {Kind: "overgrow", OutgoingDamageMult: pinchBoost("grass")},
		"swarm":    {Kind: "swarm", OutgoingDamageMult: pinchBoost("bug")},

		// --- status-immunity guards ---
		"immunity":     {Kind: "immunity", BlocksStatus: func(st StatusCond) bool { return st == StatusPoison || st == StatusToxic }},
		"limber":       {Kind: "limber", BlocksStatus: func(st StatusCond) bool { return st == StatusParalysis }},
		"water-veil":   {Kind: "water-veil", BlocksStatus: func(st StatusCond) bool { return st == StatusBurn }},
		"magma-armor":  {Kind: "magma-armor", BlocksStatus: func(st StatusCond) bool { return st == StatusFreeze }},
		"insomnia":     {Kind: "insomnia", BlocksStatus: func(st StatusCond) bool { return st == StatusSleep }},
		"vital-spirit": {Kind: "vital-spirit", BlocksStatus: func(st StatusCond) bool { return st == StatusSleep }},
		"sweet-veil":   {Kind: "sweet-veil", BlocksStatus: func(st StatusCond) bool { return st == StatusSleep }},
		"own-tempo":    {Kind: "own-tempo" /* blocks confusion; volatile guard land elsewhere */},
		"leaf-guard": {
			// Refuses every major status while the sun is up (harsh sunlight
			// in canon; we have one sun tier). Weather-aware, so it uses the
			// state-carrying guard rather than the plain BlocksStatus.
			Kind: "leaf-guard",
			BlocksStatusState: func(s *BattleState, def *Pokemon, st StatusCond) bool {
				w := effectiveWeather(s)
				return w != nil && w.Kind == WeatherSun
			},
		},
		"inner-focus": {Kind: "inner-focus", BlocksFlinch: true},
		"shield-dust": {Kind: "shield-dust", BlockSecondaries: true},
		"oblivious": {
			// Immune to infatuation and Taunt (and, in canon, Intimidate's
			// drop — not modeled here). The guards live in the Attract and
			// Taunt volatile handlers, which consult BlocksInfatuation /
			// BlocksTaunt before setting their flag.
			Kind:              "oblivious",
			BlocksInfatuation: true,
			BlocksTaunt:       true,
		},

		// --- contact riders: 30% chance to inflict a status on contact ---
		"static": {
			Kind: "static",
			OnHit: func(s *BattleState, defSide int, m domain.Move, _ bool, rng *RNG, log *[]LogLine) {
				if !m.HasFlag("contact") || !rng.Chance(30) {
					return
				}
				atk := s.Active(1 - defSide)
				if inflictStatusFrom(atk, 1-defSide, defSide, StatusParalysis, s, rng, log) {
					def := s.Active(defSide)
					*log = append(*log, LogLine{
						Type: "ability", Side: defSide,
						Text: fmt.Sprintf("%s's Static paralyzed %s!", def.Name, atk.Name),
					})
				}
			},
		},
		"flame-body": {
			Kind: "flame-body",
			OnHit: func(s *BattleState, defSide int, m domain.Move, _ bool, rng *RNG, log *[]LogLine) {
				if !m.HasFlag("contact") || !rng.Chance(30) {
					return
				}
				atk := s.Active(1 - defSide)
				if inflictStatusFrom(atk, 1-defSide, defSide, StatusBurn, s, rng, log) {
					def := s.Active(defSide)
					*log = append(*log, LogLine{
						Type: "ability", Side: defSide,
						Text: fmt.Sprintf("%s's Flame Body burned %s!", def.Name, atk.Name),
					})
				}
			},
		},
		"poison-point": {
			Kind: "poison-point",
			OnHit: func(s *BattleState, defSide int, m domain.Move, _ bool, rng *RNG, log *[]LogLine) {
				if !m.HasFlag("contact") || !rng.Chance(30) {
					return
				}
				atk := s.Active(1 - defSide)
				if inflictStatusFrom(atk, 1-defSide, defSide, StatusPoison, s, rng, log) {
					def := s.Active(defSide)
					*log = append(*log, LogLine{
						Type: "ability", Side: defSide,
						Text: fmt.Sprintf("%s's Poison Point poisoned %s!", def.Name, atk.Name),
					})
				}
			},
		},
		"effect-spore": {
			Kind: "effect-spore",
			OnHit: func(s *BattleState, defSide int, m domain.Move, _ bool, rng *RNG, log *[]LogLine) {
				if !m.HasFlag("contact") || !rng.Chance(30) {
					return
				}
				// Pick one of three outcomes uniformly (canon is 9/9/11/71
				// for sleep/para/poison/nothing, but inside the 30% trigger
				// it's roughly 1/3 each; we do exactly 1/3 for clarity).
				atk := s.Active(1 - defSide)
				switch rng.IntN(3) {
				case 0:
					inflictStatusFrom(atk, 1-defSide, defSide, StatusSleep, s, rng, log)
				case 1:
					inflictStatusFrom(atk, 1-defSide, defSide, StatusParalysis, s, rng, log)
				default:
					inflictStatusFrom(atk, 1-defSide, defSide, StatusPoison, s, rng, log)
				}
			},
		},

		"cute-charm": {
			// Contact rider: 30% chance to infatuate the attacker. Fires
			// through a substitute like the other contact riders (the
			// attacker still made contact with the doll's holder).
			Kind: "cute-charm",
			OnHit: func(s *BattleState, defSide int, m domain.Move, _ bool, rng *RNG, log *[]LogLine) {
				if !m.HasFlag("contact") || !rng.Chance(30) {
					return
				}
				atk := s.Active(1 - defSide)
				if atk.Volatiles.Attract {
					return
				}
				atk.Volatiles.Attract = true
				def := s.Active(defSide)
				*log = append(*log, LogLine{
					Type: "ability", Side: defSide,
					Text: fmt.Sprintf("%s's Cute Charm infatuated %s!", def.Name, atk.Name),
				})
			},
		},

		"poison-touch": {
			// Attacker-side contact rider: the holder's contact moves have a 30%
			// chance to poison the target. It's the ability's own effect, not a
			// move secondary, so Shield Dust on the target doesn't suppress it;
			// but like every contact rider it can't reach a target behind a
			// substitute (the doll took the touch).
			Kind: "poison-touch",
			OnDealDamage: func(s *BattleState, atkSide int, m domain.Move, rng *RNG, log *[]LogLine) {
				if !m.HasFlag("contact") || !rng.Chance(30) {
					return
				}
				def := s.Active(1 - atkSide)
				if def.Fainted || def.HP <= 0 || hasSubstitute(def) {
					return
				}
				inflictStatus(def, 1-atkSide, StatusPoison, s, rng, log)
			},
		},

		"stench": {
			// Attacker-side rider: every damaging move the holder lands has a
			// 10% chance to make the target flinch. Like Poison Touch it's the
			// ability's own effect (Shield Dust doesn't suppress it) and reaches
			// only a directly-struck target, never one behind a substitute.
			// Inner Focus still blocks the flinch via applyFlinchVolatile.
			Kind: "stench",
			OnDealDamage: func(s *BattleState, atkSide int, m domain.Move, rng *RNG, log *[]LogLine) {
				if !rng.Chance(10) {
					return
				}
				def := s.Active(1 - atkSide)
				if def.Fainted || def.HP <= 0 || hasSubstitute(def) {
					return
				}
				applyFlinchVolatile(def, 1-atkSide, m, s, rng, log)
			},
		},

		// --- reactive defense: react to being hit by a damaging move ---
		"justified": {
			// Raises Attack by 1 stage when struck by a Dark-type move. The
			// boost is self-induced, so it dodges the foe-lower guards
			// (Clear Body etc.) — those only gate foe-caused drops.
			Kind: "justified",
			OnHit: func(s *BattleState, defSide int, m domain.Move, hitSub bool, _ *RNG, log *[]LogLine) {
				if hitSub || m.Type != "dark" {
					return
				}
				p := s.Active(defSide)
				*log = append(*log, LogLine{
					Type: "ability", Side: defSide,
					Text: fmt.Sprintf("%s's Justified raised its Attack!", p.Name),
				})
				applyStages(p, defSide, "attack", 1, log)
			},
		},
		"weak-armor": {
			// When hit by a physical move: Defense −1, Speed +2 (Gen 7+).
			// Both changes are self-induced; the Def drop is the holder's own
			// reaction, not a foe-lower, so it isn't blocked by Clear Body and
			// doesn't bait Defiant.
			Kind: "weak-armor",
			OnHit: func(s *BattleState, defSide int, m domain.Move, hitSub bool, _ *RNG, log *[]LogLine) {
				if hitSub || m.Category != domain.CatPhysical {
					return
				}
				p := s.Active(defSide)
				*log = append(*log, LogLine{
					Type: "ability", Side: defSide,
					Text: fmt.Sprintf("%s's Weak Armor shifted its build!", p.Name),
				})
				applyStages(p, defSide, "defense", -1, log)
				applyStages(p, defSide, "speed", 2, log)
			},
		},

		// --- stat protectors and reactors ---
		"clear-body":   {Kind: "clear-body", BlocksStatLowerByFoe: func(stat string) bool { return true }},
		"hyper-cutter": {Kind: "hyper-cutter", BlocksStatLowerByFoe: func(stat string) bool { return stat == "attack" }},
		"big-pecks":    {Kind: "big-pecks", BlocksStatLowerByFoe: func(stat string) bool { return stat == "defense" }},
		"keen-eye":     {Kind: "keen-eye", BlocksStatLowerByFoe: func(stat string) bool { return stat == "accuracy" }},
		"defiant": {
			Kind: "defiant",
			OnStatLoweredByFoe: func(p *Pokemon, side int, stat string, log *[]LogLine) {
				*log = append(*log, LogLine{
					Type: "ability", Side: side,
					Text: fmt.Sprintf("%s's Defiant raised its Attack sharply!", p.Name),
				})
				applyStages(p, side, "attack", 2, log)
			},
		},
		"competitive": {
			Kind: "competitive",
			OnStatLoweredByFoe: func(p *Pokemon, side int, stat string, log *[]LogLine) {
				*log = append(*log, LogLine{
					Type: "ability", Side: side,
					Text: fmt.Sprintf("%s's Competitive raised its Sp. Atk sharply!", p.Name),
				})
				applyStages(p, side, "spatk", 2, log)
			},
		},

		"anger-point": {
			// Taking a critical hit maxes Attack outright (+6), no matter the
			// current stage — even from −6. Fires only on a direct crit; a
			// crit soaked by a substitute leaves the holder untouched.
			Kind: "anger-point",
			OnCrit: func(s *BattleState, defSide int, log *[]LogLine) {
				p := s.Active(defSide)
				if p.Stages.Atk >= 6 {
					return
				}
				*log = append(*log, LogLine{
					Type: "ability", Side: defSide,
					Text: fmt.Sprintf("%s's Anger Point maxed its Attack!", p.Name),
				})
				applyStages(p, defSide, "attack", 6-p.Stages.Atk, log)
			},
		},

		// --- on-KO / on-faint reactions ---
		"aftermath": {
			// When fainted by a contact move, the attacker loses 1/4 of its
			// max HP. Indirect damage, so the attacker's Magic Guard blocks it.
			Kind: "aftermath",
			OnFaint: func(s *BattleState, faintedSide, atkSide int, m domain.Move, log *[]LogLine) {
				atk := s.Active(atkSide)
				if !m.HasFlag("contact") || atk.Fainted || abilityBlocksIndirectDamage(atk) {
					return
				}
				chipFraction(atk, atkSide, 0.25, "Aftermath", log)
			},
		},
		"moxie": {
			Kind: "moxie",
			OnKO: func(s *BattleState, side int, log *[]LogLine) {
				p := s.Active(side)
				*log = append(*log, LogLine{
					Type: "ability", Side: side,
					Text: fmt.Sprintf("%s's Moxie raised its Attack!", p.Name),
				})
				applyStages(p, side, "attack", 1, log)
			},
		},

		// --- end-of-turn ticks ---
		"speed-boost": {
			Kind: "speed-boost",
			EndOfTurn: func(s *BattleState, side int, _ *RNG, log *[]LogLine) {
				p := s.Active(side)
				*log = append(*log, LogLine{
					Type: "ability", Side: side,
					Text: fmt.Sprintf("%s's Speed Boost activated!", p.Name),
				})
				applyStages(p, side, "speed", 1, log)
			},
		},
		"rain-dish": {
			Kind: "rain-dish",
			EndOfTurn: func(s *BattleState, side int, _ *RNG, log *[]LogLine) {
				if w := effectiveWeather(s); w != nil && w.Kind == WeatherRain {
					healFraction(s.Active(side), side, 1.0/16, "Rain Dish", log)
				}
			},
		},
		"ice-body": {
			Kind: "ice-body",
			EndOfTurn: func(s *BattleState, side int, _ *RNG, log *[]LogLine) {
				if w := effectiveWeather(s); w != nil && w.Kind == WeatherSnow {
					healFraction(s.Active(side), side, 1.0/16, "Ice Body", log)
				}
			},
		},
		"dry-skin": {
			Kind: "dry-skin",
			// Dry Skin: Water absorbed (heal 1/4), Fire takes 1.25x.
			TypeMultOverride: func(t domain.Type) (float64, bool) {
				if t == "water" {
					return 0, true
				}
				return 1, false
			},
			OnImmunityBonus: func(s *BattleState, side int, t domain.Type, log *[]LogLine) {
				absorbAndHeal(s, side, t, "water", 0.25, "Dry Skin", log)
			},
			IncomingDamageMult: func(m domain.Move, def *Pokemon, typeEff float64) float64 {
				if m.Type == "fire" {
					return 1.25
				}
				return 1
			},
			EndOfTurn: func(s *BattleState, side int, _ *RNG, log *[]LogLine) {
				w := effectiveWeather(s)
				if w == nil {
					return
				}
				switch w.Kind {
				case WeatherRain:
					healFraction(s.Active(side), side, 1.0/8, "Dry Skin", log)
				case WeatherSun:
					chipFraction(s.Active(side), side, 1.0/8, "Dry Skin", log)
				}
			},
		},

		// --- end-of-turn status self-cure ---
		"shed-skin": {
			// 30% chance each turn-end to shed any major status.
			Kind: "shed-skin",
			EndOfTurn: func(s *BattleState, side int, rng *RNG, log *[]LogLine) {
				p := s.Active(side)
				if p.Status == StatusNone || !rng.Chance(30) {
					return
				}
				clearStatus(p)
				*log = append(*log, LogLine{
					Type: "ability", Side: side,
					Text: fmt.Sprintf("%s shed its status with Shed Skin!", p.Name),
				})
			},
		},
		"hydration": {
			// Cures any major status at turn-end while it's raining.
			Kind: "hydration",
			EndOfTurn: func(s *BattleState, side int, _ *RNG, log *[]LogLine) {
				p := s.Active(side)
				if p.Status == StatusNone {
					return
				}
				if w := effectiveWeather(s); w == nil || w.Kind != WeatherRain {
					return
				}
				clearStatus(p)
				*log = append(*log, LogLine{
					Type: "ability", Side: side,
					Text: fmt.Sprintf("%s's Hydration cured its status!", p.Name),
				})
			},
		},

		// --- switch-out: cure / regen ---
		"natural-cure": {
			Kind: "natural-cure",
			OnSwitchOut: func(p *Pokemon, side int, log *[]LogLine) {
				if p.Status == StatusNone {
					return
				}
				clearStatus(p)
				*log = append(*log, LogLine{
					Type: "ability", Side: side,
					Text: fmt.Sprintf("%s's Natural Cure healed its status!", p.Name),
				})
			},
		},
		"regenerator": {
			Kind: "regenerator",
			OnSwitchOut: func(p *Pokemon, side int, log *[]LogLine) {
				if p.HP >= p.MaxHP {
					return
				}
				amt := p.MaxHP / 3
				if amt < 1 {
					amt = 1
				}
				if p.HP+amt > p.MaxHP {
					amt = p.MaxHP - p.HP
				}
				p.HP += amt
				*log = append(*log, LogLine{
					Type: "ability", Side: side,
					Text: fmt.Sprintf("%s restored HP with Regenerator (+%d).", p.Name, amt),
				})
			},
		},

		// --- misc ---
		"skill-link":   {Kind: "skill-link", MaxesMultihit: true},
		"liquid-ooze":  {Kind: "liquid-ooze", DrainBackfires: true},
		"unaware":      {Kind: "unaware", IgnoresOpponentStages: true},
		"rock-head":    {Kind: "rock-head", BlocksRecoil: true},
		"serene-grace": {Kind: "serene-grace", SecondaryChanceMult: 2},
		"magic-guard":  {Kind: "magic-guard", BlocksIndirectDamage: true},
		"soundproof":   {Kind: "soundproof" /* handled in resolveAccuracy via direct Kind check */},
		"cloud-nine":   {Kind: "cloud-nine", SuppressWeather: true},
		"battle-armor": {Kind: "battle-armor", BlockCrit: true},
		"shell-armor":  {Kind: "shell-armor", BlockCrit: true},

		// --- weather speed boosters (× 2 in matching weather) ---
		"swift-swim": {Kind: "swift-swim", SpeedMult: func(p *Pokemon, w *WeatherState) float64 {
			if w != nil && w.Kind == WeatherRain {
				return 2
			}
			return 1
		}},
		"chlorophyll": {Kind: "chlorophyll", SpeedMult: func(p *Pokemon, w *WeatherState) float64 {
			if w != nil && w.Kind == WeatherSun {
				return 2
			}
			return 1
		}},
		"sand-rush": {Kind: "sand-rush", SpeedMult: func(p *Pokemon, w *WeatherState) float64 {
			if w != nil && w.Kind == WeatherSandstorm {
				return 2
			}
			return 1
		}},
		"slush-rush": {Kind: "slush-rush", SpeedMult: func(p *Pokemon, w *WeatherState) float64 {
			if w != nil && w.Kind == WeatherSnow {
				return 2
			}
			return 1
		}},

		// --- status-power abilities ---
		"quick-feet": {
			// Spe × 1.5 if statused. effectiveSpeed has a Kind == "quick-feet"
			// special case that suppresses the paralysis halve, so the holder
			// also keeps full speed under paralysis.
			Kind: "quick-feet",
			SpeedMult: func(p *Pokemon, w *WeatherState) float64 {
				if p.Status != StatusNone {
					return 1.5
				}
				return 1
			},
		},
		"guts": {
			// Atk × 1.5 if statused; ignores burn's Atk halve. Outgoing mod
			// handles both: when burned, normal physical is halved in damage.go
			// — Guts cancels that by multiplying by 2.0 (back to 1.0) AND
			// adds the 1.5 boost = 3.0 total when burned, 1.5 when otherwise
			// statused.
			Kind: "guts",
			OutgoingDamageMult: func(atk *Pokemon, m domain.Move, def *Pokemon, w *WeatherState, typeEff float64) float64 {
				if atk.Status == StatusNone || m.Category != domain.CatPhysical {
					return 1
				}
				mult := 1.5
				if atk.Status == StatusBurn {
					mult *= 2 // cancel the burn halve baked into computeDamage
				}
				return mult
			},
		},
		"steadfast": {
			Kind: "steadfast",
			OnFlinched: func(p *Pokemon, side int, log *[]LogLine) {
				*log = append(*log, LogLine{
					Type: "ability", Side: side,
					Text: fmt.Sprintf("%s's Steadfast raised its Speed!", p.Name),
				})
				applyStages(p, side, "speed", 1, log)
			},
		},
	}
}

// abilityOf returns the registry record for p's ability, or nil if no
// implementation is registered. nil is safe to ignore — every dispatcher
// nil-checks before invoking.
func abilityOf(p *Pokemon) *Ability {
	if p == nil {
		return nil
	}
	return abilityRegistry[p.Ability]
}

// --- shared helpers used by multiple registry entries ---

// setWeatherFromAbility installs the weather unconditionally — unlike a
// status-move setter (applyWeatherSetter), an ability auto-setter never
// "fails" when the same weather is already up; it just refreshes to the
// default duration silently when it would be a no-op. Used by Drought,
// Drizzle, Sand Stream, Snow Warning.
func setWeatherFromAbility(s *BattleState, side int, kind WeatherKind, log *[]LogLine) {
	if s.Weather != nil && s.Weather.Kind == kind {
		return
	}
	s.Weather = &WeatherState{Kind: kind, TurnsLeft: defaultWeatherTurns}
	user := s.Active(side)
	*log = append(*log, LogLine{
		Type: "ability", Side: side,
		Text: fmt.Sprintf("%s's ability set the weather!", user.Name),
	})
	*log = append(*log, LogLine{Type: "weather", Side: -1, Text: weatherStartedText(kind)})
}

// absorbAndHeal is the Volt Absorb / Water Absorb shape: block the given
// atkType (returns mult 0 / override true) and heal the holder for the
// fraction of MaxHP. The OnImmunityBonus hook calls this.
func absorbAndHeal(s *BattleState, side int, atkType domain.Type, blocked domain.Type, frac float64, abilityName string, log *[]LogLine) {
	if atkType != blocked {
		return
	}
	p := s.Active(side)
	*log = append(*log, LogLine{
		Type: "ability", Side: side,
		Text: fmt.Sprintf("%s absorbed the %s with %s!", p.Name, atkType, abilityName),
	})
	if p.HP >= p.MaxHP {
		return
	}
	amt := int(float64(p.MaxHP) * frac)
	if amt < 1 {
		amt = 1
	}
	healPokemon(p, side, amt, log)
}

// absorbAndBoost is the Lightning Rod / Storm Drain / Sap Sipper / Motor
// Drive shape: block the given atkType and raise stat by +1 stage.
func absorbAndBoost(s *BattleState, side int, atkType domain.Type, blocked domain.Type, stat string, abilityName string, log *[]LogLine) {
	if atkType != blocked {
		return
	}
	p := s.Active(side)
	*log = append(*log, LogLine{
		Type: "ability", Side: side,
		Text: fmt.Sprintf("%s's %s drew in the attack!", p.Name, abilityName),
	})
	applyStages(p, side, stat, 1, log)
}

// clearStatus removes a Pokémon's major status and the counters that ride
// with it (sleep clock, toxic stage). Shared by status-cure abilities
// (Natural Cure on switch-out, Shed Skin / Hydration at turn-end).
func clearStatus(p *Pokemon) {
	p.Status = StatusNone
	p.SleepTurns = 0
	p.ToxicCounter = 0
}

// pinchBoost is the Blaze / Torrent / Overgrow / Swarm shape: when the
// holder sits at or below 1/3 of max HP, its moves of the matching type
// deal 1.5× damage. Returned as an OutgoingDamageMult closure so each
// ability is a one-line registry entry.
func pinchBoost(t domain.Type) func(atk *Pokemon, m domain.Move, def *Pokemon, w *WeatherState, typeEff float64) float64 {
	return func(atk *Pokemon, m domain.Move, def *Pokemon, w *WeatherState, typeEff float64) float64 {
		if m.Type == t && atk.HP*3 <= atk.MaxHP {
			return 1.5
		}
		return 1
	}
}

// healFraction heals p for frac of MaxHP, clamped to MaxHP. Used by
// end-of-turn healers (Rain Dish, Ice Body, Dry Skin in rain).
func healFraction(p *Pokemon, side int, frac float64, why string, log *[]LogLine) {
	if p.HP >= p.MaxHP {
		return
	}
	amt := int(float64(p.MaxHP) * frac)
	if amt < 1 {
		amt = 1
	}
	if p.HP+amt > p.MaxHP {
		amt = p.MaxHP - p.HP
	}
	p.HP += amt
	*log = append(*log, LogLine{
		Type: "ability", Side: side,
		Text: fmt.Sprintf("%s restored a little HP (%s, +%d).", p.Name, why, amt),
	})
}

// chipFraction inflicts frac of MaxHP as ability-residual damage. Magic
// Guard does NOT block this — Solar Power / Dry Skin's sun penalty is
// considered the ability's own cost, not "indirect damage" the way burn
// and sand are. Modeled to match Showdown.
func chipFraction(p *Pokemon, side int, frac float64, why string, log *[]LogLine) {
	amt := int(float64(p.MaxHP) * frac)
	if amt < 1 {
		amt = 1
	}
	if amt > p.HP {
		amt = p.HP
	}
	p.HP -= amt
	*log = append(*log, LogLine{
		Type: "ability", Side: side,
		Text: fmt.Sprintf("%s was hurt by %s! (-%d)", p.Name, why, amt),
	})
	if p.HP <= 0 {
		faint(p, side, log)
	}
}

// defaultAbility picks slot 0 from a species' ability list — the convention
// for batches before the picker UI grows an ability dropdown (#30).
func defaultAbility(sp domain.Species) AbilityKind {
	if len(sp.Abilities) == 0 {
		return AbilityNone
	}
	return AbilityKind(sp.Abilities[0])
}

// --- dispatchers (call from integration sites) ---

// applyOnSwitchIn runs the active Pokémon's switch-in hook, if any.
func applyOnSwitchIn(s *BattleState, side int, log *[]LogLine) {
	user := s.Active(side)
	if user.Fainted {
		return
	}
	if a := abilityOf(user); a != nil && a.OnSwitchIn != nil {
		a.OnSwitchIn(s, side, log)
	}
}

// applyOnSwitchOut runs the outgoing Pokémon's switch-out hook, if any.
// Called by doSwitch before stages/volatiles are reset so the hook can
// observe the outgoing state.
func applyOnSwitchOut(p *Pokemon, side int, log *[]LogLine) {
	if p == nil || p.Fainted {
		return
	}
	if a := abilityOf(p); a != nil && a.OnSwitchOut != nil {
		a.OnSwitchOut(p, side, log)
	}
}

// abilityTypeMultOverride lets an ability replace the type-effectiveness
// lookup. Returns (multiplier, true) when the ability overrides.
func abilityTypeMultOverride(def *Pokemon, atkType domain.Type) (float64, bool) {
	if a := abilityOf(def); a != nil && a.TypeMultOverride != nil {
		return a.TypeMultOverride(atkType)
	}
	return 1, false
}

// abilityImmunityBonus runs the on-immunity side effect for absorb-style
// abilities (Volt Absorb heals, Lightning Rod boosts SpA, Flash Fire arms
// a damage boost, etc.). Called when an attack was blocked by a
// TypeMultOverride returning (0, true).
func abilityImmunityBonus(s *BattleState, side int, atkType domain.Type, log *[]LogLine) {
	def := s.Active(side)
	if a := abilityOf(def); a != nil && a.OnImmunityBonus != nil {
		a.OnImmunityBonus(s, side, atkType, log)
	}
}

// abilityIncomingDamageMult returns the defender-side multiplier. typeEff
// is the already-computed type effectiveness of m against def, passed in
// so hooks like Filter (super-effective hits ×0.75) and Multiscale don't
// need access to the dex.
func abilityIncomingDamageMult(def *Pokemon, m domain.Move, typeEff float64) float64 {
	if a := abilityOf(def); a != nil && a.IncomingDamageMult != nil {
		return a.IncomingDamageMult(m, def, typeEff)
	}
	return 1
}

// abilityOutgoingDamageMult returns the attacker-side multiplier. typeEff
// is the move's effectiveness against def (Tinted Lens doubles
// not-very-effective hits).
func abilityOutgoingDamageMult(atk *Pokemon, m domain.Move, def *Pokemon, weather *WeatherState, typeEff float64) float64 {
	if a := abilityOf(atk); a != nil && a.OutgoingDamageMult != nil {
		return a.OutgoingDamageMult(atk, m, def, weather, typeEff)
	}
	return 1
}

// abilitySurviveOHKO clamps overkill damage when the defender's ability is
// an OHKO-survive ability (Sturdy today, Multiscale variant possible later).
func abilitySurviveOHKO(def *Pokemon, damage int) (int, bool) {
	if def == nil || damage <= 0 {
		return damage, false
	}
	if a := abilityOf(def); a != nil && a.SurviveOHKO != nil {
		return a.SurviveOHKO(def, damage)
	}
	return damage, false
}

// abilityAccuracyMult is the multiplier applied to the attacker's
// accuracy roll. 1.0 when unset.
func abilityAccuracyMult(atk *Pokemon) float64 {
	if a := abilityOf(atk); a != nil && a.AccuracyMult != 0 {
		return a.AccuracyMult
	}
	return 1
}

// abilityBlocksCrit reports whether def's ability blocks crit hits.
func abilityBlocksCrit(def *Pokemon) bool {
	if a := abilityOf(def); a != nil {
		return a.BlockCrit
	}
	return false
}

// abilityBlocksSecondaries reports whether def's ability blocks the
// attacker's secondary effects (Shield Dust).
func abilityBlocksSecondaries(def *Pokemon) bool {
	if a := abilityOf(def); a != nil {
		return a.BlockSecondaries
	}
	return false
}

// abilityBlocksOwnSecondaries reports whether atk's ability suppresses its
// own secondary effects on damaging moves (Sheer Force).
func abilityBlocksOwnSecondaries(atk *Pokemon) bool {
	if a := abilityOf(atk); a != nil {
		return a.BlockOwnSecondaries
	}
	return false
}

// abilityBlocksStatus reports whether def's ability refuses the status.
func abilityBlocksStatus(def *Pokemon, st StatusCond) bool {
	if a := abilityOf(def); a != nil && a.BlocksStatus != nil {
		return a.BlocksStatus(st)
	}
	return false
}

// abilityBlocksInfatuation reports whether p's ability makes it immune to
// infatuation (Oblivious). Consulted by applyAttractVolatile.
func abilityBlocksInfatuation(p *Pokemon) bool {
	if a := abilityOf(p); a != nil {
		return a.BlocksInfatuation
	}
	return false
}

// abilityBlocksTaunt reports whether p's ability makes it immune to Taunt
// (Oblivious). Consulted by applyTauntVolatile.
func abilityBlocksTaunt(p *Pokemon) bool {
	if a := abilityOf(p); a != nil {
		return a.BlocksTaunt
	}
	return false
}

// abilityBlocksStatusState reports whether def's ability refuses the status
// given the current battle state (Leaf Guard under sun). Consulted by
// inflictStatus after the stateless BlocksStatus guard.
func abilityBlocksStatusState(s *BattleState, def *Pokemon, st StatusCond) bool {
	if a := abilityOf(def); a != nil && a.BlocksStatusState != nil {
		return a.BlocksStatusState(s, def, st)
	}
	return false
}

// abilityBlocksRecoil reports whether p's ability cancels its own move recoil
// (Rock Head). Narrower than abilityBlocksIndirectDamage.
func abilityBlocksRecoil(p *Pokemon) bool {
	if a := abilityOf(p); a != nil {
		return a.BlocksRecoil
	}
	return false
}

// abilitySecondaryChanceMult returns the multiplier applied to the holder's
// secondary-effect chances (Serene Grace = 2). 1 when unset.
func abilitySecondaryChanceMult(p *Pokemon) float64 {
	if a := abilityOf(p); a != nil && a.SecondaryChanceMult != 0 {
		return a.SecondaryChanceMult
	}
	return 1
}

// abilityIgnoresStages reports whether p's ability ignores the opponent's
// stat stages in the damage formula (Unaware).
func abilityIgnoresStages(p *Pokemon) bool {
	if a := abilityOf(p); a != nil {
		return a.IgnoresOpponentStages
	}
	return false
}

// abilityMaxesMultihit reports whether p's ability forces multi-strike moves
// to their maximum hit count (Skill Link).
func abilityMaxesMultihit(p *Pokemon) bool {
	if a := abilityOf(p); a != nil {
		return a.MaxesMultihit
	}
	return false
}

// abilityDrainBackfires reports whether the drained Pokémon's ability turns a
// drain into damage on the drainer (Liquid Ooze).
func abilityDrainBackfires(drained *Pokemon) bool {
	if a := abilityOf(drained); a != nil {
		return a.DrainBackfires
	}
	return false
}

// abilityBlocksFlinch reports whether def's ability blocks flinching.
func abilityBlocksFlinch(def *Pokemon) bool {
	if a := abilityOf(def); a != nil {
		return a.BlocksFlinch
	}
	return false
}

// applyOnFlinched fires the post-flinch reaction (Steadfast +1 Spe).
// Called from applyVolatile right after a flinch volatile lands.
func applyOnFlinched(p *Pokemon, side int, log *[]LogLine) {
	if a := abilityOf(p); a != nil && a.OnFlinched != nil {
		a.OnFlinched(p, side, log)
	}
}

// applyOnHit fires the defender's on-hit hook (contact riders, reactive
// defense). Called after damage applies, only if dealDamage reported a
// successful hit. hitSub is true when a substitute absorbed the blow.
func applyOnHit(s *BattleState, defSide int, m domain.Move, hitSub bool, rng *RNG, log *[]LogLine) {
	def := s.Active(defSide)
	if def.Fainted {
		return
	}
	if a := abilityOf(def); a != nil && a.OnHit != nil {
		a.OnHit(s, defSide, m, hitSub, rng, log)
	}
}

// applyPressurePP charges the extra PP a foe's Pressure ability imposes. A move
// that targets the foe (anything not self-targeted) costs one additional PP from
// the mover's slot when that foe is exerting Pressure. Clamped at zero so it can
// never underflow, and skipped for Struggle (no real slot) and forced-move turns
// (charge/rampage) whose PP was already paid on the initiating turn.
func applyPressurePP(s *BattleState, side int, atk *Pokemon, moveIdx int, m domain.Move) {
	if m.ID == "" || m.Target == domain.TargetSelf {
		return
	}
	if moveIdx < 0 || moveIdx >= len(atk.Moves) {
		return
	}
	foe := s.Active(1 - side)
	if foe.Fainted {
		return
	}
	if a := abilityOf(foe); a == nil || !a.ExertsPressure {
		return
	}
	if atk.Moves[moveIdx].PP > 0 {
		atk.Moves[moveIdx].PP--
	}
}

// applyOnDealDamage fires the attacker's own on-hit rider (Poison Touch) after
// its damaging move connects. Called once per connecting strike, on the direct
// hit only — the substitute path never reaches here.
func applyOnDealDamage(s *BattleState, atkSide int, m domain.Move, rng *RNG, log *[]LogLine) {
	atk := s.Active(atkSide)
	if atk.Fainted {
		return
	}
	if a := abilityOf(atk); a != nil && a.OnDealDamage != nil {
		a.OnDealDamage(s, atkSide, m, rng, log)
	}
}

// applyOnKO fires the attacker's on-KO reaction (Moxie +1 Atk) after its move
// faints the foe. No-op if the attacker fainted in the same exchange (e.g. to
// recoil or Destiny Bond) — a fainted Pokémon doesn't collect the boost.
func applyOnKO(s *BattleState, side int, log *[]LogLine) {
	atk := s.Active(side)
	if atk.Fainted || atk.HP <= 0 {
		return
	}
	if a := abilityOf(atk); a != nil && a.OnKO != nil {
		a.OnKO(s, side, log)
	}
}

// applyOnCrit fires the defender's reaction to taking a critical hit
// (Anger Point). Called only from the direct-damage path, never when a
// substitute absorbed the crit.
func applyOnCrit(s *BattleState, defSide int, log *[]LogLine) {
	def := s.Active(defSide)
	if def.Fainted || def.HP <= 0 {
		return
	}
	if a := abilityOf(def); a != nil && a.OnCrit != nil {
		a.OnCrit(s, defSide, log)
	}
}

// applyOnFaint fires the fainted Pokémon's reaction to its killer (Aftermath).
// atkSide is the attacker that scored the KO; m is the finishing move.
func applyOnFaint(s *BattleState, faintedSide, atkSide int, m domain.Move, log *[]LogLine) {
	p := s.Active(faintedSide)
	if a := abilityOf(p); a != nil && a.OnFaint != nil {
		a.OnFaint(s, faintedSide, atkSide, m, log)
	}
}

// abilityBlocksStatLowerByFoe reports whether def's ability blocks a stat
// drop induced by the foe. Used by applyStages.
func abilityBlocksStatLowerByFoe(def *Pokemon, stat string) bool {
	if a := abilityOf(def); a != nil && a.BlocksStatLowerByFoe != nil {
		return a.BlocksStatLowerByFoe(stat)
	}
	return false
}

// applyOnStatLoweredByFoe fires the reaction hook (Defiant / Competitive).
func applyOnStatLoweredByFoe(p *Pokemon, side int, stat string, log *[]LogLine) {
	if a := abilityOf(p); a != nil && a.OnStatLoweredByFoe != nil {
		a.OnStatLoweredByFoe(p, side, stat, log)
	}
}

// abilitySpeedMult returns the speed multiplier the active's ability
// applies. 1.0 when unset; weather-aware (Swift Swim / Chlorophyll /
// Sand Rush / Slush Rush) consult effectiveWeather, which honors
// Cloud Nine.
func abilitySpeedMult(p *Pokemon, weather *WeatherState) float64 {
	if a := abilityOf(p); a != nil && a.SpeedMult != nil {
		return a.SpeedMult(p, weather)
	}
	return 1
}

// weatherSuppressed reports whether either active Pokémon's ability
// suppresses the global weather (Cloud Nine, future Air Lock). Damage,
// speed, and residual call sites use this to short-circuit weather
// effects without actually clearing the field state.
func weatherSuppressed(s *BattleState) bool {
	for i := 0; i < 2; i++ {
		p := s.Active(i)
		if p.Fainted {
			continue
		}
		if a := abilityOf(p); a != nil && a.SuppressWeather {
			return true
		}
	}
	return false
}

// effectiveWeather returns s.Weather, or nil if it's been suppressed by a
// Cloud Nine-style ability. The underlying field state (and turn counter)
// is unchanged — only consumers of weather effects see "no weather".
func effectiveWeather(s *BattleState) *WeatherState {
	if weatherSuppressed(s) {
		return nil
	}
	return s.Weather
}

// abilitySuppressesWeather reports whether p's ability suppresses weather
// effects (Cloud Nine, future Air Lock). Used by computeDamage /
// ExpectedDamage as a defensive idempotent — if either combatant has
// suppression, the weather param is treated as nil regardless of what the
// caller passed.
func abilitySuppressesWeather(p *Pokemon) bool {
	if p == nil {
		return false
	}
	a := abilityOf(p)
	return a != nil && a.SuppressWeather
}

// abilityBlocksIndirectDamage reports whether p's ability immunizes it
// against all residual / chip / recoil damage (Magic Guard).
func abilityBlocksIndirectDamage(p *Pokemon) bool {
	if a := abilityOf(p); a != nil {
		return a.BlocksIndirectDamage
	}
	return false
}

// applyAbilityEndOfTurn fires p's end-of-turn ability tick, if any. Called
// after weather residual + weather tick in ResolveTurn.
func applyAbilityEndOfTurn(s *BattleState, side int, rng *RNG, log *[]LogLine) {
	p := s.Active(side)
	if p.Fainted {
		return
	}
	if a := abilityOf(p); a != nil && a.EndOfTurn != nil {
		a.EndOfTurn(s, side, rng, log)
	}
}
