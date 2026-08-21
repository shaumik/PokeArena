package engine

import "fmt"

// Ability suppression: Neutralizing Gas and Gastro Acid.
//
// Both answer the same question — "does this Pokémon's ability do anything
// right now?" — and the engine had no way to ask it. abilityOf takes a
// *Pokemon and nothing else, and it is called from 62 places, several of them
// inside hook signatures that never carry the BattleState. Threading state
// through all of that to serve two mechanics is the wrong trade.
//
// So suppression is mirrored onto the Pokémon instead, exactly as field-wide
// item suppression is (Volatiles.MagicRoomHere / syncMagicRoomFlags, items.go).
// abilityOf reads one bool; syncAbilitySuppression is the only writer that
// derives it from the field, and ValidateStateInvariants checks the mirror
// against the field so a missing call site is a loud failure rather than a
// silent one.
//
// Why this was worth building: the second battle royale had a team switch
// Weezing in specifically to shut off a foe's ability, and lost the Pokémon
// for nothing — "neutralizing-gas" was registered with a Kind and no hooks,
// and no suppression code existed anywhere in the repo.

// AbilityNeutralizingGas suppresses every *other* ability on the field while
// its holder is out. Named as a constant because three files ask about this
// one slug by identity, and a typo'd string literal would fail silently — the
// exact way the ability was inert before.
const AbilityNeutralizingGas AbilityKind = "neutralizing-gas"

// emitsNeutralizingGas reports whether p is currently filling the field with
// gas. Reads p.Ability raw rather than going through abilityOf, which would be
// circular: abilityOf's answer is what this function is being used to compute.
//
// A fainted holder emits nothing. It stays the active until the replace phase,
// so without the check the gas would outlive the Pokémon by up to a full
// residual block — long enough for the foe to be denied a Speed Boost tick it
// should have got the moment Weezing went down.
//
// Gastro Acid on the gas holder shuts the gas off too. Canon runs the same
// order (Showdown's Pokemon#ignoringAbility tests the gastroacid volatile
// before the gas exemption), so a doused Weezing is just a Weezing.
func emitsNeutralizingGas(p *Pokemon) bool {
	return p != nil && !p.Fainted && p.Ability == AbilityNeutralizingGas &&
		!p.Volatiles.GastroAcid
}

// abilitySuppressionFor computes, from the field, whether side's active should
// have its ability suppressed. This is the definition; the mirror on the
// Pokémon is a cache of it.
//
// Order matches canon's: Gastro Acid first (it suppresses anything, including
// Neutralizing Gas itself), then the gas holder's exemption from its own gas,
// then the foe's gas.
func abilitySuppressionFor(s *BattleState, side int) bool {
	p := s.Active(side)
	if p == nil {
		return false
	}
	if p.Volatiles.GastroAcid {
		return true
	}
	if p.Ability == AbilityNeutralizingGas {
		return false
	}
	return emitsNeutralizingGas(s.Active(1 - side))
}

// syncAbilitySuppression pushes the field's suppression state onto both
// actives and handles the moment it lifts. The only writer of
// AbilitySuppressed that derives the value from the field.
//
// Idempotent: it compares before it writes, which is what makes it safe to
// call from every point where the field can have changed (see the call sites
// in switching.go and turn.go) without double-firing the resume below.
//
// **Resume is a real event, not just a flag flip.** When the gas leaves, canon
// re-runs the switch-in ability of everything still on the field — Showdown's
// neutralizinggas.onEnd calls singleEvent('Start', ...) for each active. That
// is the difference between the three cases the mechanic has to get right:
//
//	Drought's sun set before the gas arrived   stays up (nothing un-sets weather)
//	an Intimidate that already fired            does not un-fire (it was a one-off)
//	Multiscale / Regenerator                    genuinely stop, and genuinely restart
//
// and it is also why a Drought holder that entered *into* the gas gets its sun
// at the moment the gas clears rather than never.
//
// A Pokémon arriving on a gassed field is not a resume: it switches in with
// its volatiles zeroed, so the flag goes false→true here and no transition is
// reported. Only true→false fires.
func syncAbilitySuppression(s *BattleState, log *[]LogLine) {
	var resumed []int
	for i := 0; i < 2; i++ {
		p := s.Active(i)
		if p == nil {
			continue
		}
		want := abilitySuppressionFor(s, i)
		if p.Volatiles.AbilitySuppressed == want {
			continue
		}
		p.Volatiles.AbilitySuppressed = want
		if !want {
			resumed = append(resumed, i)
		}
	}
	if len(resumed) == 0 {
		return
	}
	// no-reveal: field-level and names nobody. The gas holder revealed itself
	// when it announced on entry, and this line is about the field going back
	// to normal — the resumed abilities below each reveal themselves if and
	// when they actually announce something.
	*log = append(*log, LogLine{
		Type: "ability", Side: -1,
		Text: "The effects of Neutralizing Gas wore off!",
	})
	for _, side := range resumed {
		applyOnSwitchIn(s, side, log)
	}
}

// seedAbilitySuppression establishes the mirror on a freshly built battle,
// before any turn is resolved.
//
// Needed because LegalActions is answered from the state alone: a Weezing led
// against an Arena Trap is free to switch on the very first choice, and the
// controller asks that question before ResolveTurn has run once. Without this,
// the first turn's sync was the earliest the field became true, and everything
// read before it saw an ungassed board — including ValidateStateInvariants,
// which would have failed a brand-new battle.
//
// No log and no resume: nothing can have been suppressed yet, so there is
// nothing to announce or restart. The gas holder's own entry announcement
// still comes from its switch-in hook on turn 1.
func seedAbilitySuppression(s *BattleState) {
	for i := 0; i < 2; i++ {
		if p := s.Active(i); p != nil {
			p.Volatiles.AbilitySuppressed = abilitySuppressionFor(s, i)
		}
	}
}

// announceNeutralizingGas is the gas holder's own switch-in hook. It only
// speaks — the suppression itself is already in place by the time this runs,
// installed by the syncAbilitySuppression call that every switch-in path makes
// before it reaches the ability hooks.
//
// Splitting the announcement from the effect this way is what makes turn-1
// leads work: syncAbilitySuppression runs at the top of ResolveTurn, ahead of
// both leads' switch-in hooks, so a lead Drought facing a lead Weezing is
// already suppressed when its own hook would have fired. Canon reaches the
// same place by giving the gas an onPreStart that runs before every onStart.
func announceNeutralizingGas(s *BattleState, side int, log *[]LogLine) {
	p := s.Active(side)
	revealAbility(p)
	*log = append(*log, LogLine{
		Type: "ability", Side: side,
		Text: fmt.Sprintf("%s's Neutralizing Gas filled the area!", p.Name),
	})
}
