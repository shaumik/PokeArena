package engine

// willcrit.go lifts the "always lands a critical hit" mechanic of Frost Breath
// and Storm Throw. Showdown marks these with a `willCrit` callback the
// declarative dataset can't carry, so they're gated by move ID — the same
// approach as the other JS-callback mechanics (see weatherheal.go). The crit
// override lives in computeDamage; this is just the move set. Battle Armor /
// Shell Armor still veto the crit because the ability block runs after the
// override.

var alwaysCritMoveIDs = map[string]bool{
	"frost-breath": true,
	"storm-throw":  true,
}

func isAlwaysCrit(id string) bool { return alwaysCritMoveIDs[id] }
