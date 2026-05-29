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

// Volatiles is the bag of volatile conditions on a Pokémon. Stateful volatiles
// are pointer-or-nil (nil = absent); transient ones are bool. All clear on
// switch-out via clearVolatiles.
type Volatiles struct {
	Confusion    *ConfusionState `json:"confusion,omitempty"`
	Flinch       bool            `json:"flinch,omitempty"`
	Charging     *ChargingState  `json:"charging,omitempty"`
	MustRecharge bool            `json:"must_recharge,omitempty"`
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
type Side struct {
	Trainer string    `json:"trainer"`
	Team    []Pokemon `json:"team"`
	Active  int       `json:"active"`
}

// BattleState is the complete, serializable state of a battle.
type BattleState struct {
	ID       string  `json:"id"`
	Sides    [2]Side `json:"sides"`
	Turn     int     `json:"turn"`
	Phase    Phase   `json:"phase"`
	Winner   int     `json:"winner"` // -1 ongoing, 0 or 1 = side, 2 = draw
	Replace  [2]bool `json:"replace"`
	Seed     uint64  `json:"seed"`
	RNGState uint64  `json:"rng_state"`
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
		DexNo: sp.DexNo,
		Name:  sp.Name,
		Type1: sp.Type1,
		Type2: sp.Type2,
		MaxHP: calcHP(sp.Base.HP),
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
// trainer chose. ValidateTeam is the gate that proves moveIDs are legal
// for sp; this function trusts that and looks them up directly.
func buildPokemonFromPick(dex *domain.Dex, sp domain.Species, moveIDs []string) Pokemon {
	p := pokemonShell(sp)
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
	p2 string, picks2 []TeamPick, seed uint64) (*BattleState, error) {
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
		s.Team = append(s.Team, buildPokemonFromPick(dex, sp, p.MoveIDs))
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
		}
		c.Sides[i].Team = team
	}
	return &c
}

// LegalActions returns every action a side may legally take right now.
// During PhaseChoosing that is its usable moves plus switches to live
// teammates; during PhaseReplace it is switches only.
func LegalActions(s *BattleState, side int) []Action {
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

	// Recharge: the user spends this turn recharging. The controller may
	// still switch; if it picks a move, the engine consumes the turn as
	// recharge regardless of which one. The index doesn't matter — we
	// surface the move that triggered the recharge if it's still in slot
	// 0..N, else a sentinel -1.
	if act.Volatiles.MustRecharge {
		out = append(out, Action{Kind: ActionMove, Index: -1})
		for i := range sd.Team {
			if !sd.Team[i].Fainted && i != sd.Active {
				out = append(out, Action{Kind: ActionSwitch, Index: i})
			}
		}
		return out
	}

	for i := range act.Moves {
		if act.Moves[i].PP > 0 {
			out = append(out, Action{Kind: ActionMove, Index: i})
		}
	}
	if len(out) == 0 { // every move out of PP -> Struggle
		out = append(out, Action{Kind: ActionMove, Index: -1})
	}
	for i := range sd.Team {
		if !sd.Team[i].Fainted && i != sd.Active {
			out = append(out, Action{Kind: ActionSwitch, Index: i})
		}
	}
	return out
}
