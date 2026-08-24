package engine

import (
	"fmt"

	"pokearena/internal/domain"
	"pokearena/internal/specs"
)

// HazardKind identifies an entry-hazard side condition. Empty means none;
// concrete values match the slugs the domain layer's Move.SideCondition
// field uses (set by the data-sync transform).
type HazardKind string

const (
	HazardStealthRock HazardKind = "stealthrock"
	HazardSpikes      HazardKind = "spikes"
	HazardToxicSpikes HazardKind = "toxicspikes"
)

// Hazards is the bag of entry-hazard layers on one side. StealthRock is
// binary (canon: one layer, ×4 chip on a 4× weak switch-in). Spikes stacks
// to 3 layers (1/8, 1/6, 1/4 chip on grounded switch-ins). ToxicSpikes
// stacks to 2 (1 = poison, 2 = toxic on a grounded non-Poison non-Steel
// switch-in; a grounded Poison-type clears the layers on entry).
type Hazards struct {
	StealthRock bool `json:"stealth_rock,omitempty"`
	Spikes      int  `json:"spikes,omitempty"`
	ToxicSpikes int  `json:"toxic_spikes,omitempty"`
}

// spikesLayerCap / toxicSpikesLayerCap are the canonical maxima. Setters
// past the cap fail with "But it failed!".
const (
	spikesLayerCap      = 3
	toxicSpikesLayerCap = 2
)

// rockChartAgainst returns the Rock-type effectiveness against a defender.
// Hardcoded so the switch-in hook needn't carry a *domain.Dex through every
// ability/hazard call site. Source of truth is internal/domain — if the
// type chart ever changes, this table needs the matching update.
//
// Rock is 2× vs Fire/Ice/Flying/Bug; 0.5× vs Fighting/Ground/Steel;
// 1× otherwise. No type is immune to Rock, so Stealth Rock chips every
// switch-in (including Flying-types — which is the whole reason it's the
// most influential hazard in the game).
func rockChartAgainst(t domain.Type) float64 {
	switch t {
	case "fire", "ice", "flying", "bug":
		return 2.0
	case "fighting", "ground", "steel":
		return 0.5
	}
	return 1.0
}

func stealthRockEffectiveness(p *Pokemon) float64 {
	mult := rockChartAgainst(p.Type1)
	if p.Type2 != "" {
		mult *= rockChartAgainst(p.Type2)
	}
	return mult
}

// isPoisonType reports whether p has Poison as one of its types. Used by
// Toxic Spikes absorption — a grounded Poison-type clears the layers on
// entry, regardless of its other type.
func isPoisonType(p *Pokemon) bool { return isType(p, "poison") }

// applyHazardsOnSwitchIn fires the entry-hazard sequence on side as a
// Pokémon walks in. Canon order is Stealth Rock → Spikes → Toxic Spikes;
// each phase short-circuits if the incoming faints. Called by
// doSwitchWithCarry after the "Go, X!" log line, before the ability
// switch-in hook.
//
// Magic Guard immunizes against the damage chips (Stealth Rock, Spikes)
// but not against Toxic Spikes' status — canon: Magic Guard prevents
// indirect *damage*, not status infliction. A grounded Poison-type
// absorbs Toxic Spikes (the layers clear and the status is skipped);
// Steel-types resist the poison status itself via inflictStatus' type
// immunity, but the layers persist.
//
// Heavy-Duty Boots used to be checked once at the top, with a note saying the
// item covers the whole category. That is true of the two chips and false of
// one branch: upstream's toxicspikes onSwitchIn asks about groundedness and the
// Poison-type absorb *before* it asks about the boots, so a booted grounded
// Poison-type soaks the layers up and clears the field for whatever comes next.
// Stopping the wearer being poisoned is not the same as stopping the layers
// being absorbed. So the check sits on the two chips and on the status leg of
// Toxic Spikes, and the absorb runs regardless.
func applyHazardsOnSwitchIn(s *BattleState, side int, log *[]LogLine) {
	h := &s.Sides[side].Conditions.Hazards
	p := s.Active(side)
	if p == nil || p.Fainted {
		return
	}
	boots := itemIgnoresHazards(p)

	if h.StealthRock && !boots {
		applyStealthRockChip(p, side, log)
		if p.Fainted {
			return
		}
	}
	if h.Spikes > 0 && !boots && isGroundedOnEntry(s, p) {
		applySpikesChip(p, side, h.Spikes, log)
		if p.Fainted {
			return
		}
	}
	if h.ToxicSpikes > 0 && isGroundedOnEntry(s, p) {
		applyToxicSpikesEntry(s, side, boots, log)
	}
}

func applyStealthRockChip(p *Pokemon, side int, log *[]LogLine) {
	if abilityBlocksIndirectDamage(p) {
		return
	}
	mult := stealthRockEffectiveness(p)
	dmg := int(float64(p.MaxHP) * mult / 8.0)
	if dmg < 1 {
		dmg = 1
	}
	if dmg > p.HP {
		dmg = p.HP
	}
	p.HP -= dmg
	*log = append(*log, LogLine{
		Type: "hazard", Side: side,
		Text: fmt.Sprintf("Pointed stones dug into %s! (-%d)", p.Name, dmg),
	})
	if p.HP <= 0 {
		faint(p, side, log)
	}
}

// spikesFractionDenom returns the divisor for spikes chip at a given layer
// count: 1 → 8 (12.5%), 2 → 6 (≈16.7%), 3 → 4 (25%). Canonical.
func spikesFractionDenom(layers int) int {
	switch layers {
	case 1:
		return 8
	case 2:
		return 6
	case 3:
		return 4
	}
	return 0
}

func applySpikesChip(p *Pokemon, side int, layers int, log *[]LogLine) {
	if abilityBlocksIndirectDamage(p) {
		return
	}
	denom := spikesFractionDenom(layers)
	if denom == 0 {
		return
	}
	dmg := p.MaxHP / denom
	if dmg < 1 {
		dmg = 1
	}
	if dmg > p.HP {
		dmg = p.HP
	}
	p.HP -= dmg
	*log = append(*log, LogLine{
		Type: "hazard", Side: side,
		Text: fmt.Sprintf("%s was hurt by the spikes! (-%d)", p.Name, dmg),
	})
	if p.HP <= 0 {
		faint(p, side, log)
	}
}

// applyToxicSpikesEntry handles the Toxic Spikes interaction for a
// grounded switch-in. A Poison-type absorbs the layers (they clear, no
// status). Otherwise inflictStatus tries to apply poison (1 layer) or
// toxic (2 layers) — Steel-types are filtered out there by the existing
// type-immunity check, so the layers persist for the next non-Steel
// switch-in.
func applyToxicSpikesEntry(s *BattleState, side int, boots bool, log *[]LogLine) {
	p := s.Active(side)
	h := &s.Sides[side].Conditions.Hazards
	if isPoisonType(p) {
		h.ToxicSpikes = 0
		*log = append(*log, LogLine{
			Type: "hazard", Side: side,
			Text: fmt.Sprintf("%s absorbed the Toxic Spikes!", p.Name),
		})
		return
	}
	// Heavy-Duty Boots keep the poison off, below the absorb — see the note on
	// applyHazardsOnSwitchIn.
	if boots {
		return
	}
	st := StatusPoison
	if h.ToxicSpikes >= 2 {
		st = StatusToxic
	}
	inflictStatus(p, side, st, s, nil, log)
}

func init() {
	specs.RegisterSideCondition("stealthrock")
	specs.RegisterSideCondition("spikes")
	specs.RegisterSideCondition("toxicspikes")
	registerSideCondition("stealthrock", func(s *BattleState, side int, log *[]LogLine) {
		applyHazardSetter(s, side, HazardStealthRock, log)
	})
	registerSideCondition("spikes", func(s *BattleState, side int, log *[]LogLine) {
		applyHazardSetter(s, side, HazardSpikes, log)
	})
	registerSideCondition("toxicspikes", func(s *BattleState, side int, log *[]LogLine) {
		applyHazardSetter(s, side, HazardToxicSpikes, log)
	})
}

// applyHazardSetter spawns or stacks an entry hazard on the foe's side.
// kind is the hazard slug parsed from Move.SideCondition; caster is the
// side that used the setter, so layers go on (1 - caster).
//
// Stacking: Stealth Rock is binary (already up → fail). Spikes and Toxic
// Spikes tick up to their cap (1→2→3 / 1→2); at the cap, the setter
// fails with the canonical "But it failed!" log line.
func applyHazardSetter(s *BattleState, caster int, kind HazardKind, log *[]LogLine) {
	target := 1 - caster
	h := &s.Sides[target].Conditions.Hazards
	switch kind {
	case HazardStealthRock:
		if h.StealthRock {
			*log = append(*log, LogLine{Type: "fail", Side: caster, Text: "But it failed!"})
			return
		}
		h.StealthRock = true
		*log = append(*log, LogLine{
			Type: "hazard", Side: target,
			Text: "Pointed stones float in the air around the foe's team!",
		})
	case HazardSpikes:
		if h.Spikes >= spikesLayerCap {
			*log = append(*log, LogLine{Type: "fail", Side: caster, Text: "But it failed!"})
			return
		}
		h.Spikes++
		*log = append(*log, LogLine{
			Type: "hazard", Side: target,
			Text: "Spikes were scattered all around the foe's team's feet!",
		})
	case HazardToxicSpikes:
		if h.ToxicSpikes >= toxicSpikesLayerCap {
			*log = append(*log, LogLine{Type: "fail", Side: caster, Text: "But it failed!"})
			return
		}
		h.ToxicSpikes++
		*log = append(*log, LogLine{
			Type: "hazard", Side: target,
			Text: "Poison spikes were scattered all around the foe's team's feet!",
		})
	}
}

// HazardChipOnSwitchIn estimates the HP a Pokémon would lose to the
// damage hazards on side sc when it switches in. Stealth Rock + Spikes
// only (Toxic Spikes is a status, accounted for separately in scoring
// heuristics). Used by the AI's switchScore so a switch into a
// hazard-coated side gets the realistic tempo penalty. Magic Guard
// zeros the estimate to match runtime behavior.
func HazardChipOnSwitchIn(p *Pokemon, sc *SideConditions, pw *PseudoWeather) int {
	if p == nil || sc == nil || abilityBlocksIndirectDamage(p) {
		return 0
	}
	h := sc.Hazards
	total := 0
	if h.StealthRock {
		mult := stealthRockEffectiveness(p)
		d := int(float64(p.MaxHP) * mult / 8.0)
		if d < 1 {
			d = 1
		}
		total += d
	}
	if h.Spikes > 0 && isGrounded(p, pw) {
		denom := spikesFractionDenom(h.Spikes)
		if denom > 0 {
			d := p.MaxHP / denom
			if d < 1 {
				d = 1
			}
			total += d
		}
	}
	return total
}

// clearHazardsOnSide wipes all entry-hazard layers from one side. Used by
// Rapid Spin (own side) and Defog (both sides). Returns true if anything
// was cleared, so the caller can decide whether to log a no-op line.
func clearHazardsOnSide(s *BattleState, side int) bool {
	h := &s.Sides[side].Conditions.Hazards
	if !h.StealthRock && h.Spikes == 0 && h.ToxicSpikes == 0 {
		return false
	}
	h.StealthRock = false
	h.Spikes = 0
	h.ToxicSpikes = 0
	return true
}

// clearScreensOnSide drops every screen on one side. Used by Defog (both
// sides) — canon: Defog clears the user's screens AND the foe's, in
// addition to hazards.
func clearScreensOnSide(s *BattleState, side int) bool {
	sc := &s.Sides[side].Conditions
	cleared := false
	if sc.Reflect != nil {
		sc.Reflect = nil
		cleared = true
	}
	if sc.LightScreen != nil {
		sc.LightScreen = nil
		cleared = true
	}
	if sc.AuroraVeil != nil {
		sc.AuroraVeil = nil
		cleared = true
	}
	return cleared
}

// applyRapidSpin is the post-damage hook for the rapid-spin move ID: it
// clears hazards on the user's own side, which Showdown encodes in JS and
// so has to be hand-coded here. The +1 Speed is *not* handled here — it
// rides the move's 100% self-targeted secondary and goes through
// applyDamageEffects with every other secondary.
//
// This comment used to claim the Speed boost was already wired while the
// data pipeline was quietly dropping the secondary's payload, so the boost
// never applied and the comment was what stopped anyone checking.
//
// Called from executeMove after applyDamageEffects, on at least one hit.
func applyRapidSpin(s *BattleState, side int, log *[]LogLine) {
	if !clearHazardsOnSide(s, side) {
		return
	}
	user := s.Active(side)
	*log = append(*log, LogLine{
		Type: "hazard", Side: side,
		Text: fmt.Sprintf("%s blew away the hazards!", user.Name),
	})
}

// applyDefog is the status-move handler for the defog move ID. Lowers the
// foe's evasion by 1 (Showdown encodes that in JS too, not in the move's
// boosts block), then clears hazards AND screens on BOTH sides — Gen 6+
// behavior. Terrain isn't cleared here yet (no test fixture exercises it
// in this batch; the canon behavior is to clear it too, which we'll fold
// in once the audit covers pseudoWeather).
func applyDefog(s *BattleState, side int, log *[]LogLine) {
	foe := s.Active(1 - side)
	// The evasion drop is hand-coded here rather than riding the boosts block,
	// so it needs that block's checks re-made: the herb one, and the substitute
	// one. Canon's defog onHit opens with
	// `if (!target.volatiles['substitute'] || move.infiltrates)` before it
	// boosts, and only then clears the field — so a doll stops the drop and not
	// the sweep. Same shape as the Intimidate one, found in the same pass and
	// unfiled by the port because no case reaches it.
	if !hasSubstitute(foe) {
		applyStagesFromFoe(foe, 1-side, "evasion", -1, s, log)
		applyItemStatCheck(foe, 1-side, log)
	}

	clearHazardsOnSide(s, side)
	clearHazardsOnSide(s, 1-side)
	clearScreensOnSide(s, side)
	clearScreensOnSide(s, 1-side)
	*log = append(*log, LogLine{
		Type: "hazard", Side: -1,
		Text: "All field effects were swept away!",
	})
}
