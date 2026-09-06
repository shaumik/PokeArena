package engine

import (
	"fmt"
	"strings"

	"github.com/shaumik/PokeArena/internal/domain"
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

	AbilityMoldBreaker AbilityKind = "mold-breaker"

	// AbilityGluttony makes the holder eat a quarter-HP pinch berry at half HP
	// instead. Flag-only: the whole effect lives in pinchThresholdFor, which the
	// item layer consults — plus the latch it reads through gluttonyArmed, which
	// is what stops the lift applying the instant Neutralizing Gas clears.
	AbilityGluttony AbilityKind = "gluttony"
)

// AbilitySimple doubles every stat-stage change its holder receives. Named
// because Simple Beam writes it by identity and nothing in the dex carries it,
// so a typo'd literal would produce an ability that silently does nothing —
// the exact failure mode this whole cluster is about.
const AbilitySimple AbilityKind = "simple"

// abilityStageDeltaMult returns the multiplier the holder's ability applies to
// an incoming stat-stage change. 1 for everything but Simple, and 1 for a
// suppressed ability — abilityOf already answers that.
func abilityStageDeltaMult(p *Pokemon) int {
	if a := abilityOf(p); a != nil && a.StageDeltaMult != 0 {
		return a.StageDeltaMult
	}
	return 1
}

// abilityIsGluttony reports whether p eats its pinch berries early. Split out
// so the item layer never has to know the slug.
func abilityIsGluttony(p *Pokemon) bool {
	a := abilityOf(p)
	return a != nil && a.Kind == AbilityGluttony
}

// Ability is the registry record for one ability. Every field is optional;
// only set the hooks the ability actually participates in. Dispatchers (see
// the funcs below the registry) handle nil-checks so call sites stay tight.
//
// Hook timing reference:
//
//	OnSwitchIn         — after the new active is installed (doSwitch) and on turn-1 leads
//	OnSwitchOut        — on the outgoing Pokémon, before stages/volatiles are reset
//	OnEnd              — when an ability-setting move overwrites this ability in place
//	TypeMultOverride   — first thing in computeDamage / ExpectedDamage; replaces the type chart
//	OnImmunityBonus    — fires when TypeMultOverride returned (0, true) for an incoming hit
//	BasePowerMult      — base-power group, attacker side (canon's onBasePower)
//	SourceBasePowerMult — base-power group, defender side (canon's onSourceBasePower)
//	AtkStatMult        — stat group, the attacker's own Atk/SpA (canon's onModifyAtk/onModifySpA)
//	SourceAtkStatMult  — stat group, the defender lowering the attacker's Atk/SpA (Thick Fat)
//	IncomingDamageMult — final-modifier group (defender); canon's onSourceModifyDamage
//	OutgoingDamageMult — final-modifier group (attacker); canon's onModifyDamage
//	SurviveOHKO        — post-formula damage cap (defender side); returns (cappedDamage, fired)
//	AccuracyMult       — applied to the attacker's accuracy roll
//	BlockCrit          — if true, defender takes no crits
//	BlockSecondaries   — if true, attacker's secondaries against this defender can't fire
//	BlockOwnSecondaries — if true, attacker's own secondaries are suppressed (Sheer Force)
//	BlocksStatus       — return true to refuse a status infliction (defender side)
//	BlocksFlinch       — if true, defender immune to flinch
//	BlocksConfusion    — if true, defender immune to confusion
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
	// OnEnd fires when the holder stops having this ability *while staying on
	// the field* — Skill Swap, Role Play, Worry Seed, Simple Beam. It is not
	// the switch-out hook: leaving the field resets the whole volatile set
	// anyway, so only the mid-battle rewrite needs a tear-down. Upstream's
	// singleEvent('End', oldAbility) in Pokemon#setAbility.
	OnEnd func(p *Pokemon, side int, log *[]LogLine)

	TypeMultOverride func(atkType domain.Type) (mult float64, override bool)
	OnImmunityBonus  func(s *BattleState, side int, atkType domain.Type, log *[]LogLine)

	// BasePowerMult is the base-power group: canon's `onBasePower`, chained
	// with every other base-power handler and applied to base power *before*
	// the damage formula runs. SourceBasePowerMult is the defender-side form
	// (`onSourceBasePower`) — Dry Skin's ×1.25 from Fire is one of these, not
	// an incoming-damage multiplier.
	//
	// Neither is handed the type effectiveness, and that is deliberate rather
	// than an oversight: canon runs this group before the type chart is
	// consulted, so a base-power handler cannot see it. An ability that really
	// does key on effectiveness (Tinted Lens, Filter) is a final-group handler
	// and belongs in the pair below.
	//
	// The *Priority fields are canon's `on*BasePowerPriority`. They are not
	// decoration: chainMod rounds at every pairing, so composing the same
	// modifiers in a different order can differ by a point, and the chain is
	// assembled highest-priority-first to match. See basePowerMod in damage.go.
	BasePowerMult           func(atk *Pokemon, m domain.Move, def *Pokemon, weather *WeatherState) float64
	BasePowerPriority       int
	SourceBasePowerMult     func(atk *Pokemon, m domain.Move, def *Pokemon, weather *WeatherState) float64
	SourceBasePowerPriority int

	// AtkStatMult is the stat group: canon's `onModifyAtk` / `onModifySpA` on
	// the attacker. Which of the two runs is decided by the move's *category*,
	// exactly as getDamage decides it, so a handler that only registers one of
	// them (Guts and Hustle are Atk-only, Solar Power SpA-only) has to test the
	// category itself. SourceAtkStatMult is the defender-side form
	// (`onSourceModifyAtk` / `onSourceModifySpA`) — Thick Fat lowers the
	// attacker's stat rather than reducing the damage, and the difference is
	// visible once truncation is involved.
	//
	// AtkStatDirect marks a handler that returns `this.modify(stat, x)` rather
	// than `this.chainModify(x)`. Hustle is the only one in this registry, and
	// upstream flags it in a comment: "This should be applied directly to the
	// stat as opposed to chaining with the others."
	AtkStatMult           func(atk *Pokemon, m domain.Move, def *Pokemon, weather *WeatherState) float64
	AtkStatPriority       int
	AtkStatDirect         bool
	SourceAtkStatMult     func(atk *Pokemon, m domain.Move, def *Pokemon, weather *WeatherState) float64
	SourceAtkStatPriority int

	IncomingDamageMult func(m domain.Move, def *Pokemon, typeEff float64) float64
	OutgoingDamageMult func(atk *Pokemon, m domain.Move, def *Pokemon, weather *WeatherState, typeEff float64) float64
	SurviveOHKO        func(def *Pokemon, damage int) (int, bool)

	AccuracyMult float64
	// AccuracyMultVs is the defender-side accuracy multiplier: it scales the
	// chance of a move landing on the holder (evasion abilities like Sand Veil
	// / Snow Cloak / Tangled Feet return < 1; Wonder Skin halves status-move
	// accuracy). State- and move-aware so it can read weather and category.
	AccuracyMultVs func(s *BattleState, def *Pokemon, m domain.Move) float64
	// NoGuard makes every move to or from the holder land regardless of
	// accuracy or evasion. Checked on both attacker and defender in
	// resolveAccuracy (No Guard).
	NoGuard             bool
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
	BlocksConfusion   bool

	// DrainBackfires turns the holder's drained HP into damage on the
	// drainer instead of healing (Liquid Ooze).
	DrainBackfires bool

	// BlocksRecoil makes the holder immune to its own move recoil without
	// touching other indirect damage (Rock Head — narrower than Magic Guard).
	BlocksRecoil bool

	// StageDeltaMult scales every stat-stage change the holder receives, from
	// any source (Simple = 2). Zero means "unset" and the dispatcher treats it
	// as 1.
	StageDeltaMult int

	// MaxesMultihit makes the holder's multi-strike moves always hit the
	// maximum number of times (Skill Link — Bullet Seed always hits 5).
	MaxesMultihit bool

	// ExertsPressure makes every foe move that targets this Pokémon cost one
	// extra PP (Pressure). Consulted at PP-payment time in executeMove.
	ExertsPressure bool

	// BreaksMold makes the holder's attacks ignore the target's
	// damage-affecting defensive abilities (type immunities, Sturdy,
	// damage-reduction, crit blocks, Soundproof). Consulted via
	// abilityBreaksMold in computeDamage and the OHKO / accuracy gates
	// (Mold Breaker; Teravolt / Turboblaze share the flag).
	BreaksMold bool

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
				revealAbility(user)
				*log = append(*log, LogLine{
					Type: "ability", Side: side,
					Text: fmt.Sprintf("%s's Intimidate cuts %s's Attack!", user.Name, foe.Name),
				})
				// Intimidate reaches the foe from applyOnSwitchIn, nowhere near
				// a move's boosts block, so every check that block performs has
				// to be re-made here. The herb one was; the substitute one was
				// not, and that is the whole of the defect: a doll stops
				// Intimidate outright.
				//
				// Canon puts the check inside Intimidate's own onStart rather
				// than in the shared boost path —
				// `if (target.volatiles['substitute']) { this.add('-immune',
				// target) } else { this.boost(...) }` — and announces the
				// ability first either way, which is why the log line above
				// stays above this.
				if hasSubstitute(foe) {
					*log = append(*log, LogLine{
						Type: "immune", Side: foeSide,
						Text: fmt.Sprintf("%s's substitute took the intimidation!", foe.Name),
					})
					return
				}
				applyStagesFromFoe(foe, foeSide, "attack", -1, s, log)
				applyItemStatCheck(foe, foeSide, log)
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
		"frisk": {
			// Reveals the foe's held item on entry (information only, no battle
			// effect). Silent when the foe is itemless. The item is shown from
			// its slug since the engine carries no item-name table.
			Kind: "frisk",
			OnSwitchIn: func(s *BattleState, side int, log *[]LogLine) {
				user := s.Active(side)
				foe := s.Active(1 - side)
				if foe.Fainted || foe.Item == ItemNone {
					return
				}
				revealAbility(user)
				revealItem(foe)
				*log = append(*log, LogLine{
					Type: "ability", Side: side,
					Text: fmt.Sprintf("%s frisked %s and found its %s!", user.Name, foe.Name, itemDisplayName(foe.Item)),
				})
			},
		},
		AbilityMoldBreaker: {
			// Attacks ignore the target's damage-affecting defensive abilities.
			// The piercing itself lives at the consult sites (computeDamage, the
			// OHKO gate, resolveAccuracy) via abilityBreaksMold; here we only
			// carry the flag and the canonical entry announcement.
			Kind:       AbilityMoldBreaker,
			BreaksMold: true,
			OnSwitchIn: func(s *BattleState, side int, log *[]LogLine) {
				p := s.Active(side)
				revealAbility(p)
				*log = append(*log, LogLine{
					Type: "ability", Side: side,
					Text: fmt.Sprintf("%s breaks the mold!", p.Name),
				})
			},
		},
		// Gluttony: flag-only, but genuinely functional — pinchThresholdFor
		// lifts a quarter-HP berry trigger to half HP for the holder. It has no
		// hook of its own because the effect belongs to the item layer, which
		// is the only thing that knows a berry's declared threshold.
		AbilityGluttony: {Kind: AbilityGluttony},

		// --- recognized but inert ---
		// These abilities appear on species in the dex but have no effect the
		// engine can express yet. They are registered (rather than left absent)
		// so the roster is explicitly complete: a nil lookup can't tell "not a
		// real ability" from "not modeled". Each notes what unblocks it. All
		// carry only Kind, so every dispatcher no-ops exactly as before.
		//
		// Blocked on unmodeled infrastructure:
		//   forewarn                     — needs the dex threaded into OnSwitchIn
		//                                  to rank the foe's moves by power.
		//
		// Gluttony and Harvest left this group when berries arrived; Unnerve
		// left when its latch did — see the OnSwitchIn below.
		// Inert by design in a trainer/PvP singles battle:
		//   illuminate — affects wild-encounter rates only.
		//   run-away   — guarantees fleeing wild battles only.
		//   healer     — heals an ally's status; there is no ally in singles.
		"forewarn":   {Kind: "forewarn"},
		"illuminate": {Kind: "illuminate"},
		"run-away":   {Kind: "run-away"},
		"healer":     {Kind: "healer"},

		// Neutralizing Gas suppresses every other ability on the field while
		// its holder is out, and restores them when it leaves. The hook here
		// only announces: the suppression lives in abilitysuppression.go,
		// because it is field state that has to be correct before any other
		// ability hook runs, including on turn-1 leads.
		AbilityNeutralizingGas: {
			Kind:       AbilityNeutralizingGas,
			OnSwitchIn: announceNeutralizingGas,
		},
		"rivalry": {
			// ×1.25 against a target of the same gender, ×0.75 against the
			// opposite one — "fights harder against a rival". No effect when
			// either side is genderless, which is canon and is why it needed
			// gender modeled before it could do anything.
			Kind:              "rivalry",
			BasePowerPriority: bpPrioRivalry,
			BasePowerMult: func(atk *Pokemon, _ domain.Move, def *Pokemon, _ *WeatherState) float64 {
				if atk == nil || def == nil {
					return 1
				}
				if atk.Gender == "" || def.Gender == "" ||
					atk.Gender == domain.GenderGenderless || def.Gender == domain.GenderGenderless {
					return 1
				}
				if atk.Gender == def.Gender {
					return 1.25
				}
				return 0.75
			},
		},

		// --- hook-free but fully functional ---
		// These carry only Kind because their effect belongs to a layer that
		// consults the ability slug directly, not because there is nothing to
		// model. Registered so abilityOf finds them — an unregistered slug is
		// invisible to every lookup, which is a silent way to lose an ability.
		//
		// Sticky Hold refuses every item removal: itemIsRemovable reads it, and
		// Knock Off / Thief / Covet / Trick / Switcheroo all gate on that.
		// Klutz makes the holder's own item do nothing: itemSuppressed reads it,
		// beside Embargo and Magic Room. No species in the current dex has
		// Klutz; the mechanic is here so the day one is synced in it works.
		//
		// Only slugs that some other layer really does consult belong here.
		// Five inert ones were filed under this heading and were listed as
		// inert twenty lines above at the same time — a referee read the
		// heading, believed Neutralizing Gas worked, and a tournament team
		// spent a Pokémon switching in to suppress an ability that was never
		// suppressed. TestInertAbilitiesAreFiledAsInert keeps the two lists
		// from disagreeing again.
		"sticky-hold": {Kind: "sticky-hold"},
		"klutz":       {Kind: "klutz"},
		"pressure": {
			// Every foe move aimed at the holder costs an extra PP. Announced on
			// entry the way canon does; the PP drain itself is applied at
			// PP-payment time in executeMove via ExertsPressure.
			Kind:           "pressure",
			ExertsPressure: true,
			OnSwitchIn: func(s *BattleState, side int, log *[]LogLine) {
				p := s.Active(side)
				revealAbility(p)
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
		// Thick Fat halves the *attacker's* Atk or SpA rather than the damage:
		// `onSourceModifyAtk` / `onSourceModifySpA` upstream. Not the same
		// number as halving the finished figure — the stat truncates before the
		// division by the defense, so the two disagree by a point often, and by
		// more once the base term is small.
		//
		// The Atk hook is priority 6 rather than 5, which is upstream putting it
		// deliberately above Hustle: Hustle modifies the stat directly, so it
		// matters whether Thick Fat has already chained when it does.
		AbilityThickFat: {
			Kind:                  AbilityThickFat,
			SourceAtkStatPriority: statPrioThickFat,
			SourceAtkStatMult: func(atk *Pokemon, m domain.Move, def *Pokemon, w *WeatherState) float64 {
				if m.Type == "fire" || m.Type == "ice" {
					return 0.5
				}
				return 1
			},
		},

		"trace": {
			// On entry, copy the foe's ability and immediately run its own
			// switch-in effect (tracing Intimidate cuts the foe's Attack, tracing
			// a weather setter sets the weather, and so on). A few abilities can't
			// be traced (see abilityTraceable); against those Trace stays inert.
			Kind: "trace",
			OnSwitchIn: func(s *BattleState, side int, log *[]LogLine) {
				foe := s.Active(1 - side)
				if foe.Fainted || !abilityTraceable(foe.Ability) {
					return
				}
				p := s.Active(side)
				// Trace announces itself, so the tracer's new ability is public
				// the moment it copies — and copying it is also how the foe's
				// ability became public in the first place.
				revealAbility(p)
				revealAbility(foe)
				// Remember what to put back. The copy is field-scoped in
				// canon, and doSwitchWithCarry restores from BaseAbility on
				// the way out; without this a tracer that ever pivots is
				// locked to its first copy for the rest of the battle.
				if p.BaseAbility == "" {
					p.BaseAbility = p.Ability
				}
				p.Ability = foe.Ability
				revealAbility(p)
				*log = append(*log, LogLine{
					Type: "ability", Side: side,
					Text: fmt.Sprintf("%s's Trace copied %s's %s!", p.Name, foe.Name, prettyAbilityName(foe.Ability)),
				})
				// Run the copied ability's own entry hook.
				applyOnSwitchIn(s, side, log)
			},
		},

		// Unnerve stops the foe eating its berries. Modeled as a *latch* armed
		// by the entry hook rather than as a live "is the foe holding Unnerve"
		// read, and that is the entire content of the ported case: canon's
		// unnerve sets effectState.unnerved in onStart and clears it in onEnd,
		// and a switch is two queue actions with an Update event between them.
		// So between the old holder leaving and the new one's onStart firing,
		// there is a window in which nothing is unnerved and a pinch berry gets
		// eaten. A live read would close that window and lose the case.
		"unnerve": {
			Kind: "unnerve",
			OnSwitchIn: func(s *BattleState, side int, log *[]LogLine) {
				p := s.Active(side)
				p.Volatiles.Unnerve = true
				revealAbility(p)
				*log = append(*log, LogLine{
					Type: "ability", Side: side,
					Text: fmt.Sprintf("%s's Unnerve made the foe too nervous to eat Berries!", p.Name),
				})
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
					revealAbility(p)
					*log = append(*log, LogLine{
						Type: "ability", Side: side,
						Text: fmt.Sprintf("%s's Flash Fire raised its Fire power!", p.Name),
					})
				}
			},
			// Stat group: the charge's condition registers onModifyAtk *and*
			// onModifySpA upstream, so the ×1.5 lands on whichever stat the
			// move's category names — never on the finished damage figure.
			AtkStatPriority: statPrioAbility,
			AtkStatMult: func(atk *Pokemon, m domain.Move, def *Pokemon, w *WeatherState) float64 {
				if atk.Volatiles.FlashFireCharged && m.Type == "fire" {
					return 1.5
				}
				return 1
			},
			// The charge is the ability's own state, so losing the ability
			// discards it (canon's flashfire.onEnd removes the volatile). The
			// boost above is already gated on abilityOf, so this changes nothing
			// while the ability is gone — it matters when it comes *back*: a
			// Skill Swap out and back in must not restore a charge the holder
			// spent that time not having.
			OnEnd: func(p *Pokemon, side int, log *[]LogLine) {
				p.Volatiles.FlashFireCharged = false
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
		// Technician is the highest-priority base-power handler in the whole
		// gen-9 dataset (30), which settles what its threshold reads. Upstream
		// computes `basePowerAfterMultiplier = this.modify(basePower,
		// this.event.modifier)` before testing `<= 60`, and that line looks like
		// "check the boosted power" — but with nothing above it in the chain the
		// accumulated modifier is still 1, and modify(bp, 1) is bp. The line
		// earns its keep under the gen-7 mod, which overrides the priority to 19
		// and so puts Battery (22) ahead of it; upstream's own two cases pin the
		// split, refusing the boost after a gen-7 Battery and granting it after a
		// gen-9 Steely Spirit. So the raw read below is right for this engine,
		// and `docs/royale-followups.md` claiming otherwise was wrong.
		//
		// Raw means the move's power as the formula receives it, which is after
		// basePowerCallback (executeMove rewrites m.Power for Rage Fist, Trump
		// Card and the rest) and before Charge's ×2 — Charge is a priority-9
		// handler, far below Technician.
		"technician": {
			Kind:              "technician",
			BasePowerPriority: bpPrioTechnician,
			BasePowerMult: func(atk *Pokemon, m domain.Move, def *Pokemon, w *WeatherState) float64 {
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
		// Reckless and Iron Fist share a priority (23) and a numerator: upstream
		// writes both as `chainModify([4915, 4096])`, the same 1.19995 the type
		// boosters use rather than a clean 1.2.
		//
		// Canon also boosts crash-damage moves (`move.recoil || move.hasCrashDamage`),
		// which High Jump Kick and Jump Kick carry and this engine does not model
		// as recoil. That is a gap in what counts as reckless, not in which group
		// the boost belongs to, so it is filed rather than fixed here.
		"reckless": {
			Kind:              "reckless",
			BasePowerPriority: bpPrioPunchBoost,
			BasePowerMult: func(atk *Pokemon, m domain.Move, def *Pokemon, w *WeatherState) float64 {
				if m.Self != nil && m.Self.Recoil > 0 {
					return mod4096(4915)
				}
				return 1
			},
		},
		"iron-fist": {
			Kind:              "iron-fist",
			BasePowerPriority: bpPrioPunchBoost,
			BasePowerMult: func(atk *Pokemon, m domain.Move, def *Pokemon, w *WeatherState) float64 {
				if m.HasFlag("punch") {
					return mod4096(4915)
				}
				return 1
			},
		},
		// Hustle is the one stat handler that does not chain. Upstream says so in
		// a comment and then writes `return this.modify(atk, 1.5)` rather than
		// chainModify, so it truncates the Attack stat where it stands and
		// whatever else applies chains onto the truncated figure.
		"hustle": {
			Kind:            "hustle",
			AccuracyMult:    0.8,
			AtkStatPriority: statPrioAbility,
			AtkStatDirect:   true,
			AtkStatMult: func(atk *Pokemon, m domain.Move, def *Pokemon, w *WeatherState) float64 {
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
			Kind:              "analytic",
			BasePowerPriority: bpPrioThirtyPercent,
			BasePowerMult: func(atk *Pokemon, m domain.Move, def *Pokemon, w *WeatherState) float64 {
				if atk.Volatiles.MovedLast {
					return mod4096(5325)
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
			BasePowerPriority:   bpPrioThirtyPercent,
			BasePowerMult: func(atk *Pokemon, m domain.Move, def *Pokemon, w *WeatherState) float64 {
				if len(m.Secondaries) > 0 {
					return mod4096(5325)
				}
				return 1
			},
		},
		"solar-power": {
			Kind:            "solar-power",
			AtkStatPriority: statPrioAbility,
			// onModifySpA only, so the category test is the ability's own and
			// not the dispatcher's — a Solar Power physical move gets nothing.
			AtkStatMult: func(atk *Pokemon, m domain.Move, def *Pokemon, w *WeatherState) float64 {
				if w != nil && w.Kind == WeatherSun && m.Category == domain.CatSpecial {
					return 1.5
				}
				return 1
			},
			EndOfTurn: func(s *BattleState, side int, _ *RNG, log *[]LogLine) {
				if w := weatherFor(s.Active(side), effectiveWeather(s)); w != nil && w.Kind == WeatherSun {
					chipFraction(s.Active(side), side, 1.0/8, "Solar Power", log)
				}
			},
		},

		// --- switch-in offense pick: Download ---
		"download": {
			// On entry, raise Atk if the foe's Defense is lower than its
			// Sp. Def, otherwise raise Sp. Atk — pick the offense the foe is
			// worse at.
			//
			// The comparison reads stat *stages* as well as the raw stats. It
			// used to read the raw stats alone, on the reasoning that "foes
			// rarely carry boosts at the moment a fresh mon switches in" —
			// true as a frequency claim and not what canon does. Download
			// answers on the numbers actually in front of it, so a Pokemon
			// that has spent the last three turns setting up gets read as the
			// wall it now is. See downloadDefensiveScores for the Wonder Room
			// wrinkle that comes with it.
			Kind: "download",
			OnSwitchIn: func(s *BattleState, side int, log *[]LogLine) {
				foe := s.Active(1 - side)
				if foe.Fainted {
					return
				}
				p := s.Active(side)
				defScore, spdScore := downloadDefensiveScores(foe, &s.PseudoWeather)
				stat, label := "spatk", "Sp. Atk"
				if defScore < spdScore {
					stat, label = "attack", "Attack"
				}
				revealAbility(p)
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
			Kind:              "sand-force",
			BasePowerPriority: bpPrioThirtyPercent,
			BasePowerMult: func(atk *Pokemon, m domain.Move, def *Pokemon, w *WeatherState) float64 {
				if w != nil && w.Kind == WeatherSandstorm &&
					(m.Type == "rock" || m.Type == "ground" || m.Type == "steel") {
					return mod4096(5325)
				}
				return 1
			},
		},

		// --- pinch abilities: ×1.5 to a fixed move type at ≤ 1/3 HP ---
		"blaze":    pinchAbility("blaze", "fire"),
		"torrent":  pinchAbility("torrent", "water"),
		"overgrow": pinchAbility("overgrow", "grass"),
		"swarm":    pinchAbility("swarm", "bug"),

		// --- status-immunity guards ---
		"immunity":     {Kind: "immunity", BlocksStatus: func(st StatusCond) bool { return st == StatusPoison || st == StatusToxic }},
		"limber":       {Kind: "limber", BlocksStatus: func(st StatusCond) bool { return st == StatusParalysis }},
		"water-veil":   {Kind: "water-veil", BlocksStatus: func(st StatusCond) bool { return st == StatusBurn }},
		"magma-armor":  {Kind: "magma-armor", BlocksStatus: func(st StatusCond) bool { return st == StatusFreeze }},
		"insomnia":     {Kind: "insomnia", BlocksStatus: func(st StatusCond) bool { return st == StatusSleep }},
		"vital-spirit": {Kind: "vital-spirit", BlocksStatus: func(st StatusCond) bool { return st == StatusSleep }},
		"sweet-veil":   {Kind: "sweet-veil", BlocksStatus: func(st StatusCond) bool { return st == StatusSleep }},
		// Own Tempo carried only a Kind and a comment saying the guard lived
		// "elsewhere"; it did not, and nothing in the package read the slug, so
		// the ability was inert while describing itself as working. Found by
		// the registry audit AbilityInertReason drives. (Canon also has it
		// refuse Intimidate from Gen 8 on; that half is not modeled.)
		"own-tempo": {Kind: "own-tempo", BlocksConfusion: true},
		"leaf-guard": {
			// Refuses every major status while the sun is up (harsh sunlight
			// in canon; we have one sun tier). Weather-aware, so it uses the
			// state-carrying guard rather than the plain BlocksStatus.
			Kind: "leaf-guard",
			BlocksStatusState: func(s *BattleState, def *Pokemon, st StatusCond) bool {
				// weatherFor, like every other sun/rain-keyed ability: a
				// Utility Umbrella holder is not standing in the sun.
				w := weatherFor(def, effectiveWeather(s))
				return w != nil && w.Kind == WeatherSun
			},
		},
		"early-bird":  {Kind: "early-bird" /* sleep ticks twice as fast; handled in canAct */},
		"damp":        {Kind: "damp" /* blocks Explosion / Self-Destruct / Aftermath; gated in executeMove */},
		"no-guard":    {Kind: "no-guard", NoGuard: true},
		"scrappy":     {Kind: "scrappy" /* Normal/Fighting hit Ghost; lifted in effectivenessWithLifts */},
		"arena-trap":  {Kind: "arena-trap" /* traps grounded foes; enforced in LegalActions */},
		"magnet-pull": {Kind: "magnet-pull" /* traps Steel foes; enforced in LegalActions */},
		"infiltrator": {Kind: "infiltrator" /* ignores screens and substitutes; wired in damage.go / substitute.go */},
		"unburden": {
			// Doubles Speed once the holder has lost its held item. The
			// Volatiles.Unburden flag is armed in consumeItem and cleared on
			// switch-out with the rest of the volatile set.
			//
			// The empty-slot check belongs here rather than in the arming, which
			// is where canon puts it too: the flag records "this Pokémon has lost
			// an item", and the doubling applies only while the slot is *still*
			// empty. A holder that picks something up goes back to base Speed
			// without the flag being cleared, and gets the boost back if it loses
			// that one as well. Sticky Barb's ping-pong is the reachable case —
			// eat a berry, then take the barb off a contact attacker, and the
			// flag alone would leave it at double Speed holding an item.
			Kind: "unburden",
			SpeedMult: func(p *Pokemon, w *WeatherState) float64 {
				if p.Volatiles.Unburden && p.Item == ItemNone {
					return 2
				}
				return 1
			},
		},
		"sand-veil": {
			// +evasion in a sandstorm (moves lose 20% accuracy against it) and
			// immune to sandstorm chip. The evasion boost lives in the
			// defender-side accuracy hook; the chip immunity in weatherResidual.
			Kind: "sand-veil",
			AccuracyMultVs: func(s *BattleState, def *Pokemon, m domain.Move) float64 {
				if w := effectiveWeather(s); w != nil && w.Kind == WeatherSandstorm {
					return 0.8
				}
				return 1
			},
		},
		"snow-cloak": {
			// +evasion while it's snowing (moves lose 20% accuracy).
			Kind: "snow-cloak",
			AccuracyMultVs: func(s *BattleState, def *Pokemon, m domain.Move) float64 {
				if w := effectiveWeather(s); w != nil && w.Kind == WeatherSnow {
					return 0.8
				}
				return 1
			},
		},
		"wonder-skin": {
			// Halves the accuracy of status-category moves aimed at the holder
			// (canon caps them at 50% base; on the common 100-accuracy status
			// move that's the same result). Damaging moves are untouched.
			Kind: "wonder-skin",
			AccuracyMultVs: func(s *BattleState, def *Pokemon, m domain.Move) float64 {
				if m.Category == domain.CatStatus {
					return 0.5
				}
				return 1
			},
		},
		"overcoat": {
			// Immune to weather chip damage (the sand-chip exemption lives in
			// weatherResidual via abilityImmuneToSandstorm) and to powder moves
			// (powderRefusedBy, alongside the Grass-type immunity and Safety
			// Goggles). Both are read off the slug rather than through a hook.
			Kind: "overcoat",
		},
		"tangled-feet": {
			// Evasion doubles while the holder is confused — moves land at half
			// their normal accuracy.
			Kind: "tangled-feet",
			AccuracyMultVs: func(s *BattleState, def *Pokemon, m domain.Move) float64 {
				if def.Volatiles.Confusion != nil {
					return 0.5
				}
				return 1
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
				if !moveMakesContact(m, s.Active(1-defSide)) || !rng.Chance(30) {
					return
				}
				atk := s.Active(1 - defSide)
				if inflictStatusFrom(atk, 1-defSide, defSide, StatusParalysis, s, rng, log) {
					def := s.Active(defSide)
					revealAbility(def)
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
				if !moveMakesContact(m, s.Active(1-defSide)) || !rng.Chance(30) {
					return
				}
				atk := s.Active(1 - defSide)
				if inflictStatusFrom(atk, 1-defSide, defSide, StatusBurn, s, rng, log) {
					def := s.Active(defSide)
					revealAbility(def)
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
				if !moveMakesContact(m, s.Active(1-defSide)) || !rng.Chance(30) {
					return
				}
				atk := s.Active(1 - defSide)
				if inflictStatusFrom(atk, 1-defSide, defSide, StatusPoison, s, rng, log) {
					def := s.Active(defSide)
					revealAbility(def)
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
				if !moveMakesContact(m, s.Active(1-defSide)) || !rng.Chance(30) {
					return
				}
				// Pick one of three outcomes uniformly (canon is 9/9/11/71
				// for sleep/para/poison/nothing, but inside the 30% trigger
				// it's roughly 1/3 each; we do exactly 1/3 for clarity).
				atk := s.Active(1 - defSide)
				roll := rng.IntN(3)
				// Effect Spore is a powder effect (Gen VI+), so Grass types,
				// Overcoat and Safety Goggles are immune to it. Checked after
				// both rolls, never before: the guard must not change which
				// numbers the rest of the turn draws. nil breaker — Mold
				// Breaker only ignores abilities for its holder's own moves,
				// and this is an ability rider, not a move.
				if _, immune := powderImmuneBy(nil, atk); immune {
					return
				}
				switch roll {
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
			// Contact rider: 30% chance to infatuate the attacker. Does NOT fire
			// through a substitute — applyOnHit refuses every on-hit ability when
			// the doll took the blow. This comment used to assert the opposite
			// and describe it as deliberate; it was wrong, and Cute Charm was the
			// fifth contact rider affected by that reading, not a special case.
			Kind: "cute-charm",
			OnHit: func(s *BattleState, defSide int, m domain.Move, _ bool, rng *RNG, log *[]LogLine) {
				if !moveMakesContact(m, s.Active(1-defSide)) || !rng.Chance(30) {
					return
				}
				atk := s.Active(1 - defSide)
				// Same gender rule Attract follows: the holder is the source of
				// the infatuation, the contact attacker is the target.
				if !gendersAttract(s.Active(defSide), atk) {
					return
				}
				if atk.Volatiles.Attract {
					return
				}
				atk.Volatiles.Attract = true
				def := s.Active(defSide)
				revealAbility(def)
				*log = append(*log, LogLine{
					Type: "ability", Side: defSide,
					Text: fmt.Sprintf("%s's Cute Charm infatuated %s!", def.Name, atk.Name),
				})
			},
		},

		"poison-touch": {
			// Attacker-side contact rider: the holder's contact moves have a 30%
			// chance to poison the target. Like every contact rider it cannot
			// reach a target behind a substitute (the doll took the touch).
			//
			// A note here used to say that because this is the ability's own
			// effect rather than a move secondary, Shield Dust on the target
			// does not suppress it. Upstream disagrees, in a comment written for
			// exactly that confusion: "Despite not being a secondary, Shield
			// Dust / Covert Cloak block Poison Touch's effect", followed by the
			// check. So both predicates are consulted below.
			Kind: "poison-touch",
			OnDealDamage: func(s *BattleState, atkSide int, m domain.Move, rng *RNG, log *[]LogLine) {
				// Poison Touch is the attacker's own rider, so the item that can
				// suppress contact is the attacker's — not the defender's.
				if !moveMakesContact(m, s.Active(atkSide)) || !rng.Chance(30) {
					return
				}
				def := s.Active(1 - atkSide)
				if def.Fainted || def.HP <= 0 || hasSubstitute(def) {
					return
				}
				if abilityBlocksSecondaries(s, def) || itemBlocksSecondaries(def) {
					return
				}
				// The poison is foe-caused (the attacker inflicts it), so route it
				// through inflictStatusFrom: a target holding Synchronize bounces
				// the poison back onto the Poison Touch attacker, same as any other
				// opponent-inflicted status.
				inflictStatusFrom(def, 1-atkSide, atkSide, StatusPoison, s, rng, log)
			},
		},

		"stench": {
			// Attacker-side rider: every damaging move the holder lands has a
			// 10% chance to make the target flinch. Reaches only a
			// directly-struck target, never one behind a substitute, and Inner
			// Focus still blocks the flinch via applyFlinchVolatile.
			//
			// This carried the same wrong note Poison Touch did — that Shield
			// Dust does not suppress it. Upstream's Stench is not the ability's
			// own effect at all: onModifyMove *pushes* a real
			// {chance: 10, volatileStatus: 'flinch'} onto move.secondaries, so
			// everything that refuses added effects refuses it. It also declines
			// to stack on a move that already flinches, which is why
			// moveAlreadyFlinches is consulted here — the same predicate the
			// flinch items use, for the same reason.
			Kind: "stench",
			OnDealDamage: func(s *BattleState, atkSide int, m domain.Move, rng *RNG, log *[]LogLine) {
				if moveAlreadyFlinches(m) || !rng.Chance(10) {
					return
				}
				def := s.Active(1 - atkSide)
				if def.Fainted || def.HP <= 0 || hasSubstitute(def) {
					return
				}
				if abilityBlocksSecondaries(s, def) || itemBlocksSecondaries(def) {
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
				revealAbility(p)
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
				revealAbility(p)
				*log = append(*log, LogLine{
					Type: "ability", Side: defSide,
					Text: fmt.Sprintf("%s's Weak Armor shifted its build!", p.Name),
				})
				applyStages(p, defSide, "defense", -1, log)
				applyStages(p, defSide, "speed", 2, log)
			},
		},

		"cursed-body": {
			// When the holder is struck by a damaging move, 30% chance to
			// disable that move on the attacker for a few turns. Skips a hit
			// soaked by a substitute (the holder wasn't really struck) and a
			// move the attacker no longer knows or that's already disabled.
			Kind: "cursed-body",
			OnHit: func(s *BattleState, defSide int, m domain.Move, hitSub bool, rng *RNG, log *[]LogLine) {
				if hitSub || m.ID == "" || !rng.Chance(30) {
					return
				}
				atk := s.Active(1 - defSide)
				if atk.Fainted || atk.Volatiles.Disable != nil || !knowsMove(atk, m.ID) {
					return
				}
				atk.Volatiles.Disable = &DisableState{MoveID: m.ID, MoveName: m.Name, Turns: defaultDisableTurns}
				def := s.Active(defSide)
				*log = append(*log, LogLine{
					Type: "disable", Side: defSide,
					Text: fmt.Sprintf("%s's Cursed Body disabled %s's %s!", def.Name, atk.Name, m.Name),
				})
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
				revealAbility(p)
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
				revealAbility(p)
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
				revealAbility(p)
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
				if !moveMakesContact(m, atk) || atk.Fainted || abilityBlocksIndirectDamage(atk) || dampActive(s) {
					return
				}
				chipFraction(atk, atkSide, 0.25, "Aftermath", log)
			},
		},
		"moxie": {
			Kind: "moxie",
			OnKO: func(s *BattleState, side int, log *[]LogLine) {
				p := s.Active(side)
				revealAbility(p)
				*log = append(*log, LogLine{
					Type: "ability", Side: side,
					Text: fmt.Sprintf("%s's Moxie raised its Attack!", p.Name),
				})
				applyStages(p, side, "attack", 1, log)
			},
		},

		// --- end-of-turn ticks ---
		"harvest": {
			// Regrows the Berry the holder most recently ate: every turn in
			// harsh sunlight, half the time otherwise. Refuses while the holder
			// is already carrying something, so it restocks an empty slot
			// rather than duplicating a berry.
			//
			// The restore is Recycle's, reading the LastConsumedItem that
			// consumeItem records — and, like Recycle, giveItem then clears
			// that memory. Harvest still chains across turns because eating the
			// regrown berry writes the slug back.
			Kind: "harvest",
			EndOfTurn: func(s *BattleState, side int, rng *RNG, log *[]LogLine) {
				p := s.Active(side)
				if p.Item != ItemNone || p.LastConsumedItem == ItemNone {
					return
				}
				// Berries only: a spent White Herb or Focus Sash stays spent.
				// An unmodeled slug has no registry record and no Berry flag,
				// which is the right answer for it too.
				if it := itemRegistry[p.LastConsumedItem]; it == nil || !it.Berry {
					return
				}
				sun := false
				if w := effectiveWeather(s); w != nil && w.Kind == WeatherSun {
					sun = true
				}
				if !sun && !rng.Chance(50) {
					return
				}
				kind := p.LastConsumedItem
				giveItem(p, kind)
				revealItem(p)
				revealAbility(p)
				*log = append(*log, LogLine{
					Type: "ability", Side: side,
					Text: fmt.Sprintf("%s harvested one %s!", p.Name, itemDisplayName(kind)),
				})
			},
		},
		"speed-boost": {
			Kind: "speed-boost",
			EndOfTurn: func(s *BattleState, side int, _ *RNG, log *[]LogLine) {
				p := s.Active(side)
				revealAbility(p)
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
				if w := weatherFor(s.Active(side), effectiveWeather(s)); w != nil && w.Kind == WeatherRain {
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
			// The Fire half is a base-power handler on the *defender*
			// (`onSourceBasePower`, priority 17), not an incoming-damage
			// multiplier. The distinction is not cosmetic: as an incoming
			// multiplier it scaled the finished figure, `+2` and all, where
			// canon scales the base power the Fire move started from.
			SourceBasePowerPriority: bpPrioDrySkin,
			SourceBasePowerMult: func(atk *Pokemon, m domain.Move, def *Pokemon, w *WeatherState) float64 {
				if m.Type == "fire" {
					return 1.25
				}
				return 1
			},
			EndOfTurn: func(s *BattleState, side int, _ *RNG, log *[]LogLine) {
				w := weatherFor(s.Active(side), effectiveWeather(s))
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
				revealAbility(p)
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
				if w := weatherFor(p, effectiveWeather(s)); w == nil || w.Kind != WeatherRain {
					return
				}
				clearStatus(p)
				revealAbility(p)
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
				revealAbility(p)
				*log = append(*log, LogLine{
					Type: "ability", Side: side,
					Text: fmt.Sprintf("%s's Natural Cure healed its status!", p.Name),
				})
			},
		},
		"regenerator": {
			Kind: "regenerator",
			OnSwitchOut: func(p *Pokemon, side int, log *[]LogLine) {
				// Heal Block still applies: onSwitchOut runs before
				// clearVolatile, so the block is still standing when canon
				// asks. A Regenerator pivot under Heal Block comes back at
				// the HP it left on.
				if p.HP >= p.MaxHP || healBlocked(p) {
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
				revealAbility(p)
				*log = append(*log, LogLine{
					Type: "ability", Side: side,
					Text: fmt.Sprintf("%s restored HP with Regenerator (+%d).", p.Name, amt),
				})
			},
		},

		// --- misc ---
		"skill-link":  {Kind: "skill-link", MaxesMultihit: true},
		"liquid-ooze": {Kind: "liquid-ooze", DrainBackfires: true},
		"unaware":     {Kind: "unaware", IgnoresOpponentStages: true},
		// Simple is on no species in this dex; it exists because Simple Beam
		// sets it, and a move that hands out an ability nobody implements is a
		// move that narrates success and does nothing.
		"simple":       {Kind: AbilitySimple, StageDeltaMult: 2},
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
		// Guts is Atk ×1.5 while statused and nothing else. It used to be Atk
		// ×1.5 *plus* a ×2 on the finished damage to cancel the burn halve this
		// engine applied to the Attack stat — three multipliers standing in for
		// canon's one, and landing on neither of canon's numbers. Canon's burn
		// halve is a damage modifier that simply does not fire for a Guts
		// holder (`!pokemon.hasAbility('guts')` in modifyDamage), so the
		// cancellation has nothing left to cancel; see burnHalvesDamage.
		//
		// onModifyAtk only, so the category test is the ability's own.
		"guts": {
			Kind:            "guts",
			AtkStatPriority: statPrioAbility,
			AtkStatMult: func(atk *Pokemon, m domain.Move, def *Pokemon, w *WeatherState) float64 {
				if atk.Status == StatusNone || m.Category != domain.CatPhysical {
					return 1
				}
				return 1.5
			},
		},
		"steadfast": {
			Kind: "steadfast",
			OnFlinched: func(p *Pokemon, side int, log *[]LogLine) {
				revealAbility(p)
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
//
// A suppressed ability (Neutralizing Gas on the field, or a Gastro Acid landed
// on this Pokémon) reads as nil here, which is the whole of how suppression
// works: one gate on the single lookup every mechanic already goes through,
// rather than 62 call sites each learning to ask. See abilitysuppression.go.
//
// p.Ability is deliberately untouched by suppression — the Pokémon still *has*
// the ability, and the fog-of-war reveal set, the snapshot and Trace's
// uncopiable list all still need to see it.
func abilityOf(p *Pokemon) *Ability {
	if p == nil || p.Volatiles.AbilitySuppressed {
		return nil
	}
	return abilityRegistry[p.Ability]
}

// abilityBreaksMold reports whether the attacker's ability makes its moves
// ignore the target's damage-affecting defensive abilities (Mold Breaker).
func abilityBreaksMold(atk *Pokemon) bool {
	a := abilityOf(atk)
	return a != nil && a.BreaksMold
}

// abilitySuppressed reports whether p's ability is switched off for the move
// currently resolving — Showdown's Battle#suppressingAbility. A mold-breaking
// attacker suppresses every other Pokemon's ability for as long as its move is
// resolving; its own is untouched, which is why a Mold Breaker user keeps its
// own defensive abilities on the turn it attacks.
//
// This is the reach half of Mold Breaker. The flag itself was always right;
// what was wrong was that it was consulted at five hand-placed call sites
// rather than being a fact about the field, so anything not on that list —
// Shield Dust, Clear Body, Sticky Hold, Damp, a Levitate holder dragged onto
// Spikes — was unreachable. Predicates that decide a defender-side question
// take the state now and ask here.
//
// One known narrowing: groundedness (terrain.go) does not consult this, so a
// mold breaker's move does not suppress Levitate for the *terrain* multipliers
// the way upstream's isGrounded does. The hazard path does — that is the case
// upstream has a test for — and the terrain leg would need the battle state
// threaded through a helper that computeDamage deliberately calls without one.
func abilitySuppressed(s *BattleState, p *Pokemon) bool {
	return s != nil && s.moldBreaker != nil && s.moldBreaker != p
}

// itemDisplayName turns an item slug ("choice-band") into a human label
// ("Choice Band") for log lines.
//
// Prefers the registry's own Name, which is the catalog's exact string and the
// only way to get apostrophes and casing right — title-casing the slug gives
// "King S Rock" and "Never Melt Ice". Falls back to title-casing for a slug the
// engine catalogs but does not model, so an unmodeled item still reads as
// something rather than as an empty string.
func itemDisplayName(k ItemKind) string {
	if it := itemRegistry[k]; it != nil && it.Name != "" {
		return it.Name
	}
	parts := strings.Split(string(k), "-")
	for i, p := range parts {
		if p != "" {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, " ")
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
	user := s.Active(side)
	// The setter's rock extends an ability-spawned weather exactly as it does a
	// move-spawned one: canon hangs the extension off each weather condition's
	// durationCallback, which every setWeather caller runs through. Drought +
	// Heat Rock for eight turns is the whole reason to hold the rock.
	s.Weather = &WeatherState{Kind: kind, TurnsLeft: weatherTurnsFor(user, defaultWeatherTurns, kind)}
	revealAbility(user)
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
	revealAbility(p)
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
	revealAbility(p)
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
func pinchBoost(t domain.Type) func(atk *Pokemon, m domain.Move, def *Pokemon, w *WeatherState) float64 {
	return func(atk *Pokemon, m domain.Move, def *Pokemon, w *WeatherState) float64 {
		if m.Type == t && atk.HP*3 <= atk.MaxHP {
			return 1.5
		}
		return 1
	}
}

// pinchAbility is the whole registry entry. All four register onModifyAtk and
// onModifySpA upstream, so the boost is a stat modifier and the dispatcher
// picks the stat from the move's category — which is why the closure above
// tests only the type and the HP.
func pinchAbility(kind AbilityKind, t domain.Type) *Ability {
	return &Ability{
		Kind:            kind,
		AtkStatPriority: statPrioAbility,
		AtkStatMult:     pinchBoost(t),
	}
}

// healFraction heals p for frac of MaxHP, clamped to MaxHP. Used by
// end-of-turn healers (Rain Dish, Ice Body, Dry Skin in rain).
func healFraction(p *Pokemon, side int, frac float64, why string, log *[]LogLine) {
	if p.HP >= p.MaxHP || healBlocked(p) {
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
	revealAbility(p)
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
	hurt(p, amt)
	revealAbility(p)
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

// abilityAccuracyMultVs returns the defender-side accuracy multiplier the
// target's ability imposes (evasion abilities, Wonder Skin). 1.0 when unset.
func abilityAccuracyMultVs(s *BattleState, def *Pokemon, m domain.Move) float64 {
	if a := abilityOf(def); a != nil && a.AccuracyMultVs != nil {
		return a.AccuracyMultVs(s, def, m)
	}
	return 1
}

// abilityImmuneToSandstorm reports whether p's ability shields it from
// sandstorm chip damage (Sand Veil, Sand Rush, Sand Force, Overcoat). Rock /
// Ground / Steel typing is handled separately in weatherResidual.
func abilityImmuneToSandstorm(p *Pokemon) bool {
	a := abilityOf(p)
	if a == nil {
		return false
	}
	switch a.Kind {
	case "sand-veil", "sand-rush", "sand-force", "overcoat":
		return true
	}
	return false
}

// abilityNoGuard reports whether p's ability makes moves to and from it
// always land (No Guard).
func abilityNoGuard(p *Pokemon) bool {
	if a := abilityOf(p); a != nil {
		return a.NoGuard
	}
	return false
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
func abilityBlocksSecondaries(s *BattleState, def *Pokemon) bool {
	if abilitySuppressed(s, def) {
		return false
	}
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

// abilityTraceable reports whether an ability can be copied by Trace. An
// empty slot and the handful of canonically-uncopiable abilities we model
// (Trace itself, Neutralizing Gas) return false; everything else is fair game.
func abilityTraceable(k AbilityKind) bool {
	switch k {
	case AbilityNone, "trace", AbilityNeutralizingGas:
		return false
	}
	return true
}

// prettyAbilityName turns an ability slug ("flame-body") into its display
// form ("Flame Body") for log lines.
func prettyAbilityName(k AbilityKind) string {
	words := strings.Split(string(k), "-")
	for i, w := range words {
		if w == "" {
			continue
		}
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}

// abilityTrapsSwitch reports whether the active Pokémon on `side` is barred
// from switching out by the foe's ability: Arena Trap holds grounded foes,
// Magnet Pull holds Steel foes. Ghost-types ignore trapping entirely. Called
// from LegalActions alongside the partial-trap / Ingrain checks.
func abilityTrapsSwitch(s *BattleState, side int) bool {
	p := s.Active(side)
	if isType(p, "ghost") {
		return false
	}
	foe := s.Active(1 - side)
	if foe.Fainted {
		return false
	}
	a := abilityOf(foe)
	if a == nil {
		return false
	}
	switch a.Kind {
	case "arena-trap":
		return isGrounded(p, &s.PseudoWeather)
	case "magnet-pull":
		return isType(p, "steel")
	}
	return false
}

// dampActive reports whether either active Pokémon has Damp, which fizzles
// Explosion / Self-Destruct and suppresses Aftermath's contact chip.
func dampActive(s *BattleState) bool {
	for i := 0; i < 2; i++ {
		p := s.Active(i)
		if p.Fainted || abilitySuppressed(s, p) {
			continue
		}
		if a := abilityOf(p); a != nil && a.Kind == "damp" {
			return true
		}
	}
	return false
}

// abilityInfiltrator reports whether p's ability lets its moves pass through
// the foe's screens and substitute (Infiltrator).
func abilityInfiltrator(p *Pokemon) bool {
	a := abilityOf(p)
	return a != nil && a.Kind == "infiltrator"
}

// abilityScrappy reports whether p's ability lets its Normal / Fighting moves
// hit Ghost-types (Scrappy). Consulted by effectivenessWithLifts.
func abilityScrappy(p *Pokemon) bool {
	a := abilityOf(p)
	return a != nil && a.Kind == "scrappy"
}

// abilityIsEarlyBird reports whether p's ability makes it sleep off status
// twice as fast (Early Bird). Consulted in canAct's sleep branch.
func abilityIsEarlyBird(p *Pokemon) bool {
	a := abilityOf(p)
	return a != nil && a.Kind == "early-bird"
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

// abilityBlocksConfusion reports whether def's ability refuses confusion
// (Own Tempo). Silent, like the status-immunity guards inflictStatus consults:
// the engine says nothing, so the foe learns nothing.
func abilityBlocksConfusion(def *Pokemon) bool {
	if a := abilityOf(def); a != nil {
		return a.BlocksConfusion
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
// successful hit.
//
// A hit a Substitute absorbed does not reach the holder, so no on-hit ability
// fires: canon's substitute handles the damage in onTryPrimaryHit and the
// DamagingHit event never runs for the target. The guard is here rather than in
// each hook because five of the six forgot it — Static, Flame Body, Poison
// Point, Effect Spore and Cute Charm all paralyzed, burned, poisoned and
// infatuated attackers through a doll, and Cute Charm's comment described that
// as deliberate. Cursed Body checks hitSub itself as well; that is now redundant
// and left in place as documentation of the contract.
//
// hitSub is still passed through so a future hook can distinguish the cases if
// one ever legitimately needs to.
func applyOnHit(s *BattleState, defSide int, m domain.Move, hitSub bool, rng *RNG, log *[]LogLine) {
	def := s.Active(defSide)
	if def.Fainted || hitSub {
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
//
// Nor does the KO that wins the battle. Canon reaches Moxie through
// onSourceAfterFaint, and faintMessages runs `if (checkWin && this.checkWin())
// return true` on the line *before* runEvent('AfterFaint')
// (ps/sim/battle.ts:2598) — so a battle-ending faint returns early and the
// event never happens. The last KO of a sweep does not boost.
//
// The check asks both sides, matching checkWin: it declares a winner when
// either side is out, so a Destiny Bond or Aftermath double-KO that empties
// both benches ends the battle too, and nothing collects.
//
// This has to be tested here rather than left to updatePhase, which is the
// engine's checkWin and runs at the very end of ResolveTurn — by which point
// the boost has long since been applied and logged.
func applyOnKO(s *BattleState, side int, log *[]LogLine) {
	atk := s.Active(side)
	if atk.Fainted || atk.HP <= 0 {
		return
	}
	if s.LiveCount(0) == 0 || s.LiveCount(1) == 0 {
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
func abilityBlocksStatLowerByFoe(s *BattleState, def *Pokemon, stat string) bool {
	if abilitySuppressed(s, def) {
		return false
	}
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

// revealAbility marks p's ability as public knowledge. Called wherever the
// engine announces the ability doing something — that announcement is exactly
// the in-battle event canon treats as the reveal.
//
// Idempotent and one-way. Silent ability reads (a damage multiplier, a status
// immunity that produces no line) deliberately do NOT reveal: if the engine
// said nothing, the foe saw nothing.
func revealAbility(p *Pokemon) {
	if p != nil {
		p.AbilityRevealed = true
	}
}
