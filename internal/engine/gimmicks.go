package engine

import (
	"fmt"
	"math"

	"pokearena/internal/domain"
	"pokearena/internal/specs"
)

// gimmicks.go owns eight one-offs that don't cluster with any of the
// other mechanic files. All are volatiles; most are timer-shaped:
//
//   Magnet Rise   — 5-turn Ground immunity on user
//   Smack Down    — physical damage + grounds target (cancels Magnet
//                   Rise, Telekinesis, Levitate, Flying immunity to
//                   Ground; persists until switch)
//   Telekinesis   — 3-turn auto-hit vs target + Ground immunity
//   Snatch        — +4 priority, steals foe's next self-targeted
//                   status move
//   Magic Coat    — +4 priority, blocks foe's next foe-targeted
//                   status move (bounceback degraded to "blocked")
//   Stockpile     — counter 1..3, +1 Def / +1 SpD per use
//   Grudge        — register-only (PP drain not modeled)
//   Gastro Acid   — register-only (ability suppression not threaded)
//
// computeDamage's Ground-type immunity gate (Magnet Rise gives
// immunity; Smack Down lifts Flying / Levitate) is the only damage-
// path hook this file adds. Snatch / Magic Coat gate inside
// applyStatusMove via the helpers below. Stockpile's +1 / +1 ride
// through applyStages directly. tickGimmicks runs the end-of-turn
// decrement for Magnet Rise and Telekinesis; Snatch / Magic Coat
// clear in the transient sweep alongside Flinch / Protect.

func init() {
	specs.RegisterVolatile("magnetrise")
	specs.RegisterVolatile("smackdown")
	specs.RegisterVolatile("telekinesis")
	specs.RegisterVolatile("snatch")
	specs.RegisterVolatile("magiccoat")
	specs.RegisterVolatile("stockpile")
	specs.RegisterVolatile("grudge")
	specs.RegisterVolatile("gastroacid")
	registerVolatile("magnetrise", applyMagnetRiseVolatile)
	registerVolatile("smackdown", applySmackDownVolatile)
	registerVolatile("telekinesis", applyTelekinesisVolatile)
	registerVolatile("snatch", applySnatchVolatile)
	registerVolatile("magiccoat", applyMagicCoatVolatile)
	registerVolatile("stockpile", applyStockpileVolatile)
	registerVolatile("grudge", applyGrudgeVolatile)
	registerVolatile("gastroacid", applyGastroAcidVolatile)
}

// MagnetRiseState counts down 5 end-of-turn ticks of Ground immunity.
type MagnetRiseState struct {
	TurnsLeft int `json:"turns_left"`
}

// TelekinesisState counts down 3 end-of-turn ticks of auto-hit-vs-
// target + Ground immunity.
type TelekinesisState struct {
	TurnsLeft int `json:"turns_left"`
}

// StockpileState counts stacks 1..3. Each stack riders +1 Def /
// +1 SpD on apply via applyStages — the live stack count is
// surfaced so future Swallow / Spit Up moves can read it.
type StockpileState struct {
	Count int `json:"count"`
}

const (
	defaultMagnetRiseTurns  = 5
	defaultTelekinesisTurns = 3
	maxStockpileStacks      = 3
)

func applyMagnetRiseVolatile(p *Pokemon, side int, _ domain.Move, _ *BattleState, _ *RNG, log *[]LogLine) {
	if p.Volatiles.MagnetRise != nil {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	if p.Volatiles.SmackDown {
		// Already grounded — Magnet Rise can't lift off canon.
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	p.Volatiles.MagnetRise = &MagnetRiseState{TurnsLeft: defaultMagnetRiseTurns}
	*log = append(*log, LogLine{
		Type: "magnetrise", Side: side,
		Text: fmt.Sprintf("%s levitated with electromagnetism!", p.Name),
	})
}

// applySmackDownVolatile is the damage-primary handler for Smack Down.
// Sets the persistent grounding flag and cancels any active Magnet
// Rise / Telekinesis on the target.
func applySmackDownVolatile(p *Pokemon, side int, _ domain.Move, _ *BattleState, _ *RNG, log *[]LogLine) {
	if p.Volatiles.SmackDown {
		return
	}
	p.Volatiles.SmackDown = true
	p.Volatiles.MagnetRise = nil
	p.Volatiles.Telekinesis = nil
	*log = append(*log, LogLine{
		Type: "smackdown", Side: side,
		Text: fmt.Sprintf("%s fell straight down!", p.Name),
	})
}

func applyTelekinesisVolatile(p *Pokemon, side int, _ domain.Move, _ *BattleState, _ *RNG, log *[]LogLine) {
	if p.Volatiles.Telekinesis != nil {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	if p.Volatiles.SmackDown {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	p.Volatiles.Telekinesis = &TelekinesisState{TurnsLeft: defaultTelekinesisTurns}
	*log = append(*log, LogLine{
		Type: "telekinesis", Side: side,
		Text: fmt.Sprintf("%s was hurled into the air!", p.Name),
	})
}

// applySnatchVolatile arms the steal flag. Cleared either when a
// foe's self-target status move is intercepted or at end-of-turn.
func applySnatchVolatile(p *Pokemon, side int, _ domain.Move, _ *BattleState, _ *RNG, log *[]LogLine) {
	p.Volatiles.Snatch = true
	*log = append(*log, LogLine{
		Type: "snatch", Side: side,
		Text: fmt.Sprintf("%s waited for a target to make a move!", p.Name),
	})
}

// applyMagicCoatVolatile arms the block flag. Cleared either when a
// foe's foe-target status move is bounced or at end-of-turn.
func applyMagicCoatVolatile(p *Pokemon, side int, _ domain.Move, _ *BattleState, _ *RNG, log *[]LogLine) {
	p.Volatiles.MagicCoat = true
	*log = append(*log, LogLine{
		Type: "magiccoat", Side: side,
		Text: fmt.Sprintf("%s shrouded itself with Magic Coat!", p.Name),
	})
}

// applyStockpileVolatile stacks 1..3. Each stack rides +1 Def /
// +1 SpD via applyStages.
func applyStockpileVolatile(p *Pokemon, side int, _ domain.Move, _ *BattleState, _ *RNG, log *[]LogLine) {
	if p.Volatiles.Stockpile == nil {
		p.Volatiles.Stockpile = &StockpileState{Count: 0}
	}
	if p.Volatiles.Stockpile.Count >= maxStockpileStacks {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	p.Volatiles.Stockpile.Count++
	*log = append(*log, LogLine{
		Type: "stockpile", Side: side,
		Text: fmt.Sprintf("%s stockpiled %d!", p.Name, p.Volatiles.Stockpile.Count),
	})
	applyStages(p, side, "defense", 1, log)
	applyStages(p, side, "spdef", 1, log)
}

// hpRatioPowerMoves scale base power with the user's remaining HP
// (floor(150 × curHP ÷ maxHP), min 1). Only water-spout is in the current
// Gen-1-scoped learnset; eruption and dragon-energy are listed so the mechanic
// is correct the moment they're ever synced in.
var hpRatioPowerMoves = map[string]bool{
	"water-spout":   true,
	"eruption":      true,
	"dragon-energy": true,
}

func isHPRatioPowerMove(id string) bool { return hpRatioPowerMoves[id] }

// stockpileCount returns the user's live stockpile stacks (0 if none). Read by
// Spit Up (dynamic base power) and Swallow (heal fraction).
func stockpileCount(p *Pokemon) int {
	if p.Volatiles.Stockpile == nil {
		return 0
	}
	return p.Volatiles.Stockpile.Count
}

// releaseStockpile empties the stockpile and reverses the +1 Def / +1 SpD per
// stack it had granted (Spit Up and Swallow both spend the stockpile when they
// fire). No-op when nothing is stockpiled.
func releaseStockpile(p *Pokemon, side int, log *[]LogLine) {
	n := stockpileCount(p)
	if n == 0 {
		return
	}
	p.Volatiles.Stockpile = nil
	applyStages(p, side, "defense", -n, log)
	applyStages(p, side, "spdef", -n, log)
}

// swallowHealFraction maps a stockpile count to the fraction of max HP Swallow
// restores: 1/4, 1/2, full for 1/2/3 stacks.
var swallowHealFraction = map[int]float64{1: 0.25, 2: 0.5, 3: 1.0}

// applySwallow heals the user by the stockpile-scaled fraction and empties the
// stockpile. With no stockpile it fails loudly. Gated by move ID in
// applyStatusMove — Swallow ships with no declarative heal block because the
// amount is dynamic.
func applySwallow(s *BattleState, side int, log *[]LogLine) {
	p := s.Active(side)
	n := stockpileCount(p)
	if n == 0 {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	amt := int(math.Round(float64(p.MaxHP) * swallowHealFraction[n]))
	healPokemon(p, side, amt, log)
	releaseStockpile(p, side, log)
}

// applyGrudgeVolatile is register-only — PP drain on attacker faint
// isn't modeled. The flag is set so future PP-aware mechanics can
// read it, and the start-of-grudge flavor log fires.
func applyGrudgeVolatile(p *Pokemon, side int, _ domain.Move, _ *BattleState, _ *RNG, log *[]LogLine) {
	p.Volatiles.Grudge = true
	*log = append(*log, LogLine{
		Type: "grudge", Side: side,
		Text: fmt.Sprintf("%s wants its foe to bear a grudge!", p.Name),
	})
}

// applyGastroAcidVolatile suppresses the target's ability until it switches
// out (the volatile is cleared with the rest of the bag on the way off the
// field, which is canon's duration for it).
//
// The GastroAcid flag used to be set and read by nothing: the note here said
// suppression "isn't threaded into the ability hook layer", and the move's
// "ability was suppressed!" line was a claim the engine did not honor. It is
// honored now, and by the same one-bool gate on abilityOf that Neutralizing
// Gas uses — the two mechanics are the same question asked twice, so they
// share the answer. See abilitysuppression.go.
//
// Only the GastroAcid volatile is set here. The AbilitySuppressed mirror that
// abilityOf actually reads is left to syncAbilitySuppression, which runs
// immediately after this move resolves — writing it here as well was tried and
// removed: no mutation of either write could be told apart by any test, and a
// second writer of a mirror is exactly what desynced the Magic Room one.
func applyGastroAcidVolatile(p *Pokemon, side int, _ domain.Move, _ *BattleState, _ *RNG, log *[]LogLine) {
	if p.Volatiles.GastroAcid {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	p.Volatiles.GastroAcid = true
	*log = append(*log, LogLine{
		Type: "gastroacid", Side: side,
		Text: fmt.Sprintf("%s's ability was suppressed!", p.Name),
	})
}

// tickGimmicks decrements the timer volatiles on side's active and
// clears any that expire. Called from ResolveTurn's end-of-turn block.
// Persistent volatiles (SmackDown, Snatch, MagicCoat, Stockpile,
// Grudge, GastroAcid) are NOT touched here — Snatch / MagicCoat
// clear in the transient sweep (one-turn flags); the rest persist
// until switch-out wipes the bag.
func tickGimmicks(s *BattleState, side int, log *[]LogLine) {
	p := s.Active(side)
	if mr := p.Volatiles.MagnetRise; mr != nil {
		mr.TurnsLeft--
		if mr.TurnsLeft <= 0 {
			p.Volatiles.MagnetRise = nil
			*log = append(*log, LogLine{
				Type: "magnetrise", Side: side,
				Text: fmt.Sprintf("%s's electromagnetism wore off.", p.Name),
			})
		}
	}
	if tk := p.Volatiles.Telekinesis; tk != nil {
		tk.TurnsLeft--
		if tk.TurnsLeft <= 0 {
			p.Volatiles.Telekinesis = nil
			*log = append(*log, LogLine{
				Type: "telekinesis", Side: side,
				Text: fmt.Sprintf("%s was freed from the telekinesis!", p.Name),
			})
		}
	}
}

// Magnet Rise, Telekinesis and Smack Down used to have their own Ground-move
// predicates here, read by computeDamage and by nothing else — which is why a
// lifted Pokemon dodged Earthquake and still took Spikes. All three are legs of
// groundedness (terrain.go) now, so every rule that cares about the ground
// gets the same answer.

// telekinesisAutoHits reports whether moves against the target should
// auto-hit (Telekinesis lifts the target and makes it a sitting duck).
// Called from resolveAccuracy.
func telekinesisAutoHits(def *Pokemon) bool {
	return def.Volatiles.Telekinesis != nil
}

// snatchInterceptsSelfStatus reports whether the foe should steal
// this side's incoming self-targeted status move. On true, the
// caller logs the snatch line, clears the foe's flag, and routes the
// status move via the foe's side instead.
func snatchInterceptsSelfStatus(s *BattleState, side int, m domain.Move) bool {
	if m.Category != domain.CatStatus {
		return false
	}
	if m.Target != domain.TargetSelf {
		return false
	}
	foe := s.Active(1 - side)
	return foe.Volatiles.Snatch
}

// magicCoatBouncesFoeStatus reports whether the foe should bounce
// this side's incoming foe-targeted status move. Bounceback is
// degraded — the move is blocked outright rather than redirected,
// but the audit-cleaning slug is registered. On true the caller
// clears the foe's flag, logs the block line, and returns from
// applyStatusMove without applying any effect.
func magicCoatBouncesFoeStatus(s *BattleState, side int, m domain.Move) bool {
	if m.Category != domain.CatStatus {
		return false
	}
	if m.Target != domain.TargetFoe {
		return false
	}
	foe := s.Active(1 - side)
	return foe.Volatiles.MagicCoat
}
