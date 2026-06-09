package engine

import (
	"fmt"
	"math"

	"pokearena/internal/domain"
)

// volatileHandler is the contract a mechanic file fulfils to claim a
// `Move.Primary.Volatile` slug. The signature is wide so every existing
// case can register without an adapter — handlers that don't need rng or
// the battle state simply ignore those args. Registration happens at
// package init via registerVolatile from the mechanic's own file.
type volatileHandler func(p *Pokemon, side int, source domain.Move, s *BattleState, rng *RNG, log *[]LogLine)

// volatileHandlers is the dispatch table applyVolatile consults. Populated
// by registerVolatile calls in init() functions across the package. Each
// mechanic owns its slugs; the spine only routes the call.
var volatileHandlers = map[string]volatileHandler{}

func registerVolatile(slug string, h volatileHandler) {
	volatileHandlers[slug] = h
}

// sideConditionSetter is the contract a mechanic fulfils to claim a
// `Move.SideCondition` slug. Screens (Reflect / Light Screen / Aurora
// Veil) and hazards (Stealth Rock / Spikes / Toxic Spikes) register
// closures that bind their own ScreenKind / HazardKind constants and
// call applyScreenSetter / applyHazardSetter under the hood.
type sideConditionSetter func(s *BattleState, side int, log *[]LogLine)

// sideConditionSetters is the dispatch table applyStatusMove consults
// when a status move declares a side-condition. Same registration pattern
// as volatileHandlers.
var sideConditionSetters = map[string]sideConditionSetter{}

func registerSideCondition(slug string, h sideConditionSetter) {
	sideConditionSetters[slug] = h
}

// applyStatusMove handles the guaranteed primary effect of a status-category
// move. The primary applies to the move's declared target.
//
// Weather and terrain setters (Move.Weather / Move.Terrain != "") are
// dispatched here too: if the move names one, the new condition takes effect
// for its default-turn duration. A setter that names the *currently active*
// weather / terrain fails (matches Showdown — Rain Dance in rain is a
// wasted PP; same for Electric Terrain in electric terrain).
func applyStatusMove(s *BattleState, side int, m domain.Move, rng *RNG, log *[]LogLine) {
	if m.Weather != "" {
		applyWeatherSetter(s, side, WeatherKind(m.Weather), log)
		return
	}
	if m.Terrain != "" {
		applyTerrainSetter(s, side, TerrainKind(m.Terrain), log)
		return
	}
	if m.SideCondition != "" {
		if h, ok := sideConditionSetters[m.SideCondition]; ok {
			h(s, side, log)
		}
		return
	}
	if m.PseudoWeather != "" {
		if h, ok := pseudoWeatherSetters[m.PseudoWeather]; ok {
			h(s, side, log)
		}
		return
	}
	// Defog: status move with no top-level effect block — Showdown encodes
	// its evasion drop and field-wipe in JS. Handled here by move ID rather
	// than via the SideCondition path (Defog's own sideCondition is "").
	if m.ID == "defog" {
		applyDefog(s, side, log)
		return
	}
	// forceSwitch status variants (Roar, Whirlwind): no Primary,
	// no Weather/Terrain/etc — the whole point is the switch. A
	// foe with no live bench is a "But it failed!"; a successful
	// switch logs the drag-out line and runs hazards on the
	// incoming. The bypass-acc / sound flags from upstream are
	// already on the move; no extra accuracy work needed here.
	if m.ForceSwitch {
		if !applyForceSwitch(s, side, rng, log) {
			*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		}
		return
	}
	if m.Primary == nil {
		return
	}
	atk := s.Active(side)
	def := s.Active(1 - side)
	tgt, tside := def, 1-side
	if m.Target == domain.TargetSelf {
		tgt, tside = atk, side
	}
	if failed := applyEffectFields(m.Primary, m, atk, side, tgt, tside, 0, s, rng, log); failed {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
	}
}

// applyDamageEffects runs the post-damage effects of a damaging move: the
// guaranteed Self block on the user, the guaranteed Primary on the foe (e.g.
// partial-trap moves' volatileStatus), and each rolled Secondary on the foe.
// Primary effects bypass Shield Dust and Sheer Force the way Showdown's
// top-level effects do; only entries in m.Secondaries are gated by those.
func applyDamageEffects(s *BattleState, side int, m domain.Move, dmg int, rng *RNG, log *[]LogLine) {
	atk := s.Active(side)
	def := s.Active(1 - side)
	if m.Self != nil {
		applyEffectFields(m.Self, m, atk, side, atk, side, dmg, s, rng, log)
	}
	if m.Primary != nil && !def.Fainted {
		applyEffectFields(m.Primary, m, atk, side, def, 1-side, dmg, s, rng, log)
	}
	if !abilityBlocksSecondaries(def) && !abilityBlocksOwnSecondaries(atk) {
		for i := range m.Secondaries {
			sec := &m.Secondaries[i]
			if rng.Chance(sec.Chance) {
				applyEffectFields(sec, m, atk, side, def, 1-side, dmg, s, rng, log)
			}
		}
	}
}

// applyEffectFields applies an Effect block. atk/atkSide is the user; tgt/
// tgtSide is the block's target (foe for damage-move secondaries; the move's
// declared target for primaries; the user for self blocks). dmgDealt is the
// damage just dealt (for drain/recoil), zero for status moves. Returns true
// only if a status-infliction attempt failed (callers decide whether to log
// "But it failed!" — secondaries are silent, primaries are loud).
//
// Heal/Drain/Recoil/Cure/Rest always act on the user regardless of tgt; the
// other fields act on tgt. This matches canonical Pokémon mechanics: drain
// heals the attacker even though it's "on" a hit against the foe.
func applyEffectFields(e *domain.Effect, source domain.Move, atk *Pokemon, atkSide int, tgt *Pokemon, tgtSide int, dmgDealt int, s *BattleState, rng *RNG, log *[]LogLine) (statusFailed bool) {
	// Substitute on the target blocks foe-induced fields entirely: status
	// inflictions, volatile inflictions, boost drops, secondary riders. The
	// tgt==atk path (m.Self on damage moves, status moves with TargetSelf)
	// still applies because the doll doesn't sit between the user and its
	// own effects. Sound / bypass-sub moves treat the doll as transparent.
	// Returning true causes status-move dispatchers to log "But it failed!";
	// damage-move sites ignore the return value, so a sub-blocked secondary
	// is silent (canon).
	if tgt != atk && hasSubstitute(tgt) && !bypassesSubstitute(source, atk) {
		return true
	}
	if len(e.Boosts) > 0 {
		fromFoe := tgt != atk
		for _, stat := range orderedBoostStats(e.Boosts) {
			delta := e.Boosts[stat]
			if fromFoe && delta < 0 {
				applyStagesFromFoe(tgt, tgtSide, stat, delta, s, log)
			} else {
				applyStages(tgt, tgtSide, stat, delta, log)
			}
		}
	}
	if e.Status != "" {
		// Safeguard on the target's side blocks foe-induced non-volatile
		// status outright. Self-status (Rest is its own path; status moves
		// targeting self) bypasses since tgt==atk. Logged loud on a primary
		// (status-move dispatcher checks statusFailed), silent on a damage
		// secondary — same shape as the substitute block above.
		if tgt != atk && safeguardBlocksFoeStatus(s, tgtSide) {
			*log = append(*log, LogLine{Type: "safeguard", Side: tgtSide,
				Text: fmt.Sprintf("%s is protected by Safeguard!", tgt.Name)})
			statusFailed = true
		} else if !inflictStatus(tgt, tgtSide, StatusCond(e.Status), s, rng, log) {
			statusFailed = true
		}
	}
	if e.Volatile != "" {
		if tgt != atk && safeguardBlocksFoeVolatile(s, tgtSide, e.Volatile) {
			*log = append(*log, LogLine{Type: "safeguard", Side: tgtSide,
				Text: fmt.Sprintf("%s is protected by Safeguard!", tgt.Name)})
		} else {
			applyVolatile(tgt, tgtSide, e.Volatile, source, s, rng, log)
		}
	}
	if e.Heal > 0 {
		amt := int(math.Round(float64(atk.MaxHP) * e.Heal))
		healPokemon(atk, atkSide, amt, log)
	}
	if e.Drain > 0 && dmgDealt > 0 {
		amt := int(math.Round(float64(dmgDealt) * e.Drain))
		healPokemon(atk, atkSide, amt, log)
	}
	if e.Recoil > 0 && dmgDealt > 0 && !abilityBlocksIndirectDamage(atk) {
		// Canonical Showdown rounds (round-half-up) rather than truncating
		// — truncation systematically under-reported recoil on every hit
		// where the fraction landed above .5 (issue #27). Magic Guard makes
		// the user immune to recoil.
		amt := int(math.Round(float64(dmgDealt) * e.Recoil))
		applySelfDamage(atk, atkSide, amt, log)
	}
	if e.Cure {
		cureStatus(atk, atkSide, log)
	}
	if e.Rest {
		doRest(atk, atkSide, log)
	}
	return statusFailed
}

// cureStatus clears the user's non-volatile status. No-op if none.
func cureStatus(p *Pokemon, side int, log *[]LogLine) {
	if p.Status == StatusNone {
		return
	}
	prev := p.Status
	p.Status = StatusNone
	p.SleepTurns = 0
	p.ToxicCounter = 0
	*log = append(*log, LogLine{Type: "status", Side: side,
		Text: fmt.Sprintf("%s was cured of its %s!", p.Name, prev)})
}

// doRest implements Rest: cure any status, fully heal, then force a 2-turn
// sleep. Unlike normal status infliction this bypasses the "already has a
// status" check, since Rest *replaces* any existing status with Sleep.
func doRest(p *Pokemon, side int, log *[]LogLine) {
	p.Status = StatusSleep
	p.SleepTurns = 2
	p.ToxicCounter = 0
	p.HP = p.MaxHP
	*log = append(*log, LogLine{Type: "status", Side: side,
		Text: fmt.Sprintf("%s went to sleep and became healthy!", p.Name)})
}

// applyVolatile inflicts a volatile condition on the target. Routes the
// slug through volatileHandlers — each mechanic file registers its own
// handler at init() time, so adding a new volatile is one file edit
// (plus the state field on Volatiles). A fainted target short-circuits
// before any handler runs; an unknown slug is a silent no-op (matches
// the previous switch's default).
func applyVolatile(p *Pokemon, side int, name string, source domain.Move, s *BattleState, rng *RNG, log *[]LogLine) {
	if p.Fainted {
		return
	}
	if h, ok := volatileHandlers[name]; ok {
		h(p, side, source, s, rng, log)
	}
}

// orderedBoostStats returns the keys of a boost map in a stable order so the
// turn log is deterministic regardless of map iteration.
func orderedBoostStats(b map[string]int) []string {
	order := []string{"attack", "defense", "spatk", "spdef", "speed", "accuracy", "evasion"}
	out := make([]string, 0, len(b))
	for _, k := range order {
		if _, ok := b[k]; ok {
			out = append(out, k)
		}
	}
	return out
}

// inflictStatus applies a non-volatile status, respecting type immunities and
// the one-status-at-a-time rule. It reports whether the status took hold.
// s is the battle state, consulted for terrain guards (Misty blocks all
// status, Electric blocks Sleep, both only on grounded targets).
func inflictStatus(p *Pokemon, side int, st StatusCond, s *BattleState, rng *RNG, log *[]LogLine) bool {
	if p.Status != StatusNone || p.Fainted {
		return false
	}
	if abilityBlocksStatus(p, st) {
		return false
	}
	if s != nil && terrainBlocksStatus(s.Terrain, p, st) {
		return false
	}
	switch st {
	case StatusBurn:
		if isType(p, "fire") {
			return false
		}
	case StatusFreeze:
		if isType(p, "ice") {
			return false
		}
	case StatusParalysis:
		if isType(p, "electric") {
			return false
		}
	case StatusPoison, StatusToxic:
		if isType(p, "poison") || isType(p, "steel") {
			return false
		}
	}
	p.Status = st
	if st == StatusSleep {
		// Range is 2..4 (not 1..3) so a Pokémon inflicted on a turn it has
		// not yet moved doesn't immediately wake on the same turn's canAct.
		// Effective forced-skip turns are 1..3 either way (issue #24).
		p.SleepTurns = rng.Range(2, 4)
	}
	if st == StatusToxic {
		p.ToxicCounter = 1
	}
	*log = append(*log, LogLine{Type: "status", Side: side, Text: fmt.Sprintf("%s was %s!", p.Name, statusVerb(st))})
	return true
}

// applyStagesFromFoe is the foe-induced variant: it consults the target
// side's Mist shield first (blocks any drop outright), then ability
// guards (Clear Body, Hyper Cutter, Big Pecks, Keen Eye) that block
// specific drops, and finally fires reactor abilities (Defiant,
// Competitive) when a drop lands. Self-induced stat changes (Swords
// Dance, Curse on self, etc.) bypass this and call applyStages directly.
func applyStagesFromFoe(p *Pokemon, side int, stat string, delta int, s *BattleState, log *[]LogLine) {
	if mistBlocksFoeDrop(s, side) {
		*log = append(*log, LogLine{Type: "mist", Side: side,
			Text: fmt.Sprintf("%s is protected by the mist!", p.Name)})
		return
	}
	if abilityBlocksStatLowerByFoe(p, stat) {
		*log = append(*log, LogLine{Type: "ability", Side: side,
			Text: fmt.Sprintf("%s's ability prevented the stat drop!", p.Name)})
		return
	}
	applyStages(p, side, stat, delta, log)
	// Reactor hooks fire only when the drop actually occurred. applyStages
	// doesn't currently return a "did clamp" signal, so we recompute by
	// checking that the stage moved off its previous floor.
	applyOnStatLoweredByFoe(p, side, stat, log)
}

// applyStages changes a stat stage, clamped to -6..+6.
func applyStages(p *Pokemon, side int, stat string, delta int, log *[]LogLine) {
	ptr := stagePtr(p, stat)
	if ptr == nil {
		return
	}
	old := *ptr
	*ptr += delta
	if *ptr > 6 {
		*ptr = 6
	}
	if *ptr < -6 {
		*ptr = -6
	}
	if *ptr == old {
		dir := "higher"
		if delta < 0 {
			dir = "lower"
		}
		*log = append(*log, LogLine{Type: "stat", Side: side, Text: fmt.Sprintf("%s's %s won't go %s!", p.Name, statName(stat), dir)})
		return
	}
	*log = append(*log, LogLine{Type: "stat", Side: side,
		Text: fmt.Sprintf("%s's %s %s!", p.Name, statName(stat), stageVerb(delta))})
}

// stageVerb returns the canonical Pokémon log fragment for a stage change:
// ±1 "rose/fell", ±2 "rose sharply / harshly fell", ≥±3 "rose drastically /
// severely fell". The magnitude is based on the requested delta, not the
// clamped applied delta (canon convention).
func stageVerb(delta int) string {
	switch {
	case delta == 1:
		return "rose"
	case delta == 2:
		return "rose sharply"
	case delta >= 3:
		return "rose drastically"
	case delta == -1:
		return "fell"
	case delta == -2:
		return "harshly fell"
	case delta <= -3:
		return "severely fell"
	}
	return "changed"
}
