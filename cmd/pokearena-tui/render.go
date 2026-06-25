package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"pokearena/internal/domain"
	"pokearena/internal/engine"
)

// Styles. Kept deliberately small — colour carries meaning (HP, side tags),
// borders frame the three panels, everything else is plain text.
var (
	stPanel = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240")).
		Padding(0, 1)

	stTitle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("63"))
	stDim    = lipgloss.NewStyle().Foreground(lipgloss.Color("242"))
	stYou    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))  // blue
	stOpp    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("203")) // red
	stSys    = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	stWarn   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
	stKey    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("78"))
	stStatus = lipgloss.NewStyle().Foreground(lipgloss.Color("250"))
	stWin    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("82"))
	stLose   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196"))
)

// hpBar draws a width-cell bar for frac in [0,1], coloured green/yellow/red.
func hpBar(frac float64, width int) string {
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	filled := int(frac*float64(width) + 0.5)
	if filled > width {
		filled = width
	}
	colour := lipgloss.Color("82") // green
	switch {
	case frac <= 0.2:
		colour = lipgloss.Color("196") // red
	case frac <= 0.5:
		colour = lipgloss.Color("214") // amber
	}
	bar := lipgloss.NewStyle().Foreground(colour).Render(strings.Repeat("█", filled))
	rest := stDim.Render(strings.Repeat("░", width-filled))
	return bar + rest
}

// benchDots renders a side's bench as ● (alive) / ○ (fainted), excluding the
// active slot. n is the number to draw when only a count is known (foe).
func benchDots(team []engine.Pokemon, active int) string {
	var b strings.Builder
	for i, p := range team {
		if i == active {
			continue
		}
		if p.Fainted {
			b.WriteString(stDim.Render("○ "))
		} else {
			b.WriteString("● ")
		}
	}
	return strings.TrimRight(b.String(), " ")
}

// benchCount renders the foe bench as N alive dots followed by nothing else —
// the wire only tells us how many are unfainted, never which.
func benchCount(alive int) string {
	if alive <= 0 {
		return stDim.Render("none")
	}
	return strings.TrimRight(strings.Repeat("● ", alive), " ")
}

func typeLabel(t1, t2 domain.Type) string {
	if t2 == "" {
		return string(t1)
	}
	return string(t1) + "/" + string(t2)
}

func statusTag(s engine.StatusCond) string {
	if s == engine.StatusNone {
		return ""
	}
	abbr := map[engine.StatusCond]string{
		engine.StatusBurn:      "BRN",
		engine.StatusPoison:    "PSN",
		engine.StatusToxic:     "TOX",
		engine.StatusParalysis: "PAR",
		engine.StatusSleep:     "SLP",
		engine.StatusFreeze:    "FRZ",
	}[s]
	if abbr == "" {
		abbr = strings.ToUpper(string(s))
	}
	return " " + stWarn.Render("["+abbr+"]")
}

// boostTag renders non-zero stat stages: "+2 Atk -1 Spe".
func boostTag(st engine.Stages) string {
	parts := []string{}
	for _, s := range []struct {
		n     int
		label string
	}{
		{st.Atk, "Atk"}, {st.Def, "Def"}, {st.SpA, "SpA"}, {st.SpD, "SpD"},
		{st.Spe, "Spe"}, {st.Acc, "Acc"}, {st.Eva, "Eva"},
	} {
		if s.n != 0 {
			parts = append(parts, fmt.Sprintf("%+d %s", s.n, s.label))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return " " + stDim.Render(strings.Join(parts, " "))
}

// sideCondTag renders one side's screens, buffs and hazards — all public info.
func sideCondTag(c engine.SideConditions) string {
	parts := []string{}
	add := func(label string, turns int) { parts = append(parts, fmt.Sprintf("%s(%d)", label, turns)) }
	if c.Reflect != nil {
		add("Reflect", c.Reflect.TurnsLeft)
	}
	if c.LightScreen != nil {
		add("Light Screen", c.LightScreen.TurnsLeft)
	}
	if c.AuroraVeil != nil {
		add("Aurora Veil", c.AuroraVeil.TurnsLeft)
	}
	if c.Tailwind != nil {
		add("Tailwind", c.Tailwind.TurnsLeft)
	}
	if c.Safeguard != nil {
		add("Safeguard", c.Safeguard.TurnsLeft)
	}
	if c.Mist != nil {
		add("Mist", c.Mist.TurnsLeft)
	}
	if c.Hazards.StealthRock {
		parts = append(parts, "Stealth Rock")
	}
	if c.Hazards.Spikes > 0 {
		parts = append(parts, fmt.Sprintf("Spikes x%d", c.Hazards.Spikes))
	}
	if c.Hazards.ToxicSpikes > 0 {
		parts = append(parts, fmt.Sprintf("Toxic Spikes x%d", c.Hazards.ToxicSpikes))
	}
	return strings.Join(parts, " · ")
}

// fieldStrip renders the global field state — weather, terrain, rooms,
// Gravity — with turns remaining. Empty when the field is clear.
func fieldStrip(v *battleView) string {
	parts := []string{}
	if v.Weather != nil && v.Weather.Kind != engine.WeatherClear {
		parts = append(parts, fmt.Sprintf("%s(%d)", v.Weather.Kind, v.Weather.TurnsLeft))
	}
	if v.Terrain != nil && v.Terrain.Kind != engine.TerrainNone {
		parts = append(parts, fmt.Sprintf("%s terrain(%d)", v.Terrain.Kind, v.Terrain.TurnsLeft))
	}
	pw := v.PseudoWeather
	for _, t := range []struct {
		timer *engine.PWTimer
		label string
	}{
		{pw.TrickRoom, "Trick Room"}, {pw.WonderRoom, "Wonder Room"},
		{pw.MagicRoom, "Magic Room"}, {pw.Gravity, "Gravity"},
	} {
		if t.timer != nil {
			parts = append(parts, fmt.Sprintf("%s(%d)", t.label, t.timer.TurnsLeft))
		}
	}
	return strings.Join(parts, " · ")
}

// revealedMoves lists the foe moves seen so far, "2/4 revealed" style.
func revealedMoves(dex *domain.Dex, slots []foeMove) string {
	names := []string{}
	for _, ms := range slots {
		if ms.MoveID == "" {
			continue
		}
		if m, ok := dex.Moves[ms.MoveID]; ok {
			names = append(names, m.Name)
		} else {
			names = append(names, ms.MoveID)
		}
	}
	if len(slots) == 0 {
		return ""
	}
	if len(names) == 0 {
		return fmt.Sprintf("0/%d moves revealed", len(slots))
	}
	return fmt.Sprintf("%s (%d/%d revealed)", strings.Join(names, ", "), len(names), len(slots))
}

// logSideTag colours a log line's side attribution relative to us. A log
// line's Side is an absolute side index (0/1) or -1 for neutral system lines.
func logSideTag(side, meSide int) string {
	switch {
	case side < 0:
		return stSys.Render("SYS")
	case side == meSide:
		return stYou.Render("YOU")
	default:
		return stOpp.Render("OPP")
	}
}
