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
// Confuse Ray on a confused foe doesn't reset the timer). Misty Terrain
// blocks confusion on grounded targets via terrainBlocksConfusion.
func applyConfusionVolatile(p *Pokemon, side int, _ domain.Move, s *BattleState, rng *RNG, log *[]LogLine) {
	if p.Volatiles.Confusion != nil {
		return
	}
	if s != nil && terrainBlocksConfusion(s.Terrain, p) {
		return
	}
	p.Volatiles.Confusion = &ConfusionState{Turns: rng.Range(2, 5)}
	*log = append(*log, LogLine{Type: "status", Side: side,
		Text: fmt.Sprintf("%s became confused!", p.Name)})
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
// trap lasts 4-5 turns (uniform without Grip Claw — items aren't
// modeled). End-of-turn ticks the counter and chips 1/8 max HP; switch
// is blocked while the volatile is active (enforced in LegalActions).
// source carries the move name for the flavoured "trapped by X!" log.
func applyPartialTrapVolatile(p *Pokemon, side int, source domain.Move, _ *BattleState, rng *RNG, log *[]LogLine) {
	if p.Volatiles.PartialTrap != nil {
		return
	}
	p.Volatiles.PartialTrap = &PartialTrapState{
		Turns:    4 + rng.IntN(2),
		MoveName: source.Name,
	}
	*log = append(*log, LogLine{Type: "status", Side: side,
		Text: fmt.Sprintf("%s was trapped by %s!", p.Name, source.Name)})
}
