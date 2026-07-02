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
type PartialTrapState struct {
	Turns    int    `json:"turns"`
	MoveName string `json:"move_name"`
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
	// Lock/restrict volatiles (see lockrestrict.go). All gate which
	// move the holder may pick this turn: Disable bans one slug for 4
	// turns, Encore forces one slug for 3 turns, Taunt blocks status
	// for 3 turns, Embargo blocks items for 5 turns (informational —
	// items aren't modeled), Torment blocks the same move twice in a
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
	// MovedLast: this Pokémon is the last scheduled mover this turn. Set in
	// the move-resolution loop before executeMove runs for the last entry of
	// the ordered slice; read by Analytic; cleared in the end-of-turn sweep.
	MovedLast bool `json:"moved_last,omitempty"`
	// DamagedThisTurn: the holder took direct move damage earlier this turn.
	// Set in dealDamage when HP is lost; drives Revenge / Avalanche (×2 BP)
	// and Focus Punch (loses focus and fails). Cleared in the end-of-turn
	// sweep so it only ever reflects the turn in progress.
	DamagedThisTurn bool `json:"damaged_this_turn,omitempty"`
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
	DexNo        int          `json:"dex_no"`
	Name         string       `json:"name"`
	Type1        domain.Type  `json:"type1"`
	Type2        domain.Type  `json:"type2"`
	Ability      AbilityKind  `json:"ability,omitempty"`
	Item         ItemKind     `json:"item,omitempty"`
	MaxHP        int          `json:"max_hp"`
	HP           int          `json:"hp"`
	Stats        domain.Stats `json:"stats"`
	Stages       Stages       `json:"stages"`
	Status       StatusCond   `json:"status"`
	SleepTurns   int          `json:"sleep_turns"`
	ToxicCounter int          `json:"toxic_counter"`
	Volatiles    Volatiles    `json:"volatiles"`
	Moves        []MoveSlot   `json:"moves"`
	Fainted      bool         `json:"fainted"`
}

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
	return &BattleState{
		ID:       id,
		Sides:    [2]Side{s1, s2},
		Turn:     0,
		Phase:    PhaseChoosing,
		Winner:   -1,
		Seed:     seed,
		RNGState: seed,
	}, nil
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

// pokemonShell fills the species-derived fields on a fresh battle Pokémon
// — everything except Moves. Both buildPokemon (full learnset) and
// buildPokemonFromPick (chosen 1–4) layer their move list on top of this.
func pokemonShell(sp domain.Species) Pokemon {
	p := Pokemon{
		DexNo:   sp.DexNo,
		Name:    sp.Name,
		Type1:   sp.Type1,
		Type2:   sp.Type2,
		Ability: defaultAbility(sp),
		MaxHP:   calcHP(sp.Base.HP),
	}
	p.HP = p.MaxHP
	p.Stats = domain.Stats{
		HP:  p.MaxHP,
		Atk: calcStat(sp.Base.Atk),
		Def: calcStat(sp.Base.Def),
		SpA: calcStat(sp.Base.SpA),
		SpD: calcStat(sp.Base.SpD),
		Spe: calcStat(sp.Base.Spe),
	}
	return p
}

// buildPokemon inflates a Pokémon with its species' full learn list as
// moves. The legacy path used by NewBattle and quicksim — every move the
// species knows is available, the way the engine worked before the
// picker room existed.
func buildPokemon(dex *domain.Dex, sp domain.Species) Pokemon {
	p := pokemonShell(sp)
	for _, mid := range sp.Moves {
		if m, ok := dex.Moves[mid]; ok {
			p.Moves = append(p.Moves, MoveSlot{MoveID: mid, PP: m.PP, MaxPP: m.PP})
		}
	}
	return p
}

// buildPokemonFromPick inflates a Pokémon with exactly the moves the
// trainer chose. ValidateTeam is the gate that proves moveIDs, ability, and
// item are legal for sp; this function trusts that and looks them up directly.
// Empty ability falls back to slot 0 (the pokemonShell default); empty item
// means the Pokémon holds nothing.
func buildPokemonFromPick(dex *domain.Dex, sp domain.Species, moveIDs []string, ability, item string) Pokemon {
	p := pokemonShell(sp)
	if ability != "" {
		p.Ability = AbilityKind(ability)
	}
	if item != "" {
		p.Item = ItemKind(item)
	}
	for _, mid := range moveIDs {
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
	return &BattleState{
		ID:       id,
		Sides:    [2]Side{s1, s2},
		Turn:     0,
		Phase:    PhaseChoosing,
		Winner:   -1,
		Seed:     seed,
		RNGState: seed,
	}, nil
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
		s.Team = append(s.Team, buildPokemonFromPick(dex, sp, p.MoveIDs, p.Ability, p.Item))
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
	trapped := act.Volatiles.PartialTrap != nil || ingrainBlocksSwitch(act) || abilityTrapsSwitch(s, side)

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
		// Taunt drops status-category slots (dex-aware lookup).
		if dex != nil && statusBlockedByTaunt(dex, act, i) {
			continue
		}
		out = append(out, Action{Kind: ActionMove, Index: i})
	}
	if len(out) == 0 { // every move out of PP / locked out -> Struggle
		out = append(out, Action{Kind: ActionMove, Index: -1})
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
