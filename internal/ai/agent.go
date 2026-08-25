// Package ai is the agent harness — a switchable strategy interface plus a
// timeout-and-fallback runtime. Every agent decides from a View, which is the
// strict fog-of-war projection a side legitimately sees: its own team in full
// and the opponent's active Pokémon. There is no agent that reads more than a
// human would: fairness is by construction, because the hidden data simply is
// not in the View.
package ai

import (
	"context"
	"encoding/json"

	"pokearena/internal/domain"
	"pokearena/internal/engine"
)

// View is everything one side may legitimately see — exactly what the human
// player's UI renders. Notably it does NOT contain the opponent's bench
// movesets or stats, so an agent cannot plan around unrevealed Pokémon.
//
// JSON tags are part of the wire protocol now: the pvp WS handler and the
// future MCP server both serialize View to clients. Lowercase, snake_case
// matches the rest of the engine types.
type View struct {
	Me            int                   `json:"me"`              // side index this agent controls
	Self          engine.Side           `json:"self"`            // own team, in full
	Foe           engine.Pokemon        `json:"foe"`             // opponent's active Pokémon
	FoeBenchAlive int                   `json:"foe_bench_alive"` // unfainted Pokémon the opponent has benched
	Phase         engine.Phase          `json:"phase"`
	Turn          int                   `json:"turn"`
	Replace       bool                  `json:"replace"` // true when this side must replace a fainted active
	Weather       *engine.WeatherState  `json:"weather,omitempty"`
	Terrain       *engine.TerrainState  `json:"terrain,omitempty"`
	PseudoWeather engine.PseudoWeather  `json:"pseudo_weather"` // field-wide rooms/Gravity — announced loudly, public info
	FoeConditions engine.SideConditions `json:"foe_conditions"` // foe's per-side conditions (screens) — public info
	// FoeSlotConditions is the fog projection of the foe's pending slot
	// effects (Wish, Healing Wish). The events are public — both moves
	// are used in plain sight — but engine.WishState carries Amount,
	// which is the caster's MaxHP/2 and would leak hidden HP investment
	// through the back door. This projection keeps who and when, not
	// how much.
	FoeSlotConditions FoeSlotConditions `json:"foe_slot_conditions"`
}

// FoeSlotConditions mirrors engine.SlotConditions minus the heal figure.
type FoeSlotConditions struct {
	Wish        *FoeWishState `json:"wish,omitempty"`
	HealingWish bool          `json:"healing_wish,omitempty"`
}

// FoeWishState is the public face of a pending foe Wish: the caster
// (announced when the move was used) and the landing countdown (players
// count it themselves) — never the snapshotted heal amount.
type FoeWishState struct {
	Healer    string `json:"healer"`
	TurnsLeft int    `json:"turns_left"`
}

// redactFoeSlotConditions projects the foe's slot-condition bag for the
// View, dropping WishState.Amount per the FoeSlotConditions contract.
func redactFoeSlotConditions(sc engine.SlotConditions) FoeSlotConditions {
	out := FoeSlotConditions{HealingWish: sc.HealingWish}
	if sc.Wish != nil {
		out.Wish = &FoeWishState{Healer: sc.Wish.Healer, TurnsLeft: sc.Wish.TurnsLeft}
	}
	return out
}

// MakeView projects the fog-of-war view for one side of a battle, per
// docs/team-picker-room.md §6. Self is unredacted; the foe's active
// Pokémon is passed through redactFoeActive (unused moves blanked, HP
// bucketed to nearest 1% of max, internal status counters zeroed).
// Bench species are hidden by construction — only the active foe is in
// the view, plus a count of unfainted bench members.
func MakeView(s *engine.BattleState, side int) View {
	opp := 1 - side
	bench := 0
	for i := range s.Sides[opp].Team {
		if i != s.Sides[opp].Active && !s.Sides[opp].Team[i].Fainted {
			bench++
		}
	}
	var w *engine.WeatherState
	if s.Weather != nil {
		ww := *s.Weather
		w = &ww
	}
	var tr *engine.TerrainState
	if s.Terrain != nil {
		tt := *s.Terrain
		tr = &tt
	}
	return View{
		Me:                side,
		Self:              cloneSide(s.Sides[side]),
		Foe:               redactFoeActive(s.Sides[opp].Team[s.Sides[opp].Active]),
		FoeBenchAlive:     bench,
		Phase:             s.Phase,
		Turn:              s.Turn,
		Replace:           s.Replace[side],
		Weather:           w,
		Terrain:           tr,
		PseudoWeather:     engine.ClonePseudoWeather(s.PseudoWeather),
		FoeConditions:     engine.CloneSideConditions(s.Sides[opp].Conditions),
		FoeSlotConditions: redactFoeSlotConditions(s.Sides[opp].SlotConditions),
	}
}

// redactFoeActive applies the fog-of-war filter to the opponent's
// active Pokémon. Move slots count is preserved (so the viewer can
// see "the foe has 4 moves, I've seen 1"); but unused slots — those
// whose PP still equals MaxPP — are blanked.
//
// HP is floored to a 5%-of-MaxHP bucket for in-process agents (see
// bucketHP); on the wire it is further reduced to a percentage with no
// absolute count at all (see foeWire / View.MarshalJSON), so external
// clients can't read the foe's exact HP or max HP. A non-fainted
// Pokémon never reaches zero either way, so the faint signal stays
// load-bearing. Engine-internal counters (sleep turns, toxic counter,
// confusion turns) are zeroed — the *status* itself is visible, the
// *clock* is not.
//
// Ability and Item are fog-of-war: blanked until an in-battle event has
// actually revealed them (engine.Pokemon.AbilityRevealed / ItemRevealed,
// set wherever the engine announces the ability or item doing something).
// Canon does not announce either up front — a player infers them and only
// knows one the moment it visibly acts — and printing them from turn 0
// was a large amount of free information: a tournament finalist read a
// Heat Rock off turn 0 and re-planned its game around eight turns of sun
// before anything had revealed it.
//
// Exact stats stay on the struct — the in-process agents' damage model
// needs them — but never reach the wire (foeWire drops them, along with
// ability and item unconditionally, Showdown-style). That is a deliberate
// asymmetry: reference bots see a little more than external MCP agents do.
// The wire stays strictly-hidden rather than reveal-gated; narrowing the
// in-process view does not widen the wire's.
func redactFoeActive(p engine.Pokemon) engine.Pokemon {
	c := clonePokemon(p)
	for i := range c.Moves {
		if c.Moves[i].PP == c.Moves[i].MaxPP {
			c.Moves[i] = engine.MoveSlot{}
		}
	}
	c.HP = bucketHP(c.HP, c.MaxHP)
	// Hidden until an event revealed it. LastConsumedItem needs no separate
	// guard: the only writer is engine.consumeItem, which routes through
	// loseItem and therefore always sets ItemRevealed first.
	if !c.AbilityRevealed {
		c.Ability = engine.AbilityNone
	}
	if !c.ItemRevealed {
		c.Item = engine.ItemNone
		// The Choice lock names the item on its own — a live lock means a
		// Choice band/scarf/specs and nothing else — so hiding the slug while
		// shipping the lock would not hide anything. Canon does not announce
		// the lock either; a player infers it from the foe repeating a move.
		c.Volatiles.ChoiceLockMoveID = ""
	}
	c.SleepTurns = 0
	c.ToxicCounter = 0
	if c.Volatiles.Confusion != nil {
		c.Volatiles.Confusion = &engine.ConfusionState{} // presence visible, turn count hidden
	}
	return c
}

// bucketHP floors hp to a 5%-of-MaxHP bucket. 5% matches Showdown's
// HP-bar granularity — enough for human strategy, not enough to be a
// damage calculator. Flooring (rather than rounding to nearest) keeps
// the invariant that the bucketed HP is never greater than the true
// HP: a foe can never look healthier than it is. That matters for the
// low end — a foe clinging to 1 HP (e.g. a Sturdy survivor) must read
// as 1, not get rounded up to a full bucket. A live Pokémon (hp>0)
// floors to at least 1, never 0, so the faint distinction stays
// load-bearing.
//
// Bucket width is `MaxHP/20` clamped to ≥1; at our HP ranges
// (≈150–350 MaxHP) that gives ~7–17 HP buckets, which the test
// TestMakeView_RedactsFoeFog locks in.
func bucketHP(hp, maxHP int) int {
	if maxHP <= 0 || hp <= 0 {
		return hp
	}
	if hp >= maxHP {
		return maxHP // a full-HP foe reads as full, not one bucket short
	}
	bucket := maxHP / 20
	if bucket < 1 {
		bucket = 1
	}
	r := (hp / bucket) * bucket
	if r == 0 {
		r = 1 // a live Pokémon never floors to zero — the faint signal stays load-bearing
	}
	return r
}

// foeWire is the wire projection of the opponent's active Pokémon,
// matching what Pokémon Showdown sends a player about the foe. It
// embeds the (already fog-redacted) Pokemon but shadows away with nil
// pointers everything Showdown never sends proactively:
//
//   - hp/max_hp → replaced by hp_pct, a 0–100 percentage (Showdown's
//     HP Percentage Mod). A client never reads the foe's exact HP or
//     max HP (which would leak the foe's HP investment).
//   - ability → never sent. In the games an ability is inferred, and
//     confirmed only the moment it visibly activates.
//   - item → never sent, for the same reason as ability: the held item is
//     hidden information a player infers from what activates. Sending it
//     would hand the viewer a free read on the foe's whole set (a Choice
//     lock, a Focus Sash save, a Leftovers tick). The choice_lock_move_id
//     volatile is cleared for the same reason — its mere presence names
//     the item — see marshalFoe.
//   - stats → never sent. The exact spread is a free damage calculator
//     (exact Speed alone decides move order).
//   - evs/ivs/nature → never sent, for exactly the same reason. These are
//     the *inputs* stats are derived from, so shipping them would undo the
//     stats redaction one step upstream — a foe's EVs and nature reconstruct
//     its Speed and both attacking stats outright.
//   - moves → revealed slots keep their move_id but lose pp/max_pp;
//     Showdown clients count usage instead of being told.
//
// Status, boosts (stages), and volatiles stay: those are announced
// publicly in Showdown and rendered on its UI.
type foeWire struct {
	engine.Pokemon
	HP      *int    `json:"hp,omitempty"`      // shadows Pokemon.HP → nil → omitted
	MaxHP   *int    `json:"max_hp,omitempty"`  // shadows Pokemon.MaxHP → nil → omitted
	Ability *string `json:"ability,omitempty"` // shadows Pokemon.Ability → nil → omitted
	Item    *string `json:"item,omitempty"`    // shadows Pokemon.Item → nil → omitted
	// LastConsumedItem names an item the foe used to hold, which is the same
	// hidden information as the slot itself — "ate a Sitrus at 50%" tells you
	// the set. Shadowed to nil like the rest.
	LastConsumedItem *string       `json:"last_consumed_item,omitempty"`
	Stats            *domain.Stats `json:"stats,omitempty"`  // shadows Pokemon.Stats → nil → omitted
	EVs              *domain.Stats `json:"evs,omitempty"`    // shadows Pokemon.EVs → nil → omitted
	IVs              *domain.Stats `json:"ivs,omitempty"`    // shadows Pokemon.IVs → nil → omitted
	Nature           *string       `json:"nature,omitempty"` // shadows Pokemon.Nature → nil → omitted
	HPPct            int           `json:"hp_pct"`
	Moves            []foeMoveWire `json:"moves"` // shadows Pokemon.Moves — move_id only, no PP
}

// marshalFoe prepares the foe Pokémon for the wire. The foeWire shadows drop
// the top-level hidden fields, but ChoiceLockMoveID lives inside the
// value-typed Volatiles struct, which embedding can't shadow field-by-field —
// so it is cleared on a copy here. It is hidden information for the same
// reason the item slug is: a non-empty lock names a Choice item on turn one.
// Everything else in Volatiles is publicly announced in Showdown and stays.
func marshalFoe(p engine.Pokemon) engine.Pokemon {
	p.Volatiles.ChoiceLockMoveID = ""
	// Same reasoning: a non-empty metronome streak names Metronome, and the
	// count hands over the holder's current damage multiplier. Any volatile
	// that only exists because of a held item belongs in this list —
	// TestView_FoeVolatilesNameNoItem enumerates them so a new one can't be
	// added without a decision.
	p.Volatiles.MetronomeMoveID = ""
	p.Volatiles.MetronomeCount = 0
	// Micle's prime survives into the following turn by design, so it is live
	// exactly when a View gets built — and only a Micle Berry can set it.
	p.Volatiles.MicleTurns = 0
	// Unburden names the ability, which this projection hides for the same
	// reason it hides the item.
	p.Volatiles.Unburden = false
	// An armed Eject Pack names the item outright: nothing else in the engine
	// sets this flag. It is short-lived — the pack is drained at the end of the
	// move that armed it — but "usually already spent" is not a reason to put
	// hidden information on the wire.
	p.Volatiles.EjectPackArmed = false
	// The gained-boosts record is public in the sense that every boost in it
	// was announced — but it is *kept* only so a Mirror Herb can read it, and
	// it is drained the moment the herb spends. Nothing on the wire needs it,
	// so it is cleared rather than reasoned about.
	p.Volatiles.GainedBoosts = nil
	return p
}

// foeMoveWire is a foe move slot on the wire: the move's identity once
// revealed, nothing else. Slot count is preserved so a client can show
// "revealed 1 of 4"; unrevealed slots carry an empty move_id.
type foeMoveWire struct {
	MoveID string `json:"move_id"`
}

func foeMovesWire(ms []engine.MoveSlot) []foeMoveWire {
	out := make([]foeMoveWire, len(ms))
	for i, m := range ms {
		out[i] = foeMoveWire{MoveID: m.MoveID}
	}
	return out
}

// MarshalJSON renders the View for the wire (MCP tools, PvP WebSocket,
// web client). Self serializes verbatim; the foe goes through foeWire,
// which drops everything Showdown wouldn't send (exact HP, ability,
// stats, move PP). In-process agents read the View struct directly and
// still see the redacted-but-absolute values they need for damage
// math; only the serialized form changes.
func (v View) MarshalJSON() ([]byte, error) {
	type alias View // strip View's MarshalJSON to avoid infinite recursion
	return json.Marshal(struct {
		alias
		Foe foeWire `json:"foe"` // shadows alias.Foe (deeper) for JSON
	}{
		alias: alias(v),
		Foe: foeWire{
			Pokemon: marshalFoe(v.Foe),
			HPPct:   FoePercentHP(v.Foe.HP, v.Foe.MaxHP),
			Moves:   foeMovesWire(v.Foe.Moves),
		},
	})
}

// UnmarshalJSON is the inverse of MarshalJSON. The wire never carries the foe's
// absolute hp/max_hp (fog of war drops them for hp_pct), so a plain decode would
// leave Foe.HP at 0 — a healthy foe reading as fainted, the fog-of-war relay bug
// this type kept re-introducing. Recover the public HP the only way it survives
// the wire: as a percentage out of a normalized 100, so any consumer that reads
// pctHP(Foe.HP, Foe.MaxHP) gets the real number without having to know the View
// was decoded off the wire rather than built in-process.
func (v *View) UnmarshalJSON(data []byte) error {
	type alias View // strip UnmarshalJSON to avoid infinite recursion
	aux := struct {
		*alias
		Foe struct {
			engine.Pokemon
			HPPct int `json:"hp_pct"`
		} `json:"foe"` // shadows alias.Foe (deeper) to capture hp_pct
	}{alias: (*alias)(v)}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	v.Foe = aux.Foe.Pokemon
	v.Foe.HP, v.Foe.MaxHP = aux.Foe.HPPct, 100
	return nil
}

// FoePercentHP converts an absolute HP/max into a 0–100 percentage for the
// foe's public view. It floors — a foe never looks healthier than it is — but
// clamps a live Pokémon to ≥1% so the faint signal stays load-bearing; only a
// full-HP foe reads 100%. It is the single source of the fog-bucketed foe HP%:
// the wire encoder (MarshalJSON) and the prompt renderer both call it, so the
// two can't drift.
func FoePercentHP(hp, maxHP int) int {
	if maxHP <= 0 || hp <= 0 {
		return 0
	}
	pct := hp * 100 / maxHP
	if pct < 1 {
		pct = 1
	}
	if pct > 100 {
		pct = 100
	}
	return pct
}

// Agent is a battle-decision strategy. Implementations must respect the
// context deadline; the Harness enforces it as a backstop regardless.
type Agent interface {
	Name() string
	Decide(ctx context.Context, view View) (engine.Action, error)
}

// LegalActions returns every action legal from a View. Implementation is
// a thin shim over engine.LegalActions on a battle reconstructed from the
// View — there is exactly one rule set for "what's legal," and it lives in
// the engine. Previous parallel implementations of this function drifted
// silently as the engine's gating grew (PartialTrap, Charging, MustRecharge,
// lockRestrict, Taunt) and the AI proposed actions the gateway refused.
func LegalActions(v View) []engine.Action {
	return engine.LegalActions(reconstructFromView(v), v.Me)
}

// LegalActionsDex is the dex-aware variant, for callers that have one. It
// adds the filters that need to read a move (Taunt's status ban, Assault
// Vest) and enumerates a self-switch move once per bench member it could
// pivot into, so a controller can aim U-turn / Volt Switch / Baton Pass
// instead of taking whoever the engine would have picked.
func LegalActionsDex(dex *domain.Dex, v View) []engine.Action {
	return engine.LegalActionsDex(dex, reconstructFromView(v), v.Me)
}

// reconstructFromView builds the minimal BattleState the engine needs to
// rule on legality from a View. The foe side carries only the visible
// active Pokémon (faithful to the View's fog-of-war contract) — this is
// enough for every gate engine.LegalActions cares about. The same shape
// works for the deeper simulation in expectimax; that path overrides Phase
// and RNGState before resolving turns.
func reconstructFromView(v View) *engine.BattleState {
	s := &engine.BattleState{
		Phase:         v.Phase,
		Winner:        -1,
		Turn:          v.Turn,
		Weather:       cloneWeatherState(v.Weather),
		Terrain:       cloneTerrainState(v.Terrain),
		PseudoWeather: engine.ClonePseudoWeather(v.PseudoWeather),
	}
	s.Replace[v.Me] = v.Replace
	s.Sides[v.Me] = cloneSide(v.Self)
	s.Sides[1-v.Me] = engine.Side{
		Trainer:        "Foe",
		Team:           []engine.Pokemon{clonePokemon(v.Foe)},
		Active:         0,
		Conditions:     engine.CloneSideConditions(v.FoeConditions),
		SlotConditions: reconstructFoeSlotConditions(v),
	}
	return s
}

// reconstructFoeSlotConditions rebuilds an engine slot-condition bag from
// the redacted foe projection so sims see the pending effect. The hidden
// Wish amount is estimated as half the visible active's max HP — exact
// when the caster is still out, a reasonable stand-in when it isn't.
func reconstructFoeSlotConditions(v View) engine.SlotConditions {
	sc := engine.SlotConditions{HealingWish: v.FoeSlotConditions.HealingWish}
	if w := v.FoeSlotConditions.Wish; w != nil {
		sc.Wish = &engine.WishState{
			Healer:    w.Healer,
			Amount:    v.Foe.MaxHP / 2,
			TurnsLeft: w.TurnsLeft,
		}
	}
	return sc
}

func cloneWeatherState(w *engine.WeatherState) *engine.WeatherState {
	if w == nil {
		return nil
	}
	c := *w
	return &c
}

func cloneTerrainState(t *engine.TerrainState) *engine.TerrainState {
	if t == nil {
		return nil
	}
	c := *t
	return &c
}

func isLegal(v View, a engine.Action) bool {
	return engine.ActionAllowed(nil, reconstructFromView(v), v.Me, a)
}

func clonePokemon(p engine.Pokemon) engine.Pokemon {
	c := p
	c.Moves = make([]engine.MoveSlot, len(p.Moves))
	copy(c.Moves, p.Moves)
	return c
}

func cloneSide(sd engine.Side) engine.Side {
	c := sd
	c.Team = make([]engine.Pokemon, len(sd.Team))
	for i := range sd.Team {
		c.Team[i] = clonePokemon(sd.Team[i])
	}
	c.Conditions = engine.CloneSideConditions(sd.Conditions)
	// Deep-copy the slot bag too: the shallow struct copy above aliases
	// WishState through its pointer, and a sim ticking the timer on a
	// reconstructed state would mutate the real battle through it.
	c.SlotConditions = engine.CloneSlotConditions(sd.SlotConditions)
	return c
}
