package engine

import "pokearena/internal/domain"

// roost.go lifts Roost's defining side effect: while the user is roosting it
// loses its Flying type for the rest of the turn. The 50% self-heal already
// rides the move's declarative Primary block; only the type loss lives in a JS
// callback, so it's gated by move ID in applyStatusMove and read here on the
// incoming-damage path. The Roost volatile is a one-shot bool cleared in the
// end-of-turn sweep (see turn.go).
//
// Scope note: this models the type loss for damage effectiveness only — the
// most-observable effect (a grounded Charizard can be hit by Earthquake). It
// does not re-ground the roosting Pokémon for terrain or other Flying-keyed
// checks; consistent with the engine's other documented degradations.

// roostTypes returns the defender's effective types for incoming-damage
// effectiveness. With Roost active the Flying type is suppressed; a pure-Flying
// user becomes Normal (no such species ships in the Gen-1 dex, but we model it
// for correctness so it isn't wrongly treated as typeless/neutral).
func roostTypes(def *Pokemon) (domain.Type, domain.Type) {
	t1, t2 := def.Type1, def.Type2
	if !def.Volatiles.Roost {
		return t1, t2
	}
	if t1 == "flying" {
		t1 = ""
	}
	if t2 == "flying" {
		t2 = ""
	}
	if t1 == "" && t2 == "" {
		return "normal", ""
	}
	return t1, t2
}
