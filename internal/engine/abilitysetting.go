package engine

import (
	"fmt"

	"pokearena/internal/domain"
)

// abilitysetting.go owns the four moves that rewrite an ability in place:
// Worry Seed, Simple Beam, Role Play and Skill Swap.
//
// All four arrive from data-sync as bare status shells — no Primary block, no
// volatile, nothing — so before this file existed they fell through to the
// declarative path at the bottom of applyStatusMoveFrom and resolved as clean
// successes. A Worry Seed narrated a hit and left Flash Fire exactly where it
// was. That is worse than an unimplemented move: the log said the plan worked.
//
// The mechanism they share is Showdown's Pokemon#setAbility, which is not a
// field assignment. It runs three things in order:
//
//	singleEvent('End', oldAbility)    tear down what the old ability was holding
//	pokemon.ability = new            the write
//	singleEvent('Start', newAbility)  the new ability's entry effect, on the spot
//
// so Skill Swapping a Drought holder sets the sun mid-turn, and Worry Seed's
// Insomnia wakes a sleeping target the instant it lands. setAbilityInPlace
// below is that sequence, with the engine's OnEnd / OnSwitchIn hooks standing
// in for the two singleEvents and a suppression re-sync in between (the swap
// can move Neutralizing Gas from one side to the other).
//
// Scope: the changed ability is field-scoped, so it reverts on switch-out.
// That is already handled — installSwitchIn restores from BaseAbility, which
// Trace has always written and these moves now write too.

// There is no abilitySettable predicate here on purpose. Upstream refuses a
// write when either the outgoing or the incoming ability carries the
// `cantsuppress` flag — Multitype, Stance Change, Comatose and the rest of that
// list — and not one of them is on any of this dex's 80 species, so the guard
// would be a branch that can never be taken. An empty ability slot is *not* a
// refusal: canon gives every Pokémon an ability, so a blank one here is missing
// data rather than a Pokémon that resists being changed, and Worry Seed landing
// on it is the behavior that matches.

// abilitySwappable reports whether an ability survives a Skill Swap. Upstream's
// `failskillswap` flag; in this dex only Neutralizing Gas carries it, and it
// carries it for a reason worth stating — the gas suppresses whatever it is
// swapped onto, so a swap would be a way to hand a Pokémon an ability that
// cannot work while simultaneously turning the gas off on the holder that was
// paying for it.
func abilitySwappable(k AbilityKind) bool {
	return k != AbilityNeutralizingGas
}

// abilityRolePlayable reports whether an ability can be copied by Role Play.
// Upstream's `failroleplay` flag covers a wider set than Trace's `notrace`, but
// on this dex the two lists coincide — Trace and Neutralizing Gas — so this
// defers to abilityTraceable rather than repeating it: one list, one place to
// correct. Copying an empty slot is refused there, which is right for a *copy*
// even though writing onto one is fine: there is nothing to take.
func abilityRolePlayable(k AbilityKind) bool {
	return abilityTraceable(k)
}

// setAbilityInPlace overwrites side's active ability and runs the two halves of
// canon's setAbility around the write. Returns false when the target has no
// live body to change (a fainted Pokémon keeps whatever it had).
//
// The BaseAbility bookkeeping is deliberately "first writer wins": it records
// what the Pokémon walked on with, so a target hit by Worry Seed and then by
// Skill Swap still reverts to its real ability on switch-out rather than to the
// Insomnia it was wearing in between.
func setAbilityInPlace(s *BattleState, side int, to AbilityKind, log *[]LogLine) bool {
	p := s.Active(side)
	if p == nil || p.Fainted || p.HP <= 0 {
		return false
	}
	// Canon routes every one of these through Pokemon#setAbility, which runs a
	// SetAbility event that Ability Shield answers with null. Refusing here is
	// the one place that covers all four moves plus Trace, rather than five.
	//
	// Reports success, which reads oddly and is right: canon distinguishes
	// `false` (the write was refused — the move failed) from `null` (the write
	// was blocked — the move resolved and did nothing), and Ability Shield is
	// the null case. Worry Seed is explicit about it, returning setAbility's
	// own `false | null` straight out of onHit, so a shielded target gets the
	// block line and no "But it failed!" after it. Returning false here would
	// add a failure line canon does not print, and — worse — would arm
	// Stomping Tantrum.
	if abilityShieldBlocks(p, side, log) {
		return true
	}
	from := p.Ability
	if a := abilityOf(p); a != nil && a.OnEnd != nil {
		a.OnEnd(p, side, log)
	}
	if p.BaseAbility == "" {
		p.BaseAbility = from
	}
	p.Ability = to
	// Both abilities are public the moment the move announces the exchange.
	revealAbility(p)
	*log = append(*log, LogLine{
		Type: "ability", Side: side,
		Text: fmt.Sprintf("%s acquired %s!", p.Name, prettyAbilityName(to)),
	})
	finishAbilityChange(s, side, log)
	return true
}

// finishAbilityChange runs everything that must follow a write to Pokemon.
// Ability: the field-wide suppression mirror, the new ability's entry effect,
// and the status sweep.
//
// The status sweep is the half that is easy to miss. Canon does not implement
// "Immunity cures poison" as a separate rule — Immunity has an onUpdate that
// cures on any update, and gaining the ability is an update. Here the refusal
// predicate (BlocksStatus) is the same one inflictStatus consults, so asking it
// about the status the holder already has is exactly the right question: an
// ability that would have refused this status is an ability that clears it.
func finishAbilityChange(s *BattleState, side int, log *[]LogLine) {
	syncAbilitySuppression(s, log)
	applyOnSwitchIn(s, side, log)
	p := s.Active(side)
	if p.Status != StatusNone &&
		(abilityBlocksStatus(p, p.Status) || abilityBlocksStatusState(s, p, p.Status)) {
		revealAbility(p)
		cureStatus(p, side, log)
	}
}

// applyAbilitySettingMove handles the four ability-rewriting moves and reports
// whether it claimed this one. Called from applyStatusMoveFrom beside the other
// JS-callback moves.
//
// `side` is the user. Worry Seed and Simple Beam write the foe; Role Play
// writes the user; Skill Swap writes both.
func applyAbilitySettingMove(s *BattleState, side int, m domain.Move, log *[]LogLine) bool {
	switch m.ID {
	case "worry-seed", "simple-beam", "role-play", "skill-swap":
	default:
		return false
	}
	user, foe := s.Active(side), s.Active(1-side)
	failed := func() bool {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return true
	}
	if foe == nil || foe.Fainted {
		return failed()
	}

	switch m.ID {
	case "worry-seed":
		// Insomnia and Truant fail before accuracy is even rolled upstream;
		// what transfers here is that the move refuses rather than wasting a
		// write. Insomnia is on this dex (Hypno); Truant is not, and is named
		// because the refusal is about the ability, not about who has it.
		if foe.Ability == "insomnia" || foe.Ability == "truant" {
			return failed()
		}
		// The sleep cure rides finishAbilityChange's status sweep: Insomnia
		// refuses sleep, so a sleeping target gaining it wakes up. Canon spells
		// the cure out in the move as well as in the ability; one of the two is
		// enough, and the ability is the one that also covers Skill Swap.
		if !setAbilityInPlace(s, 1-side, "insomnia", log) {
			return failed()
		}

	case "simple-beam":
		if foe.Ability == AbilitySimple || foe.Ability == "truant" {
			return failed()
		}
		if !setAbilityInPlace(s, 1-side, AbilitySimple, log) {
			return failed()
		}

	case "role-play":
		if foe.Ability == user.Ability || !abilityRolePlayable(foe.Ability) {
			return failed()
		}
		revealAbility(foe)
		if !setAbilityInPlace(s, side, foe.Ability, log) {
			return failed()
		}

	case "skill-swap":
		// Gen 6 onward a swap between two holders of the same ability is legal
		// (it just does nothing visible); only gen 5 and earlier refused it, so
		// there is no same-ability guard here.
		if !abilitySwappable(user.Ability) || !abilitySwappable(foe.Ability) {
			return failed()
		}
		if !swapAbilities(s, side, log) {
			return failed()
		}
	}
	return true
}

// swapAbilities exchanges the two actives' abilities. Split out because the
// exchange cannot be two setAbilityInPlace calls: the first would rewrite the
// value the second is meant to read, and each would run the incoming ability's
// entry effect against a field that is still half-swapped. Canon has the same
// shape — both Ends, then both writes, then both Starts.
func swapAbilities(s *BattleState, side int, log *[]LogLine) bool {
	user, foe := s.Active(side), s.Active(1-side)
	if user == nil || foe == nil || user.Fainted || foe.Fainted {
		return false
	}
	// A shield on *either* side refuses the whole exchange, and nothing is
	// half-swapped. Canon's skillSwap runs SetAbility on the target and then on
	// the source, returning early on the first refusal — before any write — so
	// a shield holder using Skill Swap is refused just as surely as one being
	// targeted by it. The suite has a case for each direction.
	// True for the same reason setAbilityInPlace returns true: canon's
	// skillSwap hands back the refused event's `null`, which onHit passes
	// through as a resolved no-op rather than a failure.
	if abilityShieldBlocks(foe, 1-side, log) || abilityShieldBlocks(user, side, log) {
		return true
	}
	userAbility, foeAbility := user.Ability, foe.Ability

	if a := abilityOf(user); a != nil && a.OnEnd != nil {
		a.OnEnd(user, side, log)
	}
	if a := abilityOf(foe); a != nil && a.OnEnd != nil {
		a.OnEnd(foe, 1-side, log)
	}
	if user.BaseAbility == "" {
		user.BaseAbility = userAbility
	}
	if foe.BaseAbility == "" {
		foe.BaseAbility = foeAbility
	}
	user.Ability, foe.Ability = foeAbility, userAbility
	revealAbility(user)
	revealAbility(foe)
	*log = append(*log, LogLine{
		Type: "ability", Side: side,
		Text: fmt.Sprintf("%s swapped Abilities with %s!", user.Name, foe.Name),
	})
	// Suppression is resolved once, from the finished field, before either
	// entry effect runs — a swap can move Neutralizing Gas across the field and
	// an entry effect fired against the half-swapped state would read the wrong
	// answer. syncAbilitySuppression is idempotent, so the per-side calls
	// inside finishAbilityChange below are free.
	syncAbilitySuppression(s, log)
	finishAbilityChange(s, side, log)
	finishAbilityChange(s, 1-side, log)
	return true
}
