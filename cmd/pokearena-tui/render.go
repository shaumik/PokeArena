package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"pokearena/internal/domain"
	"pokearena/internal/engine"
)

// Styles live in theme.go (the Game Boy palette). This file holds the drawing
// helpers that compose them.

// g renders a plain literal on the LCD field (screen background). lipgloss emits
// a hard reset after every styled span, so any separator or interstitial text
// left unstyled would fall back to the terminal's default background — a dark
// sliver in the green field. Routing such text through stScreen keeps the field
// continuous.
func g(s string) string { return stScreen.Render(s) }

// greenRow joins blocks left-to-right on a continuous green field: each is first
// padded to the row's height with the LCD background, so lipgloss never inserts
// unstyled fill between columns of unequal height.
func greenRow(align lipgloss.Position, blocks ...string) string {
	h := 0
	for _, b := range blocks {
		if n := lipgloss.Height(b); n > h {
			h = n
		}
	}
	padded := make([]string, len(blocks))
	for i, b := range blocks {
		padded[i] = lipgloss.NewStyle().Background(pal.screen).Height(h).Render(b)
	}
	return lipgloss.JoinHorizontal(align, padded...)
}

// greenStack joins blocks top-to-bottom on a continuous green field, padding each
// to the widest block's width with the LCD background so JoinVertical's right
// fill is green, not the terminal default.
func greenStack(blocks ...string) string {
	w := 0
	for _, b := range blocks {
		if n := lipgloss.Width(b); n > w {
			w = n
		}
	}
	padded := make([]string, len(blocks))
	for i, b := range blocks {
		padded[i] = lipgloss.NewStyle().Background(pal.screen).Width(w).Render(b)
	}
	return lipgloss.JoinVertical(lipgloss.Left, padded...)
}

// lcd fills the whole terminal with the LCD background so the green field reaches
// the screen edges. With unknown dimensions (pre-WindowSizeMsg) it is a no-op.
func lcd(content string, w, h int) string {
	if w <= 0 || h <= 0 {
		return content
	}
	return lipgloss.NewStyle().Background(pal.screen).Width(w).Height(h).Render(content)
}

// hpBar draws a width-cell bar for frac in [0,1]. It keeps the iconic Gen-1
// green/amber/red ramp — the one place a literal color out-earns palette
// purity, since "how close am I to fainting" is the most time-critical read in
// the game — toned toward the GB's muted hues and seated on the LCD green so it
// blends into the stat box.
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
	color := lipgloss.Color("#5fa800") // green
	switch {
	case frac <= 0.2:
		color = lipgloss.Color("#c43a1a") // red
	case frac <= 0.5:
		color = lipgloss.Color("#d2a400") // amber
	}
	bar := lipgloss.NewStyle().Foreground(color).Background(pal.screen).Render(strings.Repeat("█", filled))
	rest := lipgloss.NewStyle().Foreground(pal.dark).Background(pal.screen).Render(strings.Repeat("░", width-filled))
	return bar + rest
}

// benchDots renders a side's bench as ● (alive) / ○ (fainted), excluding the
// active slot. It returns plain glyphs; the caller styles the whole run (so the
// background stays continuous — no per-glyph reset).
func benchDots(team []engine.Pokemon, active int) string {
	var b strings.Builder
	for i, p := range team {
		if i == active {
			continue
		}
		if p.Fainted {
			b.WriteString("○ ")
		} else {
			b.WriteString("● ")
		}
	}
	return strings.TrimRight(b.String(), " ")
}

// benchCount renders the foe bench as N alive dots — the wire only tells us how
// many are unfainted, never which. Plain glyphs; the caller styles the run.
func benchCount(alive int) string {
	if alive <= 0 {
		return "none"
	}
	return strings.TrimRight(strings.Repeat("● ", alive), " ")
}

// typeAbbr is the Gen-1 style three-letter type tag (ELE, PSY, …) for the
// move menu's fixed columns, where full names ("electric") would blow the
// grid out. First-three is unambiguous across the type chart (GRA/GRO,
// FIR/FIG, DAR/DRA all stay distinct).
func typeAbbr(t domain.Type) string {
	s := strings.ToUpper(string(t))
	if len(s) > 3 {
		s = s[:3]
	}
	return s
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
	return g(" ") + stWarn.Render("["+abbr+"]")
}

// boostTag renders non-zero stat stages: "+2 Atk -1 Spe".
func boostTag(st engine.Stages) string {
	parts := []string{}
	for _, s := range []struct {
		n     int
		label string
	}{
		{st.Atk, "Atk"},
		{st.Def, "Def"},
		{st.SpA, "SpA"},
		{st.SpD, "SpD"},
		{st.Spe, "Spe"},
		{st.Acc, "Acc"},
		{st.Eva, "Eva"},
	} {
		if s.n != 0 {
			parts = append(parts, fmt.Sprintf("%+d %s", s.n, s.label))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return g(" ") + stDim.Render(strings.Join(parts, " "))
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
		{pw.TrickRoom, "Trick Room"},
		{pw.WonderRoom, "Wonder Room"},
		{pw.MagicRoom, "Magic Room"},
		{pw.Gravity, "Gravity"},
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

// logSideTag colors a log line's side attribution relative to us. A log
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
