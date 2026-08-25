package engine

import (
	"fmt"

	"github.com/shaumik/PokeArena/internal/domain"
)

// skydrop.go implements Sky Drop: the two-turn move that takes its target with
// it.
//
// It was denied as "doubles-flavored two-turn", and that diagnosis does not
// survive reading upstream. Sky Drop's target is `any`, which is Fly's target;
// in a singles battle Showdown resolves it to the one opposing active like any
// other move; eleven of the nineteen cases upstream ships for it build singles
// battles, and every one of the eleven ledger rows this engine carries is a
// singles case. Nothing about the move needs a second slot.
//
// What actually blocked it is different and larger: this engine models no
// semi-invulnerability at all. A Pokémon mid-Fly is hittable by everything, and
// cancelAirborneCharge says so in as many words. Canon's Sky Drop puts *both*
// parties out of reach for the turn, and the three ledger rows about terrain
// residuals turn on exactly that.
//
// So this implements the hold and not the untargetability, and the seam between
// them is narrower than it sounds. In singles the two Pokémon out of reach are
// the only two on the field: the carrier is locked into finishing the move and
// the victim cannot act, so there is nobody left to take advantage of being
// able to hit them. What stays unmodeled is the residual gate — a Grassy
// Terrain still heals them, an Electric Terrain still refuses a Yawn — and
// those three rows stay open, saying so.
//
// The state lives on the carrier alone. See SkyDropState for why that is the
// design rather than the shortcut.

// heldBySkyDrop reports whether side's active is in the air on somebody else's
// Sky Drop.
//
// It is a scan rather than a flag because the hold is recorded once, on the
// carrier. Canon's isSkyDropped does the same walk for the same reason.
func heldBySkyDrop(s *BattleState, side int) bool {
	for i := range s.Sides {
		carrier := s.Active(i)
		if carrier == nil || carrier.Fainted {
			continue
		}
		sd := carrier.Volatiles.SkyDrop
		if sd == nil {
			continue
		}
		if sd.TargetSide == side && sd.TargetTeam == s.Sides[side].Active {
			return true
		}
	}
	return false
}

// skyDropVictim resolves the Pokémon a carrier has hold of, or nil if it is no
// longer the one standing there — it fainted and was replaced, or it was
// switched out by something the hold does not stop.
func skyDropVictim(s *BattleState, sd *SkyDropState) *Pokemon {
	if sd == nil || sd.TargetSide < 0 || sd.TargetSide > 1 {
		return nil
	}
	if s.Sides[sd.TargetSide].Active != sd.TargetTeam {
		return nil
	}
	p := s.Active(sd.TargetSide)
	if p == nil || p.Fainted {
		return nil
	}
	return p
}

// skyDropLiftRefused reports whether the lift cannot happen, and says why in the
// log when so.
//
// Canon's refusals, in canon's order. The weight clause is the one that does not
// transfer: upstream refuses a target of 200 kg or more, and this dataset has no
// weight column at all — the pokedex carries types, base stats, abilities and
// genders and nothing else. That is a dataset gap rather than a Sky Drop gap,
// and it is shared with Grass Knot, Low Kick, Heavy Slam, Heat Crash and Float
// Stone, so it is not fixed here.
func skyDropLiftRefused(s *BattleState, side int, m domain.Move, log *[]LogLine) bool {
	atk, def := s.Active(side), s.Active(1-side)
	fail := func() bool {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return true
	}
	if def == nil || def.Fainted {
		return fail()
	}
	// A doll is refused outright rather than eaten: canon returns false from
	// onTryHit, which is a failure and not a blocked hit.
	if hasSubstitute(def) && !bypassesSubstitute(m, atk) {
		return fail()
	}
	// Protect is a real refusal of the lift, and the reason upstream's
	// "should only make contact on the way down" case works: a King's Shield on
	// the lift turn leaves the target free to switch on the next one. Checked
	// here because the ordinary Protect gate lives well below the point a
	// two-turn move returns from.
	if protectBlocksFoeMove(def, m) {
		*log = append(*log, LogLine{
			Type: "protect", Side: 1 - side,
			Text: fmt.Sprintf("%s protected itself!", def.Name),
		})
		return true
	}
	// Nothing already in the air, or already carrying something, can be picked
	// up. Canon gets this from the invulnerability check it does not have a
	// counterpart for here, so it is stated directly.
	if def.Volatiles.Charging != nil || def.Volatiles.SkyDrop != nil {
		return fail()
	}
	return false
}

// startSkyDrop takes the target up. The carrier keeps its own Charging volatile
// as well, so the release goes through the ordinary two-turn path and resolves
// the same move; this records who is up there with it.
func startSkyDrop(s *BattleState, side, moveIdx int, log *[]LogLine) {
	atk, def := s.Active(side), s.Active(1-side)
	atk.Volatiles.SkyDrop = &SkyDropState{
		MoveIdx:    moveIdx,
		TargetSide: 1 - side,
		TargetTeam: s.Sides[1-side].Active,
	}
	*log = append(*log, LogLine{
		Type: "skydrop", Side: side,
		Text: fmt.Sprintf("%s took %s into the sky!", atk.Name, def.Name),
	})
}

// skyDropRelease resolves the drop, reporting whether the strike should go on to
// deal damage. False means the move is over: either there is nobody up there any
// more, or the target is a Flying-type, which canon picks up normally and then
// puts down for nothing.
//
// The caller clears the hold either way.
func skyDropRelease(s *BattleState, side int, log *[]LogLine) bool {
	atk := s.Active(side)
	sd := atk.Volatiles.SkyDrop
	victim := skyDropVictim(s, sd)
	if victim == nil {
		// Canon re-checks identity at the top of the drop and fails when the
		// Pokémon it took up is not the one below it any more. It is also
		// careful not to claim it dropped anything, which is the whole of one
		// upstream case.
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return false
	}
	*log = append(*log, LogLine{
		Type: "skydrop", Side: 1 - side,
		Text: fmt.Sprintf("%s was released from the sky!", victim.Name),
	})
	if isType(victim, "flying") {
		// Carried up like anything else and dropped for nothing. Canon says so
		// with an immunity line rather than a failure, because the move did
		// happen — it simply had no effect on something that can fly.
		*log = append(*log, LogLine{
			Type: "immune", Side: side,
			Text: fmt.Sprintf("It doesn't affect %s...", victim.Name),
		})
		return false
	}
	return true
}
