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
		dmg = p.MaxHP * p.ToxicCounter / 16
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
	p.HP -= dmg
	*log = append(*log, LogLine{
		Type: "status", Side: side,
		Text: fmt.Sprintf("%s is hurt by its %s! (-%d)", p.Name, p.Status, dmg),
	})
	if p.HP <= 0 {
		faint(p, side, log)
	}
}

// applyPartialTrapResidual chips 1/8 max HP and ticks the trap counter.
// The volatile clears when the counter reaches zero (or the holder faints).
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
		p.HP -= dmg
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

// applyWeatherResidual applies sandstorm chip damage to any active Pokémon
// that isn't Rock / Ground / Steel. Snow / Rain / Sun never chip; clear
// weather is a no-op. Faints fire here if the chip is lethal.
func applyWeatherResidual(s *BattleState, log *[]LogLine) {
	w := effectiveWeather(s) // honors Cloud Nine on either active
	if w == nil {
		return
	}
	for i := 0; i < 2; i++ {
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
		p.HP -= dmg
		*log = append(*log, LogLine{
			Type: "weather", Side: i,
			Text: fmt.Sprintf("%s is buffeted by the sandstorm! (-%d)", p.Name, dmg),
		})
		if p.HP <= 0 {
			faint(p, i, log)
		}
	}
}

// applyTerrainResidual fires Grassy Terrain's 1/16 max-HP end-of-turn heal
// on every grounded active. Other terrains don't have residual effects, so
// this is a no-op for them. Heals are not indirect damage — Magic Guard is
// irrelevant here.
func applyTerrainResidual(s *BattleState, log *[]LogLine) {
	t := s.Terrain
	if t == nil {
		return
	}
	for i := 0; i < 2; i++ {
		p := s.Active(i)
		if p.Fainted {
			continue
		}
		amt := terrainGrassyHeal(t, p)
		if amt == 0 {
			continue
		}
		if p.HP >= p.MaxHP {
			continue
		}
		before := p.HP
		p.HP += amt
		if p.HP > p.MaxHP {
			p.HP = p.MaxHP
		}
		*log = append(*log, LogLine{
			Type: "terrain", Side: i,
			Text: fmt.Sprintf("%s is healed by the Grassy Terrain! (+%d)", p.Name, p.HP-before),
		})
	}
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
