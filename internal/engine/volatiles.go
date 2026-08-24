package engine

import (
	"fmt"

	"pokearena/internal/domain"
	"pokearena/internal/specs"
)

// volatiles.go owns the small move-inflicted volatiles that don't warrant
// their own mechanic file: Confusion, Flinch, Partial Trap. State structs
// live alongside the rest of Volatiles in battle.go; handlers register
// themselves into volatileHandlers via init() so applyVolatile stays a
// table lookup.

func init() {
	specs.RegisterVolatile("confusion")
	specs.RegisterVolatile("flinch")
	specs.RegisterVolatile("partiallytrapped")
	registerVolatile("confusion", applyConfusionVolatile)
	registerVolatile("flinch", applyFlinchVolatile)
	registerVolatile("partiallytrapped", applyPartialTrapVolatile)
}

// applyConfusionVolatile sets the Confusion clock (2-5 turns, RNG-driven)
// on the target. Re-applying while already confused is a no-op (canon —
// Confuse Ray on a confused foe doesn't reset the timer). Own Tempo refuses it
// outright, and Misty Terrain blocks it on grounded targets via
// terrainBlocksConfusion.
func applyConfusionVolatile(p *Pokemon, side int, _ domain.Move, s *BattleState, rng *RNG, log *[]LogLine) {
	if p.Volatiles.Confusion != nil {
		return
	}
	if abilityBlocksConfusion(p) {
		return
	}
	if s != nil && terrainBlocksConfusion(s.Terrain, &s.PseudoWeather, p) {
		return
	}
	p.Volatiles.Confusion = &ConfusionState{Turns: rng.Range(2, 5)}
	*log = append(*log, LogLine{
		Type: "status", Side: side,
		Text: fmt.Sprintf("%s became confused!", p.Name),
	})
	// A Persim or Lum Berry snaps the holder out immediately — the confusion
	// did land, it just doesn't survive the turn it landed on.
	applyItemStatusCure(p, side, log)
}

// applyFlinchVolatile flags the target as flinched for this turn. Cleared
// at end of turn by the transient sweep in ResolveTurn. Inner Focus and
// Shield Dust block via abilityBlocksFlinch; Steadfast fires reactively
// through applyOnFlinched after the flag lands.
func applyFlinchVolatile(p *Pokemon, side int, _ domain.Move, _ *BattleState, _ *RNG, log *[]LogLine) {
	if abilityBlocksFlinch(p) {
		return
	}
	p.Volatiles.Flinch = true
	applyOnFlinched(p, side, log)
}

// applyPartialTrapVolatile inflicts the multi-turn trap used by Bind,
// Wrap, Fire Spin, Whirlpool, Clamp, Sand Tomb, Infestation. Gen 5+:
// trap lasts 4-5 turns, or the full 7 when the trapper holds a Grip
// Claw, and chips 1/8 max HP per end-of-turn — 1/6 with a Binding
// Band. Both are read off the *trapper* here and stored on the trap,
// because the residual runs on the target and the trapper may be long
// gone by then. Switch is blocked while the volatile is active
// (enforced in LegalActions). source carries the move name for the
// flavored "trapped by X!" log.
func applyPartialTrapVolatile(p *Pokemon, side int, source domain.Move, s *BattleState, rng *RNG, log *[]LogLine) {
	if p.Volatiles.PartialTrap != nil {
		return
	}
	turns := 4 + rng.IntN(2)
	denom := partialTrapDenom
	// The trapper is the active on the other side; a nil state (unit tests that
	// call the handler directly) falls back to the untuned defaults.
	if s != nil {
		denom, turns = partialTrapTuning(s.Active(1-side), turns)
	}
	p.Volatiles.PartialTrap = &PartialTrapState{
		Turns:     turns,
		MoveName:  source.Name,
		ChipDenom: denom,
	}
	*log = append(*log, LogLine{
		Type: "status", Side: side,
		Text: fmt.Sprintf("%s was trapped by %s!", p.Name, source.Name),
	})
}
