// Package engine is the Pokémon battle engine. It is a pure library: given a
// battle state and the two players' actions it produces the next state and a
// turn log, with no I/O. Determinism comes from the seeded RNG carried inside
// the state, so any battle replays exactly from its seed.
package engine

import (
	"errors"
	"fmt"

	"pokearena/internal/domain"
)

// Phase is the part of the turn cycle a battle is waiting on.
type Phase string

const (
	PhaseChoosing Phase = "choosing" // both sides choose an action
	PhaseReplace  Phase = "replace"  // a fainted active must be replaced
	PhaseEnded    Phase = "ended"    // the battle is over
)

// StatusCond is a non-volatile status condition. A Pokémon has at most one.
// Toxic is a distinct status from regular Poison: residual damage escalates
// each turn the holder is poisoned (tracked by Pokemon.ToxicCounter).
type StatusCond string

const (
	StatusNone      StatusCond = ""
	StatusBurn      StatusCond = "burn"
	StatusPoison    StatusCond = "poison"
	StatusToxic     StatusCond = "toxic"
	StatusParalysis StatusCond = "paralysis"
	StatusSleep     StatusCond = "sleep"
	StatusFreeze    StatusCond = "freeze"
)

// Stages holds the -6..+6 stat-stage modifiers, reset on switch-out. Acc and
// Eva use a different multiplier curve than the offensive/speed stages — see
// damage.go.
type Stages struct {
	Atk int `json:"atk"`
	Def int `json:"def"`
	SpA int `json:"spa"`
	SpD int `json:"spd"`
	Spe int `json:"spe"`
	Acc int `json:"acc"`
	Eva int `json:"eva"`
}

// ConfusionState is the state of a confused Pokémon. Turns is the number of
// turns of confusion remaining (initially 2-5); it decrements at the start of
// the owner's move attempt and the confusion clears when it reaches zero.
type ConfusionState struct {
	Turns int `json:"turns"`
}

// ChargingState is the state of a Pokémon locked into a two-turn move (Solar
// Beam, Fly, Dig, ...). Set on the charge turn; cleared after the strike
// turn fires or the Pokémon switches out. MoveIdx is the slot index of the
// charging move so the strike resolves against the same move regardless of
// what the controller submits next turn.
type ChargingState struct {
	MoveIdx int `json:"move_idx"`
}

// LockedMoveState is the state of a Pokémon mid-rampage on Outrage, Thrash, or
// Petal Dance. MoveIdx is the slot the user is locked into (PP was paid on the
// first turn only); Turns is how many more times — including the current one —
// the move is forced before the user collapses into fatigue confusion. Set on
// the first use, decremented at the end of every turn the user acts, and
// cleared when it reaches zero (or when the user is interrupted or switches
// out). See lockedmove.go.
type LockedMoveState struct {
	MoveIdx int `json:"move_idx"`
	Turns   int `json:"turns"`
}

// PartialTrapState is the state of a Pokémon caught by a partial-trap move
// (Bind, Wrap, Fire Spin, Whirlpool, Clamp, Sand Tomb, Infestation). The
// target takes 1/8 max-HP chip per end-of-turn and cannot switch out until
// Turns reaches zero. MoveName flavors the inflict / residual / release
// log lines so they read like Showdown's "X was trapped by Bind!" rather
// than the generic volatile name.
// ChipDenom is the divisor for the per-turn chip: 8 normally, 6 when the
// trapper held a Binding Band. It is snapshotted at the moment the trap lands
// rather than read from the trapper each turn, because the trapper can switch
// out (or lose the item) long before the trap expires — and canon fixes the
// figure at application time too. Zero is read as the default, so a state
// deserialized from before this field existed still chips correctly.
type PartialTrapState struct {
	Turns     int    `json:"turns"`
	MoveName  string `json:"move_name"`
	ChipDenom int    `json:"chip_denom,omitempty"`
}

// partialTrapDenom is the default per-turn chip divisor for a partial trap.
const partialTrapDenom = 8

// Chip returns the per-turn damage this trap deals to a Pokémon with maxHP.
func (pt *PartialTrapState) Chip(maxHP int) int {
	denom := pt.ChipDenom
	if denom <= 0 {
		denom = partialTrapDenom
	}
	dmg := maxHP / denom
	if dmg < 1 {
		dmg = 1
	}
	return dmg
}

// Volatiles is the bag of volatile conditions on a Pokémon. Stateful volatiles
// are pointer-or-nil (nil = absent); transient ones are bool. All clear on
// switch-out via clearVolatiles.
type Volatiles struct {
	Confusion    *ConfusionState   `json:"confusion,omitempty"`
	Flinch       bool              `json:"flinch,omitempty"`
	Charging     *ChargingState    `json:"charging,omitempty"`
	LockedMove   *LockedMoveState  `json:"locked_move,omitempty"`
	MustRecharge bool              `json:"must_recharge,omitempty"`
	PartialTrap  *PartialTrapState `json:"partial_trap,omitempty"`
	Substitute   *SubstituteState  `json:"substitute,omitempty"`
	// Protect / Endure: one-shot shields set by stall moves. Protect blocks
	// foe-targeted moves outright; Endure clamps lethal damage so the user
	// survives at 1 HP. Both clear at end of turn in the transient sweep.
	// ProtectCounter is the consecutive-success count shared by Protect /
	// Detect / Endure — it drives protectChance's 1/3^n curve and resets
	// when the user takes any non-stall action (handled by the defer in
	// executeMove). Unlike the bools, the counter persists across turns
	// while the user keeps stacking stalls.
	Protect        bool `json:"protect,omitempty"`
	Endure         bool `json:"endure,omitempty"`
	ProtectCounter int  `json:"protect_counter,omitempty"`
	// Roost: the user spent this turn roosting, suppressing its Flying type
	// for incoming-damage effectiveness (a grounded Charizard takes Ground
	// hits, Rock drops from 4× to 2×, etc.). One-shot — cleared in the
	// end-of-turn transient sweep. See roost.go.
	Roost bool `json:"roost,omitempty"`
	// FlashFireCharged: Flash Fire was triggered by absorbing a Fire move.
	// Boosts the holder's own Fire-type damage by 1.5× until switch-out.
	FlashFireCharged bool `json:"flash_fire_charged,omitempty"`
	// LeechSeed / AquaRing / Ingrain: residual-heal volatiles. Leech
	// Seed chips the holder 1/8 and heals the seeding side's active;
	// Aqua Ring and Ingrain heal the holder 1/16 each end-of-turn.
	// Ingrain additionally roots the holder — switching is blocked
	// via ingrainBlocksSwitch in LegalActions. See drainvolatiles.go.
	LeechSeed *LeechSeedState `json:"leech_seed,omitempty"`
	AquaRing  bool            `json:"aqua_ring,omitempty"`
	Ingrain   bool            `json:"ingrain,omitempty"`
	// Trapped is move-based trapping (Mean Look, Block): the holder may not
	// switch out. Distinct from PartialTrap, which also chips and expires on
	// its own — this one lasts until somebody faints, since the only thing
	// that clears it is a switch the holder can't make.
	Trapped bool `json:"trapped,omitempty"`
	// PerishSong is the countdown Perish Song leaves on both actives. Ticks
	// at end of turn; the holder faints at zero. Cleared by switching out
	// with the rest of this bag, which is the move's whole counterplay.
	PerishSong *PerishState `json:"perish_song,omitempty"`
	// Lock/restrict volatiles (see lockrestrict.go). All gate which
	// move the holder may pick this turn: Disable bans one slug for 4
	// turns, Encore forces one slug for 3 turns, Taunt blocks status
	// for 3 turns, Embargo suppresses the holder's item for 5 turns
	// (enforced in itemSuppressed), Torment blocks the same move twice in a
	// row (indefinite), Imprison lives on the imprisoner and refuses
	// foe-side moves whose slug is in the snapshot (indefinite).
	Disable  *DisableState  `json:"disable,omitempty"`
	Encore   *EncoreState   `json:"encore,omitempty"`
	Taunt    *TauntState    `json:"taunt,omitempty"`
	Torment  bool           `json:"torment,omitempty"`
	Imprison *ImprisonState `json:"imprison,omitempty"`
	Embargo  *EmbargoState  `json:"embargo,omitempty"`
	// LastMoveID / LastMoveName: the slug + display name of the move
	// the holder used most recently this battle. Set in executeMove
	// after choosePP (so a missed attempt still updates it, matching
	// canon). Cleared on switch-out via the Volatiles wipe. Consumed
	// by Disable / Encore / Torment for "the last move you used" logic.
	LastMoveID   string `json:"last_move_id,omitempty"`
	LastMoveName string `json:"last_move_name,omitempty"`
	// Aim / stat volatiles (see aim.go). All are persistent-until-
	// switch except Charge / LaserFocus which are one-shot consumed.
	// FocusEnergy: +2 crit-ratio stages. LaserFocus: next move auto-
	// crits. Charge: next damaging move's BP ×2 if Electric-type.
	// DefenseCurl / Minimize: flag-only (boost handled via Effect.
	// Boosts upstream; Rollout / Body Slam doublings not modeled).
	// Foresight / MiracleEye: zero positive evasion and lift the
	// matching type-chart immunity (Ghost vs Normal/Fighting; Dark
	// vs Psychic).
	FocusEnergy bool `json:"focus_energy,omitempty"`
	LaserFocus  bool `json:"laser_focus,omitempty"`
	Charge      bool `json:"charge,omitempty"`
	DefenseCurl bool `json:"defense_curl,omitempty"`
	Minimize    bool `json:"minimize,omitempty"`
	Foresight   bool `json:"foresight,omitempty"`
	MiracleEye  bool `json:"miracle_eye,omitempty"`
	// Status-adjacent volatiles (see statusvols.go). Each has its own
	// per-turn behavior; all clear on switch-out via the Volatiles
	// wipe. Attract is degraded (gender check skipped — gender isn't
	// modeled). Yawn is a 2-tick delayed Sleep. Nightmare chips a
	// sleeping holder. Curse chips the foe-cursed target. DestinyBond
	// KOs the attacker if the holder faints to a direct attack this
	// turn, and clears at end-of-turn either way.
	Attract     bool       `json:"attract,omitempty"`
	Yawn        *YawnState `json:"yawn,omitempty"`
	Nightmare   bool       `json:"nightmare,omitempty"`
	Curse       bool       `json:"curse,omitempty"`
	DestinyBond bool       `json:"destiny_bond,omitempty"`
	// Gimmick volatiles (see gimmicks.go). MagnetRise and
	// Telekinesis are pointer-or-nil timers; SmackDown / Snatch /
	// MagicCoat / Grudge / GastroAcid are bool flags. Stockpile
	// carries a 1..3 stack counter.
	MagnetRise  *MagnetRiseState  `json:"magnet_rise,omitempty"`
	Telekinesis *TelekinesisState `json:"telekinesis,omitempty"`
	SmackDown   bool              `json:"smack_down,omitempty"`
	Snatch      bool              `json:"snatch,omitempty"`
	MagicCoat   bool              `json:"magic_coat,omitempty"`
	Stockpile   *StockpileState   `json:"stockpile,omitempty"`
	Grudge      bool              `json:"grudge,omitempty"`
	GastroAcid  bool              `json:"gastro_acid,omitempty"`
	// Unburden: set when an Unburden holder loses its held item, doubling
	// its Speed until it switches out (which clears the whole volatile set).
	Unburden bool `json:"unburden,omitempty"`
	// MagicRoomHere mirrors s.PseudoWeather.MagicRoom onto the active Pokémon.
	// itemOf has no BattleState in hand and 51 call sites, several of them in
	// hook signatures that carry only the Pokémon — so field-wide item
	// suppression is mirrored here rather than threaded. syncMagicRoomFlags owns
	// every write.
	//
	// ValidateStateInvariants checks the mirror against the field, which is what
	// makes a mirror defensible — but note it is called from tests only, not
	// from ResolveTurn, so a desync is caught by the suite rather than at
	// runtime. TestStateInvariantsAcrossManySeeds asserts it after every turn
	// and every replace of a full battle, and the Magic Room tests assert it
	// either side of the setter, a switch-in, and the expiry.
	MagicRoomHere bool `json:"magic_room_here,omitempty"`
	// MovedLast: this Pokémon is the last scheduled mover this turn. Set in
	// the move-resolution loop before executeMove runs for the last entry of
	// the ordered slice; read by Analytic; cleared in the end-of-turn sweep.
	MovedLast bool `json:"moved_last,omitempty"`
	// MovedThisTurn: the holder has already resolved its move this turn. Set by
	// the mover loop right after executeMove returns and cleared in the
	// transient sweep, so it only ever describes the turn in progress. Zoom Lens
	// reads it on the *target* to decide whether its holder is moving second.
	MovedThisTurn bool `json:"moved_this_turn,omitempty"`
	// DamagedThisTurn: the holder took direct move damage earlier this turn.
	// Set in dealDamage when HP is lost; drives Revenge / Avalanche (×2 BP)
	// and Focus Punch (loses focus and fails). Cleared in the end-of-turn
	// sweep so it only ever reflects the turn in progress.
	DamagedThisTurn bool `json:"damaged_this_turn,omitempty"`
	// CustapBoost: the holder's Custap Berry activated this turn, so it moves
	// first inside its priority bracket. Armed at the top of ResolveTurn (the
	// berry is already consumed by then), read by goesFirst, and cleared in the
	// end-of-turn transient sweep — it is single-turn scheduling state, like
	// MovedLast.
	CustapBoost bool `json:"custap_boost,omitempty"`
	// MicleTurns: end-of-turn ticks a primed Micle Berry has left. Unlike
	// CustapBoost this is not single-turn scheduling state — the prime survives
	// into the following turn so the holder can actually spend it on a move —
	// but it is not indefinite either: canon gives the volatile a duration of
	// 2, so a holder that never gets a move off loses the boost instead of
	// banking it through a long sleep. Consumed by resolveAccuracy on the next
	// move that actually rolls accuracy; ticked down in the transient sweep.
	MicleTurns int `json:"micle_turns,omitempty"`
	// MetronomeMoveID / MetronomeCount: the Metronome item's consecutive-use
	// streak — the slug the holder has been repeating and how many *prior*
	// consecutive uses it has, so the first use is unboosted and each repeat
	// adds 20% to a 2x ceiling. Reset by any different move, and cleared on
	// switch-out with the rest of Volatiles, so the streak can't outlive the
	// Pokémon that earned it.
	MetronomeMoveID string `json:"metronome_move_id,omitempty"`
	MetronomeCount  int    `json:"metronome_count,omitempty"`
	// ChoiceLockMoveID: a held Choice item (Choice Band today) locks the
	// holder into the first move it uses; this is that move's slug. Set in
	// executeMove on the first use, enforced by LegalActions (only that slot
	// is offered) and executeMove (the submitted index is redirected to it).
	// Cleared on switch-out with the rest of Volatiles. Empty = not locked.
	ChoiceLockMoveID string `json:"choice_lock_move_id,omitempty"`
}

// MoveSlot is one of a Pokémon's (up to four) moves with its remaining PP.
type MoveSlot struct {
	MoveID string `json:"move_id"`
	PP     int    `json:"pp"`
	MaxPP  int    `json:"max_pp"`
}

// Pokemon is a battle instance of a species: derived stats and live state.
//
// SleepTurns is meaningful only when Status==StatusSleep. ToxicCounter is
// meaningful only when Status==StatusToxic. Both are reset by clearStatus.
// Stages and Volatiles reset on switch-out (see clearVolatiles).
type Pokemon struct {
	DexNo   int         `json:"dex_no"`
	Name    string      `json:"name"`
	Type1   domain.Type `json:"type1"`
	Type2   domain.Type `json:"type2"`
	Ability AbilityKind `json:"ability,omitempty"`
	// Gender is "male", "female" or "genderless" (domain.Gender*). Public
	// information, unlike the spread: canon shows it on the battle UI, and
	// the whole point of gender is that both sides can plan around it.
	//
	// Fixed at team build: taken from the pick, or rolled once from the
	// battle seed against the species' birth ratio for a team that didn't
	// choose. Never changes mid-battle.
	Gender string   `json:"gender,omitempty"`
	Item   ItemKind `json:"item,omitempty"`
	// AbilityRevealed / ItemRevealed are the fog-of-war reveal set: whether an
	// in-battle event has already shown this Pokémon's ability or held item to
	// the opposing side. Canon does not announce either up front — a player
	// infers them, and only *knows* one the moment it visibly acts (Drought on
	// switch-in, Static on contact, a Berry being eaten, Knock Off taking a
	// slot). Projections toward the foe blank the field until its flag is set;
	// see ai.redactFoeActive.
	//
	// Once true, always true: knowledge does not un-happen, so these survive
	// switching out, fainting and Baton Pass. They are set at the moment the
	// engine announces the activation — every `Type: "ability"` and
	// `Type: "item"` log line is paired with a revealAbility / revealItem call,
	// and TestEveryAbilityAndItemAnnouncementReveals scans the source to keep
	// it that way. Item gain and loss reveal too (giveItem / loseItem), because
	// every route into those is itself announced.
	//
	// These are public by construction — they only ever record that something
	// already visible happened — so they are NOT redacted from either side.
	AbilityRevealed bool `json:"ability_revealed,omitempty"`
	ItemRevealed    bool `json:"item_revealed,omitempty"`
	// LastConsumedItem is the item this Pokémon most recently *used up* — ate,
	// or spent on a one-shot effect. Recycle restores it. Deliberately not set
	// when an item is taken away (Knock Off, Thief, Trick) or handed over: canon
	// only lets you recycle something you consumed yourself, and gaining any new
	// item clears the memory. Survives switching out, unlike Volatiles.
	LastConsumedItem ItemKind     `json:"last_consumed_item,omitempty"`
	MaxHP            int          `json:"max_hp"`
	HP               int          `json:"hp"`
	Stats            domain.Stats `json:"stats"`
	// EVs, IVs, and Nature are the resolved spread Stats was derived from —
	// carried so a persisted battle, a replay, and a team-preview UI can all
	// show *why* a Pokémon has the stats it has without re-deriving it from
	// the pick. Resolved values, never nil-able: a pick that omitted them
	// gets EV 0 / IV 31 / neutral here, not a blank.
	//
	// Hidden information. Together they *are* the stat spread, so anything
	// that projects a Pokémon toward the opposing side must redact all three
	// exactly as it redacts Stats — see ai.foeWire.
	EVs          domain.Stats `json:"evs"`
	IVs          domain.Stats `json:"ivs"`
	Nature       string       `json:"nature,omitempty"`
	Stages       Stages       `json:"stages"`
	Status       StatusCond   `json:"status"`
	SleepTurns   int          `json:"sleep_turns"`
	ToxicCounter int          `json:"toxic_counter"`
	Volatiles    Volatiles    `json:"volatiles"`
	Moves        []MoveSlot   `json:"moves"`
	Fainted      bool         `json:"fainted"`
}

// StruggleMoveIndex is the move index that means Struggle: the user has no
// usable move and flails instead. It is a sentinel, not a slot — every real
// move index is >= 0.
//
// Named because the bare -1 was mistakable, and got mistaken: an agent's
// no-legal-actions fallback returned index 0 with a comment calling it Struggle,
// which is an ordinary "use move slot 0" and illegal in a replace phase.
const StruggleMoveIndex = -1

// Side is one trainer's team and which member is currently active.
//
// Conditions carries the per-side field effects — Reflect, Light Screen,
// Aurora Veil. They are set by status moves, count down at end of turn,
// and damp the multiplier in computeDamage. See screens.go.
//
// SlotConditions are per-active-slot state that survives a switch (Wish,
// Healing Wish). They live on Side rather than Pokemon so a Wish cast by
// Slot 0's previous occupant fires onto the new occupant. See
// slotconditions.go.
type Side struct {
	Trainer        string         `json:"trainer"`
	Team           []Pokemon      `json:"team"`
	Active         int            `json:"active"`
	Conditions     SideConditions `json:"conditions"`
	SlotConditions SlotConditions `json:"slot_conditions"`
}

// BattleState is the complete, serializable state of a battle.
//
// Weather and Terrain are nil when no field condition is active; pointer-not-
// bool so a JSON round-trip distinguishes "never set" from "freshly cleared."
// Setter moves write them; end-of-turn ticks decrement TurnsLeft and clear at
// zero. Weather and terrain coexist independently — a Rain Dance + Electric
// Terrain field is normal Showdown behavior.
type BattleState struct {
	ID            string        `json:"id"`
	Sides         [2]Side       `json:"sides"`
	Turn          int           `json:"turn"`
	Phase         Phase         `json:"phase"`
	Winner        int           `json:"winner"` // -1 ongoing, 0 or 1 = side, 2 = draw
	Replace       [2]bool       `json:"replace"`
	Seed          uint64        `json:"seed"`
	RNGState      uint64        `json:"rng_state"`
	Weather       *WeatherState `json:"weather,omitempty"`
	Terrain       *TerrainState `json:"terrain,omitempty"`
	PseudoWeather PseudoWeather `json:"pseudo_weather"`
}

// ActionKind distinguishes the two things a side can do on a turn.
type ActionKind string

const (
	ActionMove   ActionKind = "move"
	ActionSwitch ActionKind = "switch"
)

// Action is a chosen move (Index = move slot, or -1 for Struggle) or a switch
// (Index = team member to bring in).
type Action struct {
	Kind  ActionKind `json:"kind"`
	Index int        `json:"index"`
	// SwitchTarget names the bench slot a self-switch move should bring in —
	// U-turn, Volt Switch, Flip Turn, Teleport, Baton Pass. Only meaningful
	// on an ActionMove whose move self-switches; ignored everywhere else.
	//
	// nil means "let the engine choose", which it does deterministically:
	// the lowest-indexed live teammate. That is the historical behavior and
	// keeps every existing replay and every controller that doesn't know
	// about this field byte-identical.
	//
	// A pointer rather than a sentinel int because both the Go zero value and
	// an omitted JSON field have to mean "unset", and slot 0 is a perfectly
	// good bench member. Use Action.Equal rather than == to compare Actions.
	SwitchTarget *int `json:"switch_target,omitempty"`
}

// Equal compares two actions by value, following SwitchTarget rather than
// comparing the pointers. Action is otherwise comparable, and callers used
// == before the pointer field existed — this is the replacement.
func (a Action) Equal(b Action) bool {
	if a.Kind != b.Kind || a.Index != b.Index {
		return false
	}
	switch {
	case a.SwitchTarget == nil && b.SwitchTarget == nil:
		return true
	case a.SwitchTarget == nil || b.SwitchTarget == nil:
		return false
	default:
		return *a.SwitchTarget == *b.SwitchTarget
	}
}

// LogLine is one human-readable entry in a turn log. Side is 0/1, or -1 for
// neutral lines (turn markers, the result).
type LogLine struct {
	Type string `json:"type"`
	Side int    `json:"side"`
	Text string `json:"text"`
}

// NewBattle builds a fresh battle from two team specs (lists of Pokédex
// numbers). The seed makes the whole battle deterministic.
func NewBattle(dex *domain.Dex, id, p1 string, t1 []int, p2 string, t2 []int, seed uint64) (*BattleState, error) {
	s1, err := buildSide(dex, p1, t1)
	if err != nil {
		return nil, fmt.Errorf("side 1: %w", err)
	}
	s2, err := buildSide(dex, p2, t2)
	if err != nil {
		return nil, fmt.Errorf("side 2: %w", err)
	}
	st := &BattleState{
		ID:       id,
		Sides:    [2]Side{s1, s2},
		Turn:     0,
		Phase:    PhaseChoosing,
		Winner:   -1,
		Seed:     seed,
		RNGState: seed,
	}
	rollGenders(dex, st, seed, nil)
	return st, nil
}

func buildSide(dex *domain.Dex, trainer string, team []int) (Side, error) {
	if len(team) < 1 || len(team) > 6 {
		return Side{}, errors.New("team must have 1 to 6 Pokémon")
	}
	s := Side{Trainer: trainer}
	for _, dn := range team {
		sp, ok := dex.Species[dn]
		if !ok {
			return Side{}, fmt.Errorf("unknown Pokédex number %d", dn)
		}
		s.Team = append(s.Team, buildPokemon(dex, sp))
	}
	return s, nil
}

// Spread is a resolved training investment: the EVs, IVs, and nature that
// turn a species' base stats into a battle Pokémon's derived stats. Every
// field is concrete — resolveSpread is what turns a pick's optional,
// possibly-absent fields into one of these.
type Spread struct {
	EVs    domain.Stats
	IVs    domain.Stats
	Nature domain.Nature
}

// DefaultSpread is the historical fixed spread every Pokémon had before
// spreads were pickable: IV 31 across the board, no EVs, neutral nature. It
// is what a pick with no spread fields resolves to, which is why adding
// spreads changed no existing battle's numbers.
//
// Note this is deliberately *not* the zero value of Spread — zero IVs are a
// legal-but-terrible spread, not "unspecified".
func DefaultSpread() Spread {
	return Spread{IVs: domain.Uniform(MaxIV)}
}

// resolveSpread turns a pick's optional spread fields into a concrete
// Spread. Absent EVs mean none; absent IVs mean perfect; an empty nature
// means neutral.
//
// An unknown nature slug resolves to neutral rather than erroring:
// ValidateTeam is the gate that rejects it, and this function trusts that
// gate the same way buildPokemonFromPick trusts it for moves and abilities.
func resolveSpread(dex *domain.Dex, p TeamPick) Spread {
	s := DefaultSpread()
	if p.EVs != nil {
		s.EVs = *p.EVs
	}
	if p.IVs != nil {
		s.IVs = *p.IVs
	}
	if p.Nature != "" {
		s.Nature = dex.Natures[p.Nature]
	}
	return s
}

// pokemonShell fills the species-derived fields on a fresh battle Pokémon
// — everything except Moves. Both buildPokemon (full learnset) and
// buildPokemonFromPick (chosen 1–4) layer their move list on top of this.
func pokemonShell(sp domain.Species, spread Spread) Pokemon {
	p := Pokemon{
		DexNo:   sp.DexNo,
		Name:    sp.Name,
		Type1:   sp.Type1,
		Type2:   sp.Type2,
		Ability: defaultAbility(sp),
		Gender:  sp.DefaultGender(),
		MaxHP:   calcHP(sp.Base.HP, spread.IVs.HP, spread.EVs.HP),
		EVs:     spread.EVs,
		IVs:     spread.IVs,
		Nature:  spread.Nature.ID,
	}
	p.HP = p.MaxHP
	stat := func(base, iv, ev int, key string) int {
		num, den := spread.Nature.Multiplier(key)
		return calcStat(base, iv, ev, num, den)
	}
	p.Stats = domain.Stats{
		HP:  p.MaxHP,
		Atk: stat(sp.Base.Atk, spread.IVs.Atk, spread.EVs.Atk, "atk"),
		Def: stat(sp.Base.Def, spread.IVs.Def, spread.EVs.Def, "def"),
		SpA: stat(sp.Base.SpA, spread.IVs.SpA, spread.EVs.SpA, "spatk"),
		SpD: stat(sp.Base.SpD, spread.IVs.SpD, spread.EVs.SpD, "spdef"),
		Spe: stat(sp.Base.Spe, spread.IVs.Spe, spread.EVs.Spe, "speed"),
	}
	return p
}

// buildPokemon inflates a Pokémon with its species' full learn list as
// moves. The legacy path used by NewBattle and quicksim — every move the
// species knows is available, the way the engine worked before the
// picker room existed.
func buildPokemon(dex *domain.Dex, sp domain.Species) Pokemon {
	p := pokemonShell(sp, DefaultSpread())
	for _, mid := range sp.Moves {
		if m, ok := dex.Moves[mid]; ok {
			p.Moves = append(p.Moves, MoveSlot{MoveID: mid, PP: m.PP, MaxPP: m.PP})
		}
	}
	return p
}

// buildPokemonFromPick inflates a Pokémon with exactly what the trainer
// chose. ValidateTeam is the gate that proves the moves, ability, item, and
// spread are legal for sp; this function trusts that and looks them up
// directly. Empty ability falls back to slot 0 (the pokemonShell default);
// empty item means the Pokémon holds nothing; absent spread fields resolve
// to the historical IV 31 / EV 0 / neutral default.
func buildPokemonFromPick(dex *domain.Dex, sp domain.Species, pick TeamPick) Pokemon {
	p := pokemonShell(sp, resolveSpread(dex, pick))
	if pick.Ability != "" {
		p.Ability = AbilityKind(pick.Ability)
	}
	if pick.Gender != "" {
		p.Gender = pick.Gender
	}
	if pick.Item != "" {
		p.Item = ItemKind(pick.Item)
	}
	for _, mid := range pick.MoveIDs {
		m := dex.Moves[mid]
		p.Moves = append(p.Moves, MoveSlot{MoveID: mid, PP: m.PP, MaxPP: m.PP})
	}
	return p
}

// NewBattleFromPicks builds a battle from two ValidateTeam-approved team
// submissions — the picker-room path. Each Pokémon carries only its
// chosen 1–4 moves, per docs/team-picker-room.md.
//
// Callers MUST have run ValidateTeam on both sides first; this
// constructor reports only sanity-net errors (unknown DexNo), not the
// user-facing rule violations ValidateTeam owns.
func NewBattleFromPicks(dex *domain.Dex, id, p1 string, picks1 []TeamPick,
	p2 string, picks2 []TeamPick, seed uint64,
) (*BattleState, error) {
	s1, err := buildSideFromPicks(dex, p1, picks1)
	if err != nil {
		return nil, fmt.Errorf("side 1: %w", err)
	}
	s2, err := buildSideFromPicks(dex, p2, picks2)
	if err != nil {
		return nil, fmt.Errorf("side 2: %w", err)
	}
	st := &BattleState{
		ID:       id,
		Sides:    [2]Side{s1, s2},
		Turn:     0,
		Phase:    PhaseChoosing,
		Winner:   -1,
		Seed:     seed,
		RNGState: seed,
	}
	rollGenders(dex, st, seed, &[2][]TeamPick{picks1, picks2})
	return st, nil
}

func buildSideFromPicks(dex *domain.Dex, trainer string, picks []TeamPick) (Side, error) {
	if len(picks) < 1 || len(picks) > TeamSize {
		return Side{}, fmt.Errorf("team must have 1 to %d Pokémon, got %d", TeamSize, len(picks))
	}
	s := Side{Trainer: trainer}
	for _, p := range picks {
		sp, ok := dex.Species[p.DexNo]
		if !ok {
			return Side{}, fmt.Errorf("unknown Pokédex number %d", p.DexNo)
		}
		s.Team = append(s.Team, buildPokemonFromPick(dex, sp, p))
	}
	return s, nil
}

// Active returns a pointer to the currently active Pokémon on a side.
func (s *BattleState) Active(side int) *Pokemon {
	sd := &s.Sides[side]
	return &sd.Team[sd.Active]
}

// Ended reports whether the battle is over.
func (s *BattleState) Ended() bool { return s.Phase == PhaseEnded }

// LiveCount returns how many Pokémon on a side have not fainted.
func (s *BattleState) LiveCount(side int) int {
	n := 0
	for i := range s.Sides[side].Team {
		if !s.Sides[side].Team[i].Fainted {
			n++
		}
	}
	return n
}

// Clone returns a deep copy, so callers (notably the AI search) can simulate
// turns without mutating the real state.
func (s *BattleState) Clone() *BattleState {
	c := *s
	for i := range c.Sides {
		team := make([]Pokemon, len(s.Sides[i].Team))
		copy(team, s.Sides[i].Team)
		for j := range team {
			mv := make([]MoveSlot, len(s.Sides[i].Team[j].Moves))
			copy(mv, s.Sides[i].Team[j].Moves)
			team[j].Moves = mv
			if c := team[j].Volatiles.Confusion; c != nil {
				cc := *c
				team[j].Volatiles.Confusion = &cc
			}
			if ch := team[j].Volatiles.Charging; ch != nil {
				cc := *ch
				team[j].Volatiles.Charging = &cc
			}
			if lm := team[j].Volatiles.LockedMove; lm != nil {
				ll := *lm
				team[j].Volatiles.LockedMove = &ll
			}
			if sub := team[j].Volatiles.Substitute; sub != nil {
				ss := *sub
				team[j].Volatiles.Substitute = &ss
			}
			if pt := team[j].Volatiles.PartialTrap; pt != nil {
				pp := *pt
				team[j].Volatiles.PartialTrap = &pp
			}
			if ls := team[j].Volatiles.LeechSeed; ls != nil {
				ll := *ls
				team[j].Volatiles.LeechSeed = &ll
			}
			if d := team[j].Volatiles.Disable; d != nil {
				dd := *d
				team[j].Volatiles.Disable = &dd
			}
			if e := team[j].Volatiles.Encore; e != nil {
				ee := *e
				team[j].Volatiles.Encore = &ee
			}
			if tt := team[j].Volatiles.Taunt; tt != nil {
				cc := *tt
				team[j].Volatiles.Taunt = &cc
			}
			if eb := team[j].Volatiles.Embargo; eb != nil {
				ee := *eb
				team[j].Volatiles.Embargo = &ee
			}
			if imp := team[j].Volatiles.Imprison; imp != nil {
				ii := *imp
				ids := make([]string, len(imp.MoveIDs))
				copy(ids, imp.MoveIDs)
				ii.MoveIDs = ids
				team[j].Volatiles.Imprison = &ii
			}
			if y := team[j].Volatiles.Yawn; y != nil {
				yy := *y
				team[j].Volatiles.Yawn = &yy
			}
			if mr := team[j].Volatiles.MagnetRise; mr != nil {
				mm := *mr
				team[j].Volatiles.MagnetRise = &mm
			}
			if tk := team[j].Volatiles.Telekinesis; tk != nil {
				tt := *tk
				team[j].Volatiles.Telekinesis = &tt
			}
			if sp := team[j].Volatiles.Stockpile; sp != nil {
				ss := *sp
				team[j].Volatiles.Stockpile = &ss
			}
		}
		c.Sides[i].Team = team
		c.Sides[i].Conditions = CloneSideConditions(s.Sides[i].Conditions)
		c.Sides[i].SlotConditions = CloneSlotConditions(s.Sides[i].SlotConditions)
	}
	if s.Weather != nil {
		w := *s.Weather
		c.Weather = &w
	}
	if s.Terrain != nil {
		t := *s.Terrain
		c.Terrain = &t
	}
	c.PseudoWeather = ClonePseudoWeather(s.PseudoWeather)
	return &c
}

// LegalActions returns every action a side may legally take right now.
// During PhaseChoosing that is its usable moves plus switches to live
// teammates; during PhaseReplace it is switches only.
func LegalActions(s *BattleState, side int) []Action {
	return LegalActionsDex(nil, s, side)
}

// LegalActionsDex is the dex-aware variant: callers that have the dex
// on hand pass it so Taunt's status-category filter can read each
// slot's category. LegalActions falls back to nil (Taunt still bans
// moves at executeMove time — Taunt-active controllers just see a
// status-move option listed and trip the resolve-time gate).
func LegalActionsDex(dex *domain.Dex, s *BattleState, side int) []Action {
	var out []Action
	sd := &s.Sides[side]

	if s.Phase == PhaseReplace {
		for i := range sd.Team {
			if !sd.Team[i].Fainted && i != sd.Active {
				out = append(out, Action{Kind: ActionSwitch, Index: i})
			}
		}
		return out
	}

	act := &sd.Team[sd.Active]

	// Two-turn charge: the user is locked into finishing the move it started
	// last turn. No switches, no other moves.
	if ch := act.Volatiles.Charging; ch != nil {
		return []Action{{Kind: ActionMove, Index: ch.MoveIdx}}
	}

	// Locked move (Outrage / Thrash / Petal Dance): the rampage forces the
	// same move every turn and bars switching until it ends in fatigue.
	if lm := act.Volatiles.LockedMove; lm != nil {
		return []Action{{Kind: ActionMove, Index: lm.MoveIdx}}
	}

	// PartialTrap (Bind, Wrap, Fire Spin, ...) prevents the user from
	// switching while the volatile is active. Ingrain roots the user
	// and blocks switches the same way. Moves are still legal.
	// Shed Shell is an unconditional escape hatch: it beats partial traps,
	// Ingrain, and the trapping abilities alike.
	trapped := !itemAllowsSwitchOut(act) &&
		(act.Volatiles.PartialTrap != nil || act.Volatiles.Trapped ||
			ingrainBlocksSwitch(act) || abilityTrapsSwitch(s, side))

	// Recharge: the user spends this turn recharging. The controller may
	// still switch (unless trapped); if it picks a move, the engine consumes
	// the turn as recharge regardless of which one. The index doesn't matter
	// — we surface the move that triggered the recharge if it's still in
	// slot 0..N, else a sentinel -1.
	if act.Volatiles.MustRecharge {
		out = append(out, Action{Kind: ActionMove, Index: -1})
		if !trapped {
			for i := range sd.Team {
				if !sd.Team[i].Fainted && i != sd.Active {
					out = append(out, Action{Kind: ActionSwitch, Index: i})
				}
			}
		}
		return out
	}

	// Choice lock (Choice Band): once locked, only the locked move is
	// offered. If it has run out of PP the loop below finds nothing and the
	// Struggle fallback kicks in — canonical for a choice-locked empty move.
	// Switching is never gated by the lock; it clears on switch-out.
	lockedMoveID := act.Volatiles.ChoiceLockMoveID

	for i := range act.Moves {
		if act.Moves[i].PP <= 0 {
			continue
		}
		if lockedMoveID != "" && act.Moves[i].MoveID != lockedMoveID {
			continue
		}
		// Disable / Encore / Torment / Imprison: filter restricted slots
		// out of the read path so the AI and picker UIs never offer them.
		// The executeMove gate is the authoritative refuser; this is a
		// usability filter that keeps illegal options off the menu.
		if lockRestrictBlocksSlot(s, side, i) {
			continue
		}
		// Assault Vest drops every status slot (same dex-aware lookup Taunt
		// needs — the category lives on the move, not the slot).
		if dex != nil && itemBlocksStatusMoves(act) &&
			dex.Moves[act.Moves[i].MoveID].Category == domain.CatStatus {
			continue
		}
		// Taunt drops status-category slots (dex-aware lookup).
		if dex != nil && statusBlockedByTaunt(dex, act, i) {
			continue
		}
		// A self-switch move is offered once per bench member it could bring
		// in, so choosing the pivot target is an ordinary part of picking an
		// action rather than a second concept a controller has to know about.
		// Needs the dex to know the move self-switches at all; on the
		// dex-less path the move is offered untargeted and the engine picks,
		// same as it always did.
		//
		// Not gated on `trapped`. A partial trap or Arena Trap stops the
		// holder *choosing* to leave, and canon lets U-turn pivot out of both
		// regardless — applySelfSwitch has never checked it either. Hiding the
		// targets here would take the aim away from exactly the Pokémon that
		// needs the pivot most.
		if dex != nil {
			if targets := selfSwitchTargets(dex, sd, act, i); len(targets) > 0 {
				for _, t := range targets {
					out = append(out, Action{Kind: ActionMove, Index: i, SwitchTarget: &t})
				}
				continue
			}
		}
		out = append(out, Action{Kind: ActionMove, Index: i})
	}
	if len(out) == 0 { // every move out of PP / locked out -> Struggle
		out = append(out, Action{Kind: ActionMove, Index: StruggleMoveIndex})
	}
	if !trapped {
		for i := range sd.Team {
			if !sd.Team[i].Fainted && i != sd.Active {
				out = append(out, Action{Kind: ActionSwitch, Index: i})
			}
		}
	}
	return out
}

// selfSwitchTargets lists the bench slots a self-switch move in slot moveIdx
// could bring in, or nil when the move doesn't self-switch (or there is
// nobody to bring in, which makes it an ordinary move for choice purposes).
//
// The switch-blockers are deliberately not consulted: a partial trap or Arena
// Trap stops the holder *choosing* to leave, and canon lets U-turn pivot out
// of both. applySelfSwitch doesn't check them either, so listing targets here
// unconditionally is what keeps the menu honest about what will happen.
func selfSwitchTargets(dex *domain.Dex, sd *Side, act *Pokemon, moveIdx int) []int {
	if dex == nil || moveIdx < 0 || moveIdx >= len(act.Moves) {
		return nil
	}
	if dex.Moves[act.Moves[moveIdx].MoveID].SelfSwitch == "" {
		return nil
	}
	var out []int
	for i := range sd.Team {
		if i != sd.Active && !sd.Team[i].Fainted {
			out = append(out, i)
		}
	}
	return out
}

// ActionAllowed reports whether act is legal for side right now. It is the
// predicate every gate should use, in preference to scanning LegalActions for
// an exact match.
//
// The difference is SwitchTarget. LegalActions enumerates a self-switch move
// once per bench member so the option is discoverable, but a controller is
// free to submit the move untargeted, or with a target, and neither should
// depend on the caller having reproduced the exact enumeration. So the base
// action is matched on Kind and Index, and the target — if one is named — is
// checked against the bench directly.
//
// dex may be nil; the dex-dependent filters (Taunt, Assault Vest, self-switch
// enumeration) are skipped in that case, exactly as in LegalActionsDex.
func ActionAllowed(dex *domain.Dex, s *BattleState, side int, act Action) bool {
	found := false
	for _, legal := range LegalActionsDex(dex, s, side) {
		if legal.Kind == act.Kind && legal.Index == act.Index {
			found = true
			break
		}
	}
	if !found {
		return false
	}
	if act.SwitchTarget == nil {
		return true
	}
	// A named target has to be a live teammate that isn't already out. Note
	// this is checked even for actions where the field is meaningless (a
	// switch, a non-pivot move): a controller naming a dead slot is confused
	// about something, and saying so beats silently ignoring it.
	sd := &s.Sides[side]
	i := *act.SwitchTarget
	return i >= 0 && i < len(sd.Team) && i != sd.Active && !sd.Team[i].Fainted
}

// genderRollSalt keeps the gender roll off the battle's own RNG stream. The
// roll happens once at construction, before any turn resolves, so drawing
// from RNGState would shift every subsequent roll in the battle and break
// every recorded replay. A separate stream derived from the same seed keeps
// the result deterministic without touching the one the turns use.
const genderRollSalt = 0x9E3779B97F4A7C15

// rollGenders fills in the gender of every Pokémon whose team didn't pick
// one. A species with a fixed gender already has it from pokemonShell and is
// left alone; anything else is rolled against its birth ratio, so a team
// built without thinking about gender comes out mixed rather than uniform.
//
// picks, when non-nil, is what each side actually asked for — a Pokémon whose
// pick named a gender keeps it, including when that gender happens to be the
// species' likelier one. Without the picks (the dex-number NewBattle path)
// nothing was chosen, so everything rollable is rolled.
//
// Deterministic from the battle seed: the same seed and the same teams
// produce the same genders, which is what lets a replay reproduce an Attract
// that landed.
func rollGenders(dex *domain.Dex, s *BattleState, seed uint64, picks *[2][]TeamPick) {
	rng := NewRNG(seed ^ genderRollSalt)
	for side := range s.Sides {
		team := s.Sides[side].Team
		for i := range team {
			if picks != nil && i < len(picks[side]) && picks[side][i].Gender != "" {
				continue // the team chose
			}
			sp, ok := dex.Species[team[i].DexNo]
			if !ok || len(sp.Genders) < 2 {
				continue // fixed gender (or genderless): nothing to roll
			}
			if rng.IntN(1000) < int(sp.MaleRatio*1000) {
				team[i].Gender = domain.GenderMale
			} else {
				team[i].Gender = domain.GenderFemale
			}
		}
	}
}
