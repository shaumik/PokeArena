package engine

import (
	"fmt"
)

// applyResidual applies end-of-turn residual damage: non-volatile status
// (burn / poison / toxic) and partial-trap chip. Toxic escalates each
// turn (1/16, 2/16, ... capped at 15/16) via p.ToxicCounter; the partial-
// trap counter ticks down here too and the volatile clears at zero.
func applyResidual(s *BattleState, side int, log *[]LogLine) {
	p := s.Active(side)
	if p.Fainted {
		return
	}
	applyStatusResidual(p, side, log)
	if p.Fainted {
		return
	}
	applyPartialTrapResidual(p, side, log)
}

func applyStatusResidual(p *Pokemon, side int, log *[]LogLine) {
	var dmg int
	switch p.Status {
	case StatusBurn:
		dmg = p.MaxHP / 16
	case StatusPoison:
		dmg = p.MaxHP / 8
	case StatusToxic:
		// The sixteenth truncates *first* and the multiply comes after —
		// canon is clampIntRange(maxhp / 16, 1) * stage. One truncation after
		// the multiply agrees for the first three ticks and drifts from the
		// fourth on: on a 325 HP body it reads 20/40/60/81/101/121 where canon
		// reads 20/40/60/80/100/120, which is why three turns of testing never
		// caught it. docs/engine-findings.md's OPEN-3 was the same mistake
		// found and fixed for the move-damage path; residual damage was not
		// part of that sweep.
		tick := p.MaxHP / 16
		if tick < 1 {
			tick = 1
		}
		dmg = tick * p.ToxicCounter
		if p.ToxicCounter < 15 {
			p.ToxicCounter++
		}
	default:
		return
	}
	if abilityBlocksIndirectDamage(p) {
		return // Magic Guard: skip the chip but the status still ticks for toxic.
	}
	if dmg < 1 {
		dmg = 1
	}
	if dmg > p.HP {
		dmg = p.HP
	}
	hurt(p, dmg)
	*log = append(*log, LogLine{
		Type: "status", Side: side,
		Text: fmt.Sprintf("%s is hurt by its %s! (-%d)", p.Name, p.Status, dmg),
	})
	if p.HP <= 0 {
		faint(p, side, log)
	}
}

// applyPartialTrapResidual chips 1/8 max HP and ticks the trap counter.
// The volatile clears when the counter reaches zero, when the holder faints,
// when the trapper leaves the field, or when the holder spins it off.
// Magic Guard skips the chip but the counter still ticks — matching how
// burn/toxic still expire under Magic Guard.
func applyPartialTrapResidual(p *Pokemon, side int, log *[]LogLine) {
	pt := p.Volatiles.PartialTrap
	if pt == nil {
		return
	}
	if !abilityBlocksIndirectDamage(p) {
		dmg := pt.Chip(p.MaxHP)
		if dmg > p.HP {
			dmg = p.HP
		}
		hurt(p, dmg)
		*log = append(*log, LogLine{
			Type: "status", Side: side,
			Text: fmt.Sprintf("%s is hurt by %s! (-%d)", p.Name, pt.MoveName, dmg),
		})
		if p.HP <= 0 {
			faint(p, side, log)
			p.Volatiles.PartialTrap = nil
			return
		}
	}
	pt.Turns--
	if pt.Turns <= 0 {
		*log = append(*log, LogLine{
			Type: "status", Side: side,
			Text: fmt.Sprintf("%s was freed from %s!", p.Name, pt.MoveName),
		})
		p.Volatiles.PartialTrap = nil
	}
}

// speedOrder returns the two side indices fastest first, with Trick Room
// inverting the comparison and a speed tie broken by the seeded RNG.
//
// Two phases read it, and both are phases canon speed-sorts and this engine
// used to walk by side index. The residual phase is one
// fieldEvent('Residual') whose handlers go through Battle#speedSort, preceded
// by updateSpeed() so the order is fixed for the whole phase; the entry phase
// is one fieldEvent('SwitchIn') over every simultaneous arrival. Walking side 0
// then side 1 is invisible right up to the moment it is not: when a chip kills,
// who faints first decides what the survivor sees (Aftermath, Destiny Bond,
// Moxie, a Perish Song count) and which side is asked for a replacement; and
// after a double KO, whose Intimidate lands. It is also exactly the sort of
// thing that makes a replay diverge from the real game only in the games that
// were close.
//
// The speed read is goesFirst's, deliberately — effectiveSpeed, the side
// multiplier, Trick Room, in that arrangement — because a residual phase that
// disagreed with the move phase about who is faster would be a worse bug than
// the one this fixes.
func speedOrder(s *BattleState, rng *RNG) [2]int {
	w := effectiveWeather(s)
	s0 := int(float64(effectiveSpeed(s.Active(0), w)) * sideSpeedMult(s, 0))
	s1 := int(float64(effectiveSpeed(s.Active(1), w)) * sideSpeedMult(s, 1))
	first := 0
	switch {
	case s0 == s1:
		first = rng.IntN(2)
	case trickRoomActive(s):
		if s1 < s0 {
			first = 1
		}
	default:
		if s1 > s0 {
			first = 1
		}
	}
	return [2]int{first, 1 - first}
}

// applyWeatherResidual applies sandstorm chip damage to any active Pokémon
// that isn't Rock / Ground / Steel. Snow / Rain / Sun never chip; clear
// weather is a no-op. Faints fire here if the chip is lethal.
//
// order comes from speedOrder — the whole residual phase shares one, so the
// chips, the heals and the Perish Song count all agree about who is faster.
func applyWeatherResidual(s *BattleState, order [2]int, log *[]LogLine) {
	w := effectiveWeather(s) // honors Cloud Nine on either active
	if w == nil {
		return
	}
	for _, i := range order {
		p := s.Active(i)
		if p.Fainted {
			continue
		}
		dmg := weatherResidual(w, p)
		if dmg == 0 {
			continue
		}
		if abilityBlocksIndirectDamage(p) {
			continue // Magic Guard: sandstorm chip is indirect damage.
		}
		if dmg > p.HP {
			dmg = p.HP
		}
		hurt(p, dmg)
		*log = append(*log, LogLine{
			Type: "weather", Side: i,
			Text: fmt.Sprintf("%s is buffeted by the sandstorm! (-%d)", p.Name, dmg),
		})
		if p.HP <= 0 {
			faint(p, i, log)
		}
	}
}

// applyTerrainResidual fires Grassy Terrain's 1/16 max-HP end-of-turn heal on
// one grounded active. Other terrains don't have residual effects, so this is a
// no-op for them. Heals are not indirect damage — Magic Guard is irrelevant.
//
// Per-side rather than both-at-once because the caller has to interleave it
// with the item heals: upstream puts the Grassy heal at onResidualOrder 5,
// onResidualSubOrder 2 and Leftovers at 5 / 4, and Battle#comparePriority sorts
// order, then priority, then *speed*, then subOrder. So the whole of order 5 is
// one Speed-ordered block in which each Pokemon's terrain heal precedes its own
// Leftovers tick — which is what the upstream case is named after. This used to
// run in its own pass after the status chip, three orders too late.
func applyTerrainResidual(s *BattleState, side int, log *[]LogLine) {
	t := s.Terrain
	if t == nil {
		return
	}
	p := s.Active(side)
	if p.Fainted || p.HP >= p.MaxHP {
		return
	}
	amt := terrainGrassyHeal(t, &s.PseudoWeather, p)
	if amt == 0 {
		return
	}
	before := p.HP
	p.HP += amt
	if p.HP > p.MaxHP {
		p.HP = p.MaxHP
	}
	*log = append(*log, LogLine{
		Type: "terrain", Side: side,
		Text: fmt.Sprintf("%s is healed by the Grassy Terrain! (+%d)", p.Name, p.HP-before),
	})
}

// tickTerrain decrements the terrain's TurnsLeft. When it hits zero the
// terrain clears and a "<terrain> disappeared" line lands. Setters that
// name an already-active terrain are blocked at applyStatusMove, so a
// setter and a counter tick can't race here.
func tickTerrain(s *BattleState, log *[]LogLine) {
	if s.Terrain == nil {
		return
	}
	s.Terrain.TurnsLeft--
	if s.Terrain.TurnsLeft <= 0 {
		kind := s.Terrain.Kind
		s.Terrain = nil
		*log = append(*log, LogLine{Type: "terrain", Side: -1, Text: terrainClearedText(kind)})
		return
	}
	if txt := terrainContinuesText(s.Terrain.Kind); txt != "" {
		*log = append(*log, LogLine{Type: "terrain", Side: -1, Text: txt})
	}
}

// tickScreens decrements each active screen on side and clears any whose
// TurnsLeft hits zero. Screens have no per-turn flavor line — the log
// would be noisy on a Reflect+Light Screen team — only an expiry one.
func tickScreens(s *BattleState, side int, log *[]LogLine) {
	sc := &s.Sides[side].Conditions
	for _, kind := range []ScreenKind{ScreenReflect, ScreenLightScreen, ScreenAuroraVeil} {
		slot := screenSlot(sc, kind)
		if slot == nil || *slot == nil {
			continue
		}
		(*slot).TurnsLeft--
		if (*slot).TurnsLeft <= 0 {
			*slot = nil
			*log = append(*log, LogLine{Type: "screen", Side: side, Text: screenClearedText(kind)})
		}
	}
}

// tickWeather decrements the weather's TurnsLeft. When it hits zero the
// weather clears and a "<weather> stopped" line lands. Setters that name a
// weather already active are blocked at applyStatusMove, so a setter and a
// counter tick can't race here.
func tickWeather(s *BattleState, log *[]LogLine) {
	if s.Weather == nil {
		return
	}
	s.Weather.TurnsLeft--
	if s.Weather.TurnsLeft <= 0 {
		kind := s.Weather.Kind
		s.Weather = nil
		*log = append(*log, LogLine{Type: "weather", Side: -1, Text: weatherClearedText(kind)})
		return
	}
	if txt := weatherContinuesText(s.Weather.Kind); txt != "" {
		*log = append(*log, LogLine{Type: "weather", Side: -1, Text: txt})
	}
}
