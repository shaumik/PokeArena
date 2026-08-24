package engine

import (
	"fmt"

	"pokearena/internal/domain"
)

// faint marks p as fainted and wipes its transient state: HP to zero,
// non-volatile status cleared, sleep / toxic counters reset, every
// volatile dropped. The "X fainted!" log line is the only side-effect
// the caller sees.
func faint(p *Pokemon, side int, log *[]LogLine) {
	if p.Fainted {
		return
	}
	p.HP = 0
	p.Fainted = true
	p.Status = StatusNone
	p.SleepTurns = 0
	p.ToxicCounter = 0
	// MagicRoomHere survives the wipe. It is not a volatile the Pokémon earned
	// — it mirrors field state that is still up — and zeroing it here made
	// faint() a fourth, unsynced writer, which is the most common event in the
	// game. The fainted mon stays the active until the replace phase, so the
	// mirror has to keep agreeing with the field until then.
	magicRoom := p.Volatiles.MagicRoomHere
	p.Volatiles = Volatiles{MagicRoomHere: magicRoom}
	*log = append(*log, LogLine{Type: "faint", Side: side, Text: p.Name + " fainted!"})
}

// healPokemon adds amt HP, capped at MaxHP, and logs the actual restored
// amount. Heals below 1 round up so heals never silently no-op on integer
// truncation; HP that's already at MaxHP is the only no-log case.
func healPokemon(p *Pokemon, side, amt int, log *[]LogLine) {
	if amt < 1 {
		amt = 1
	}
	before := p.HP
	p.HP += amt
	if p.HP > p.MaxHP {
		p.HP = p.MaxHP
	}
	if p.HP > before {
		*log = append(*log, LogLine{
			Type: "heal", Side: side,
			Text: fmt.Sprintf("%s restored %d HP.", p.Name, p.HP-before),
		})
	}
}

// hurt subtracts HP and records that the Pokémon was damaged this turn.
//
// The flag is canon's hurtThisTurn, which spreadDamage sets on *any* nonzero
// damage — a move, recoil, a residual, hazard chip, an item. That breadth is
// the point, and it is why this is a separate flag from DamagedThisTurn: the
// engine had collapsed three distinct upstream concepts into that one field.
// Canon keeps them apart because three different moves want different
// questions answered:
//
//	hurtThisTurn              took damage from anything      — Assurance
//	attackedBy[...].thisTurn  was hit by *this* foe          — Revenge, Avalanche
//	focuspunch lostFocus      was hit by a move              — Focus Punch
//
// DamagedThisTurn serves the second and third; nothing served the first, so
// Assurance did not double off a target's own Wild Charge recoil.
//
// Every site that lowers a Pokémon's HP routes through here. The one deliberate
// exception is the cost of putting up a Substitute: canon spends that through
// directDamage, which does not touch hurtThisTurn.
func hurt(p *Pokemon, amt int) {
	if amt <= 0 {
		return
	}
	p.HP -= amt
	p.Volatiles.HurtThisTurn = true
}

// applySelfDamage subtracts amt HP from p (capped at the current HP) and
// logs it as recoil. Used by the recoil effect path; called from contexts
// where the caller has already decided the damage figure (Magic Guard
// filtering, fractional rounding) belongs to the user.
func applySelfDamage(p *Pokemon, side, amt int, log *[]LogLine) {
	if amt < 1 {
		amt = 1
	}
	if amt > p.HP {
		amt = p.HP
	}
	hurt(p, amt)
	*log = append(*log, LogLine{
		Type: "recoil", Side: side,
		Text: fmt.Sprintf("%s is hit with recoil! (-%d)", p.Name, amt),
	})
	if p.HP <= 0 {
		faint(p, side, log)
	}
}

// isType reports whether p carries the given type as either primary or
// secondary. Empty Type2 is treated as no match (matches the dataset
// convention for mono-typed species).
func isType(p *Pokemon, t domain.Type) bool { return p.Type1 == t || p.Type2 == t }

// stagePtr returns a pointer to the stage slot for stat (or nil if stat
// names an unknown attribute). The pointer is the only mutation channel
// — applyStages does the clamping; ad-hoc callers should never write to
// the returned pointer directly.
func stagePtr(p *Pokemon, stat string) *int {
	switch stat {
	case "attack":
		return &p.Stages.Atk
	case "defense":
		return &p.Stages.Def
	case "spatk":
		return &p.Stages.SpA
	case "spdef":
		return &p.Stages.SpD
	case "speed":
		return &p.Stages.Spe
	case "accuracy":
		return &p.Stages.Acc
	case "evasion":
		return &p.Stages.Eva
	}
	return nil
}

// statName renders a stat slug for display in log lines ("attack" →
// "Attack", "spatk" → "Sp. Atk"). Accuracy / evasion stay lower-case to
// match canon log style.
func statName(stat string) string {
	switch stat {
	case "attack":
		return "Attack"
	case "defense":
		return "Defense"
	case "spatk":
		return "Sp. Atk"
	case "spdef":
		return "Sp. Def"
	case "speed":
		return "Speed"
	case "accuracy":
		return "accuracy"
	case "evasion":
		return "evasion"
	}
	return stat
}

// statusVerb renders a status slug as the past-tense participle for the
// "X was poisoned!" log line. Unknown statuses fall back to the slug.
func statusVerb(st StatusCond) string {
	switch st {
	case StatusBurn:
		return "burned"
	case StatusPoison:
		return "poisoned"
	case StatusToxic:
		return "badly poisoned"
	case StatusParalysis:
		return "paralyzed"
	case StatusSleep:
		return "put to sleep"
	case StatusFreeze:
		return "frozen solid"
	}
	return string(st)
}
