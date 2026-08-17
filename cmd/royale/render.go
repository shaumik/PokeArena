package main

import (
	"fmt"
	"sort"
	"strings"

	"pokearena/internal/ai"
	"pokearena/internal/domain"
	"pokearena/internal/engine"
)

func pct(hp, max int) int {
	if max <= 0 {
		return 0
	}
	return int(float64(hp)/float64(max)*100 + 0.5)
}

func typeLine(p engine.Pokemon) string {
	if p.Type2 != "" && p.Type2 != p.Type1 {
		return fmt.Sprintf("%s/%s", p.Type1, p.Type2)
	}
	return string(p.Type1)
}

func statusStr(p engine.Pokemon) string {
	if p.Status == engine.StatusNone {
		return "—"
	}
	s := string(p.Status)
	if p.Status == engine.StatusSleep && p.SleepTurns > 0 {
		s += fmt.Sprintf("(%d)", p.SleepTurns)
	}
	if p.Status == engine.StatusToxic && p.ToxicCounter > 0 {
		s += fmt.Sprintf("(x%d)", p.ToxicCounter)
	}
	return s
}

func stagesStr(st engine.Stages) string {
	var parts []string
	add := func(name string, v int) {
		if v != 0 {
			parts = append(parts, fmt.Sprintf("%s %+d", name, v))
		}
	}
	add("atk", st.Atk)
	add("def", st.Def)
	add("spa", st.SpA)
	add("spd", st.SpD)
	add("spe", st.Spe)
	add("acc", st.Acc)
	add("eva", st.Eva)
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ", ")
}

// volatilesStr surfaces the volatile state a player would see on screen —
// the things that change what a move is worth this turn.
func volatilesStr(v engine.Volatiles) string {
	var parts []string
	if v.Confusion != nil {
		parts = append(parts, "confused")
	}
	if v.Substitute != nil {
		parts = append(parts, fmt.Sprintf("substitute(%d hp)", v.Substitute.HP))
	}
	if v.LeechSeed != nil {
		parts = append(parts, "leech-seed")
	}
	if v.Trapped {
		parts = append(parts, "trapped")
	}
	if v.PartialTrap != nil {
		parts = append(parts, "partial-trap")
	}
	if v.MustRecharge {
		parts = append(parts, "must-recharge")
	}
	if v.Charging != nil {
		parts = append(parts, "charging")
	}
	if v.LockedMove != nil {
		parts = append(parts, "locked-move")
	}
	if v.Taunt != nil {
		parts = append(parts, "taunted")
	}
	if v.Encore != nil {
		parts = append(parts, "encored")
	}
	if v.Disable != nil {
		parts = append(parts, "disabled")
	}
	if v.PerishSong != nil {
		parts = append(parts, "perish-song")
	}
	if v.Ingrain {
		parts = append(parts, "ingrain")
	}
	if v.AquaRing {
		parts = append(parts, "aqua-ring")
	}
	if v.FlashFireCharged {
		parts = append(parts, "flash-fire-charged")
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, ", ")
}

func hazardsStr(h engine.Hazards) string {
	var parts []string
	if h.StealthRock {
		parts = append(parts, "stealth-rock")
	}
	if h.Spikes > 0 {
		parts = append(parts, fmt.Sprintf("spikes x%d", h.Spikes))
	}
	if h.ToxicSpikes > 0 {
		parts = append(parts, fmt.Sprintf("toxic-spikes x%d", h.ToxicSpikes))
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ", ")
}

func screensStr(c engine.SideConditions) string {
	var parts []string
	if c.Reflect != nil {
		parts = append(parts, fmt.Sprintf("reflect(%d)", c.Reflect.TurnsLeft))
	}
	if c.LightScreen != nil {
		parts = append(parts, fmt.Sprintf("light-screen(%d)", c.LightScreen.TurnsLeft))
	}
	if c.AuroraVeil != nil {
		parts = append(parts, fmt.Sprintf("aurora-veil(%d)", c.AuroraVeil.TurnsLeft))
	}
	if c.Tailwind != nil {
		parts = append(parts, fmt.Sprintf("tailwind(%d)", c.Tailwind.TurnsLeft))
	}
	if c.Safeguard != nil {
		parts = append(parts, fmt.Sprintf("safeguard(%d)", c.Safeguard.TurnsLeft))
	}
	if c.Mist != nil {
		parts = append(parts, fmt.Sprintf("mist(%d)", c.Mist.TurnsLeft))
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ", ")
}

func fieldStr(v ai.View) string {
	var parts []string
	if v.Weather != nil {
		parts = append(parts, fmt.Sprintf("weather: %s(%d)", v.Weather.Kind, v.Weather.TurnsLeft))
	} else {
		parts = append(parts, "weather: none")
	}
	if v.Terrain != nil {
		parts = append(parts, fmt.Sprintf("terrain: %s(%d)", v.Terrain.Kind, v.Terrain.TurnsLeft))
	} else {
		parts = append(parts, "terrain: none")
	}
	for _, pw := range []struct {
		name  string
		timer *engine.PWTimer
	}{
		{"trick-room", v.PseudoWeather.TrickRoom},
		{"wonder-room", v.PseudoWeather.WonderRoom},
		{"magic-room", v.PseudoWeather.MagicRoom},
		{"gravity", v.PseudoWeather.Gravity},
	} {
		if pw.timer != nil {
			parts = append(parts, fmt.Sprintf("%s(%d)", pw.name, pw.timer.TurnsLeft))
		}
	}
	return strings.Join(parts, " | ")
}

func moveLine(dex *domain.Dex, idx int, ms engine.MoveSlot) string {
	m, ok := dex.Moves[ms.MoveID]
	if !ok {
		return fmt.Sprintf("   [%d] %-16s (unknown move)", idx, ms.MoveID)
	}
	power := "—"
	if m.Power > 0 {
		power = fmt.Sprintf("%d", m.Power)
	}
	acc := "—"
	if m.Accuracy > 0 && m.Accuracy < 100 {
		acc = fmt.Sprintf("%d%%", m.Accuracy)
	} else if m.Accuracy >= 100 {
		acc = "100%"
	}
	prio := ""
	if m.Priority != 0 {
		prio = fmt.Sprintf(" prio%+d", m.Priority)
	}
	return fmt.Sprintf("   [%d] %-18s %-8s pow %-4s acc %-5s pp %d/%d%s",
		idx, m.Name, m.Category, power, acc, ms.PP, ms.MaxPP, prio)
}

// renderView is the whole of what a player agent is allowed to know. It is
// built strictly from ai.MakeView, so the fog-of-war guarantee the engine
// already enforces is the same one the agents play under.
func renderView(dex *domain.Dex, meta Meta, v ai.View, legal []engine.Action, mine, awaiting bool, s *engine.BattleState) string {
	var b strings.Builder
	me, foe := meta.Trainers[v.Me], meta.Trainers[1-v.Me]
	slot := "p1"
	if v.Me == 1 {
		slot = "p2"
	}

	fmt.Fprintf(&b, "══ POKÉARENA ROYALE · match %s (%s) · turn %d · phase %s ══\n",
		meta.ID, meta.Round, v.Turn, v.Phase)
	fmt.Fprintf(&b, "YOU: %s [%s] as %s   VS   %s [%s]\n", me.Name, me.Theme, slot, foe.Name, foe.Theme)
	fmt.Fprintf(&b, "FIELD: %s\n", fieldStr(v))
	fmt.Fprintf(&b, "YOUR SIDE: hazards %s | screens %s\n",
		hazardsStr(v.Self.Conditions.Hazards), screensStr(v.Self.Conditions))
	fmt.Fprintf(&b, "FOE SIDE:  hazards %s | screens %s\n",
		hazardsStr(v.FoeConditions.Hazards), screensStr(v.FoeConditions))

	act := v.Self.Team[v.Self.Active]
	fmt.Fprintf(&b, "\n── YOUR ACTIVE ──\n")
	fmt.Fprintf(&b, " %s (%s)  HP %d/%d (%d%%)  status %s\n",
		act.Name, typeLine(act), act.HP, act.MaxHP, pct(act.HP, act.MaxHP), statusStr(act))
	fmt.Fprintf(&b, " ability %s | item %s | stats atk %d def %d spa %d spd %d spe %d\n",
		orNone(string(act.Ability)), orNone(string(act.Item)),
		act.Stats.Atk, act.Stats.Def, act.Stats.SpA, act.Stats.SpD, act.Stats.Spe)
	fmt.Fprintf(&b, " boosts: %s\n", stagesStr(act.Stages))
	if vs := volatilesStr(act.Volatiles); vs != "" {
		fmt.Fprintf(&b, " volatiles: %s\n", vs)
	}
	fmt.Fprintf(&b, " moves:\n")
	for i, ms := range act.Moves {
		fmt.Fprintf(&b, "%s\n", moveLine(dex, i, ms))
	}

	fmt.Fprintf(&b, "\n── YOUR BENCH ──\n")
	for i := range v.Self.Team {
		if i == v.Self.Active {
			continue
		}
		p := v.Self.Team[i]
		state := fmt.Sprintf("HP %d/%d (%d%%) status %s", p.HP, p.MaxHP, pct(p.HP, p.MaxHP), statusStr(p))
		if p.Fainted {
			state = "FAINTED"
		}
		mv := make([]string, 0, len(p.Moves))
		for _, ms := range p.Moves {
			if m, ok := dex.Moves[ms.MoveID]; ok {
				mv = append(mv, fmt.Sprintf("%s(%d)", m.Name, ms.PP))
			}
		}
		fmt.Fprintf(&b, " [%d] %-12s %-11s %s\n     %s | item %s | %s\n",
			i, p.Name, typeLine(p), state, orNone(string(p.Ability)), orNone(string(p.Item)), strings.Join(mv, ", "))
	}

	fmt.Fprintf(&b, "\n── FOE ACTIVE ──\n")
	f := v.Foe
	fmt.Fprintf(&b, " %s (%s)  HP %d%%  status %s\n", f.Name, typeLine(f), pct(f.HP, f.MaxHP), statusStr(f))
	fmt.Fprintf(&b, " ability %s | item %s | boosts: %s\n",
		orUnknown(string(f.Ability)), orUnknown(string(f.Item)), stagesStr(f.Stages))
	if vs := volatilesStr(f.Volatiles); vs != "" {
		fmt.Fprintf(&b, " volatiles: %s\n", vs)
	}
	var revealed []string
	for _, ms := range f.Moves {
		if ms.MoveID == "" {
			continue
		}
		if m, ok := dex.Moves[ms.MoveID]; ok {
			revealed = append(revealed, m.Name)
		} else {
			revealed = append(revealed, ms.MoveID)
		}
	}
	if len(revealed) == 0 {
		fmt.Fprintf(&b, " revealed moves: (none yet)\n")
	} else {
		fmt.Fprintf(&b, " revealed moves: %s\n", strings.Join(revealed, ", "))
	}
	fmt.Fprintf(&b, " foe bench still alive: %d (species hidden until sent out)\n", v.FoeBenchAlive)

	fmt.Fprintf(&b, "\n── LEGAL ACTIONS ──\n")
	if !mine {
		fmt.Fprintf(&b, " (none — you do not owe an action right now)\n")
	}
	for _, a := range legal {
		switch a.Kind {
		case engine.ActionMove:
			if a.Index == engine.StruggleMoveIndex {
				fmt.Fprintf(&b, "  move:%d   Struggle (no usable moves)\n", a.Index)
				continue
			}
			name := act.Moves[a.Index].MoveID
			if m, ok := dex.Moves[name]; ok {
				name = m.Name
			}
			fmt.Fprintf(&b, "  move:%-4d %s\n", a.Index, name)
		case engine.ActionSwitch:
			p := v.Self.Team[a.Index]
			fmt.Fprintf(&b, "  switch:%-2d %s (HP %d%%)\n", a.Index, p.Name, pct(p.HP, p.MaxHP))
		}
	}

	fmt.Fprintf(&b, "\nAWAITING YOUR ACTION: %v\n", awaiting)
	if s.Ended() {
		fmt.Fprintf(&b, "BATTLE OVER. %s\n", winnerLine(meta, s))
	}
	return b.String()
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

func orUnknown(s string) string {
	if s == "" {
		return "(unknown)"
	}
	return s
}

func winnerLine(meta Meta, s *engine.BattleState) string {
	switch s.Winner {
	case 0, 1:
		return fmt.Sprintf("WINNER: %s (%s)", meta.Trainers[s.Winner].Name, meta.Trainers[s.Winner].Theme)
	case 2:
		return "RESULT: draw"
	default:
		return "RESULT: unresolved"
	}
}

// snapshot freezes the board for the replay log. Unlike the player view this
// is complete: the judge and the post-tournament report are entitled to see
// everything, players only ever read renderView.
func snapshot(s *engine.BattleState) Snapshot {
	snap := Snapshot{Turn: s.Turn, Phase: string(s.Phase)}
	if s.Weather != nil {
		snap.Weather = fmt.Sprintf("%s(%d)", s.Weather.Kind, s.Weather.TurnsLeft)
	}
	if s.Terrain != nil {
		snap.Terrain = fmt.Sprintf("%s(%d)", s.Terrain.Kind, s.Terrain.TurnsLeft)
	}
	for i := range s.Sides {
		sd := s.Sides[i]
		ss := SideSnap{
			Trainer: sd.Trainer,
			Hazards: hazardsStr(sd.Conditions.Hazards),
			Screens: screensStr(sd.Conditions),
		}
		for j := range sd.Team {
			p := sd.Team[j]
			ss.Team = append(ss.Team, MonSnap{
				Name:    p.Name,
				HP:      p.HP,
				MaxHP:   p.MaxHP,
				Status:  string(p.Status),
				Fainted: p.Fainted,
				Active:  j == sd.Active,
			})
		}
		snap.Sides[i] = ss
	}
	return snap
}

// teamSummary prints a roster the way a trainer would brief on it — used by
// the `team` command so an agent starts the match knowing what it brought.
func teamSummary(dex *domain.Dex, t Trainer) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s — theme: %s\n", t.Name, t.Theme)
	for i, p := range t.Picks {
		sp := dex.Species[p.DexNo]
		var mv []string
		for _, id := range p.MoveIDs {
			if m, ok := dex.Moves[id]; ok {
				mv = append(mv, m.Name)
			} else {
				mv = append(mv, id+"(?)")
			}
		}
		evs := ""
		if p.EVs != nil {
			var parts []string
			for _, k := range domain.StatKeys {
				if v, _ := p.EVs.Get(k); v > 0 {
					parts = append(parts, fmt.Sprintf("%d %s", v, k))
				}
			}
			sort.Strings(parts)
			evs = strings.Join(parts, "/")
		}
		fmt.Fprintf(&b, " [%d] %-12s %-14s ability %-16s item %-16s %s\n      %s | EVs %s\n",
			i, sp.Name, typeOf(sp), orNone(p.Ability), orNone(p.Item), p.Nature,
			strings.Join(mv, ", "), orNone(evs))
	}
	return b.String()
}

func typeOf(sp domain.Species) string {
	if sp.Type2 != "" && sp.Type2 != sp.Type1 {
		return fmt.Sprintf("%s/%s", sp.Type1, sp.Type2)
	}
	return string(sp.Type1)
}
