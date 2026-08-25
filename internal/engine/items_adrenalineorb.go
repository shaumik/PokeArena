package engine

// items_adrenalineorb.go is one item whose whole difficulty is a distinction
// canon draws in its boost *object* rather than in its behavior.
//
// Adrenaline Orb gives its holder +1 Speed when an Intimidate is aimed at it.
// Upstream's handler carries the only comment in data/items.ts that explains
// itself, and it is worth repeating because the rule is not guessable:
//
//	// Adrenaline Orb activates if Intimidate is blocked by an ability like
//	// Hyper Cutter, which deletes boost.atk, but not if the holder's attack is
//	// already at -6 (or +6 if it has Contrary), which sets boost.atk to 0
//
// So "the Attack drop did not happen" is two different outcomes. A guard —
// Mist, Clear Body, Hyper Cutter, Own Tempo, Clear Amulet — *deletes* the
// entry, and the orb fires: something tried to intimidate the holder and the
// holder shrugged it off, which is exactly the moment adrenaline is supposed to
// kick in. The ±6 floor instead leaves the entry present and zero, and the orb
// stays put: nothing was refused, there was simply nothing left to take.
//
// The engine has no boost object, so the distinction is carried explicitly:
// applyStagesFromFoe reports whether a guard refused the drop, and the Attack
// stage is sampled before the attempt to recognize the floor case.
//
// A Substitute is neither: canon's Intimidate never reaches the boost at all
// against a doll (`if (target.volatiles['substitute']) { add('-immune') } else
// { boost(...) }`), so onAfterBoost does not run and the orb does not fire.
// That falls out here too, because the substitute check returns before this is
// reached.

const ItemAdrenalineOrb ItemKind = "adrenaline-orb"

func init() {
	registerItem(&Item{
		Kind: ItemAdrenalineOrb,
		Name: "Adrenaline Orb",
		Desc: "Raises the holder's Speed by 1 stage when it is targeted by Intimidate.",
	})
}

// fireAdrenalineOrb is called from Intimidate's hook once the Attack drop has
// been attempted. atkBefore is the holder's Attack stage before the attempt and
// refused says whether a guard deleted the drop rather than the floor zeroing
// it; between them they reproduce canon's two-way read of boost.atk.
func fireAdrenalineOrb(p *Pokemon, side int, atkBefore int, refused bool, log *[]LogLine) {
	it := itemOf(p)
	if it == nil || it.Kind != ItemAdrenalineOrb || p.Fainted {
		return
	}
	// Speed already maxed: canon's first refusal, and it is checked before the
	// Attack question rather than after.
	if p.Stages.Spe >= 6 {
		return
	}
	// The floor case. Only when nothing refused the drop — a guard that deleted
	// it leaves the orb free to fire even on a holder that was already at -6.
	if !refused && atkBefore <= -6 {
		return
	}
	consumeItemAnnounced(p, side, it, log)
	applyStages(p, side, "speed", 1, log)
}
