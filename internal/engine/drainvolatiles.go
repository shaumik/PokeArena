package engine

import (
	"fmt"

	"pokearena/internal/domain"
	"pokearena/internal/specs"
)

// drainvolatiles.go owns the residual-heal / residual-drain volatiles:
// Leech Seed (chip the seeded foe, heal the seeder), Aqua Ring (heal
// the user), Ingrain (heal the user + block switching). Each clears
// on switch-out via the standard Volatiles{} reset in switching.go.
// Substitute blocks Leech Seed application at the applyEffectFields
// gate; Grass-type targets are immune (canonical) and the volatile
// handler enforces that.

func init() {
	specs.RegisterVolatile("leechseed")
	specs.RegisterVolatile("aquaring")
	specs.RegisterVolatile("ingrain")
	registerVolatile("leechseed", applyLeechSeedVolatile)
	registerVolatile("aquaring", applyAquaRingVolatile)
	registerVolatile("ingrain", applyIngrainVolatile)
}

// LeechSeedState marks a Pokémon as Leech-Seeded. SourceSide is the
// side that planted the seed; end-of-turn drains move HP from the
// seeded target to the source side's active. If the source active
// is fainted or the source side has no live members the drain still
// chips the target but the heal is skipped (canon).
type LeechSeedState struct {
	SourceSide int `json:"source_side"`
}

// applyLeechSeedVolatile plants the seed on a target. Grass-types
// are immune ("It doesn't affect X..."); a target already seeded is
// a no-op silent-fail (Showdown emits "But it failed!"). source is
// the user; we infer the seeding side from side ^ 1 since the
// volatile handler signature doesn't carry the attacker explicitly.
//
// Note: applyEffectFields' substitute gate sits upstream so a
// seed against a substituted target never reaches this handler.
func applyLeechSeedVolatile(p *Pokemon, side int, _ domain.Move, _ *BattleState, _ *RNG, log *[]LogLine) {
	if p.Volatiles.LeechSeed != nil {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	if isType(p, "grass") {
		*log = append(*log, LogLine{Type: "immune", Side: side,
			Text: fmt.Sprintf("It doesn't affect %s...", p.Name)})
		return
	}
	p.Volatiles.LeechSeed = &LeechSeedState{SourceSide: 1 - side}
	*log = append(*log, LogLine{Type: "status", Side: side,
		Text: fmt.Sprintf("%s was seeded!", p.Name)})
}

// applyAquaRingVolatile flags the user for end-of-turn 1/16-HP heal.
// A user already wearing the ring is a silent no-op (canonical
// Showdown — Aqua Ring on top of Aqua Ring is a wasted PP). No
// type immunity; the move is self-target so the substitute gate
// doesn't apply.
func applyAquaRingVolatile(p *Pokemon, side int, _ domain.Move, _ *BattleState, _ *RNG, log *[]LogLine) {
	if p.Volatiles.AquaRing {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	p.Volatiles.AquaRing = true
	*log = append(*log, LogLine{Type: "status", Side: side,
		Text: fmt.Sprintf("%s surrounded itself with a veil of water!", p.Name)})
}

// applyIngrainVolatile roots the user. End-of-turn 1/16-HP heal
// (same as Aqua Ring; stacks if both are up) and the user can no
// longer switch (enforced in LegalActions via ingrainBlocksSwitch).
// Re-applying is a silent no-op.
func applyIngrainVolatile(p *Pokemon, side int, _ domain.Move, _ *BattleState, _ *RNG, log *[]LogLine) {
	if p.Volatiles.Ingrain {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	p.Volatiles.Ingrain = true
	*log = append(*log, LogLine{Type: "status", Side: side,
		Text: fmt.Sprintf("%s planted its roots!", p.Name)})
}

// applyLeechSeedResidual chips the seeded target 1/8 max HP and
// heals the source side's active by the same amount (clamped to
// the actual HP drained). Magic Guard on the target skips the chip
// and the heal. Faint resolution runs here.
func applyLeechSeedResidual(s *BattleState, side int, log *[]LogLine) {
	p := s.Active(side)
	if p.Fainted || p.Volatiles.LeechSeed == nil {
		return
	}
	if abilityBlocksIndirectDamage(p) {
		return
	}
	dmg := p.MaxHP / 8
	if dmg < 1 {
		dmg = 1
	}
	if dmg > p.HP {
		dmg = p.HP
	}
	p.HP -= dmg
	*log = append(*log, LogLine{Type: "status", Side: side,
		Text: fmt.Sprintf("%s's health is sapped by Leech Seed! (-%d)", p.Name, dmg)})
	if p.HP <= 0 {
		faint(p, side, log)
	}
	src := s.Active(p.Volatiles.LeechSeed.SourceSide)
	if src.Fainted {
		return
	}
	if src.HP >= src.MaxHP {
		return
	}
	before := src.HP
	src.HP += dmg
	if src.HP > src.MaxHP {
		src.HP = src.MaxHP
	}
	*log = append(*log, LogLine{Type: "status", Side: p.Volatiles.LeechSeed.SourceSide,
		Text: fmt.Sprintf("%s drained HP! (+%d)", src.Name, src.HP-before)})
}

// applyRingHeals fires the 1/16 max-HP heals from Aqua Ring and
// Ingrain. Both heal independently — a target with both up gets
// two ticks. Heal is not indirect damage; Magic Guard is irrelevant.
func applyRingHeals(s *BattleState, side int, log *[]LogLine) {
	p := s.Active(side)
	if p.Fainted {
		return
	}
	if p.Volatiles.AquaRing && p.HP < p.MaxHP {
		amt := p.MaxHP / 16
		if amt < 1 {
			amt = 1
		}
		before := p.HP
		p.HP += amt
		if p.HP > p.MaxHP {
			p.HP = p.MaxHP
		}
		*log = append(*log, LogLine{Type: "status", Side: side,
			Text: fmt.Sprintf("Aqua Ring restored %s's HP! (+%d)", p.Name, p.HP-before)})
	}
	if p.Volatiles.Ingrain && p.HP < p.MaxHP {
		amt := p.MaxHP / 16
		if amt < 1 {
			amt = 1
		}
		before := p.HP
		p.HP += amt
		if p.HP > p.MaxHP {
			p.HP = p.MaxHP
		}
		*log = append(*log, LogLine{Type: "status", Side: side,
			Text: fmt.Sprintf("%s absorbed nutrients with its roots! (+%d)", p.Name, p.HP-before)})
	}
}

// ingrainBlocksSwitch reports whether the holder is rooted and so
// cannot switch out voluntarily. Called from LegalActions alongside
// the partial-trap check.
func ingrainBlocksSwitch(p *Pokemon) bool {
	return p != nil && p.Volatiles.Ingrain
}
