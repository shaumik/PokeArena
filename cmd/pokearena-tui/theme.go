package main

import "github.com/charmbracelet/lipgloss"

// The TUI wears a Game Boy skin: the original DMG-01's four-shade green
// "dot-matrix" palette. Every styled surface is drawn from these four colours
// so the terminal reads like a 1996 handheld. lipgloss down-samples the
// truecolor hexes to 256/16-colour automatically on lesser terminals, so the
// look degrades gracefully rather than breaking.
//
// The same four shades feed the sprite quantiser (see sprite.go): a Crystal
// sprite's four luminance levels map one-to-one onto ink→screen, which is why
// the embedded sprites land back in this exact palette.

// palette is an ordered four-shade ramp, darkest (ink) to lightest (screen).
type palette struct {
	ink    lipgloss.Color // darkest — outlines, text
	dark   lipgloss.Color // shadow / muted
	light  lipgloss.Color // highlight
	screen lipgloss.Color // lightest — the LCD background
}

// dmg is the canonical Game Boy greenscale (#9bbc0f … #0f380f).
var dmg = palette{
	ink:    lipgloss.Color("#0f380f"),
	dark:   lipgloss.Color("#306230"),
	light:  lipgloss.Color("#8bac0f"),
	screen: lipgloss.Color("#9bbc0f"),
}

// pal is the active palette. Single theme for now; kept as a var so version
// tints (red/blue/yellow) can be slotted in behind a flag later without
// touching call sites.
var pal = dmg

// shades returns the ramp darkest→lightest for the sprite quantiser.
func (p palette) shades() [4]lipgloss.Color { return [4]lipgloss.Color{p.ink, p.dark, p.light, p.screen} }

// gbBorder frames the HP/stat boxes — a plain box in ink on the LCD.
var gbBorder = lipgloss.Border{
	Top: "─", Bottom: "─", Left: "│", Right: "│",
	TopLeft: "┌", TopRight: "┐", BottomLeft: "└", BottomRight: "┘",
}

// gbDialog is the chunky double-ruled frame of the Gen-1 text box.
var gbDialog = lipgloss.Border{
	Top: "═", Bottom: "═", Left: "║", Right: "║",
	TopLeft: "╔", TopRight: "╗", BottomLeft: "╚", BottomRight: "╝",
}

// Styles. Colour no longer carries meaning the way a modern UI's would — the
// DMG had four greys — so emphasis comes from weight and inverse video (the
// handheld's only way to "highlight"). The HP bar (see hpBar) is the one
// deliberate exception: it keeps the iconic green/amber/red because that cue is
// load-bearing for play.
var (
	// stScreen is the LCD itself: ink on the lightest green. Panels paint their
	// interior with it so sprites and text sit on a continuous green field.
	stScreen = lipgloss.NewStyle().Foreground(pal.ink).Background(pal.screen)

	stPanel = lipgloss.NewStyle().
		Border(gbBorder).
		BorderForeground(pal.ink).
		BorderBackground(pal.screen).
		Foreground(pal.ink).
		Background(pal.screen).
		Padding(0, 1)

	stDialog = lipgloss.NewStyle().
			Border(gbDialog).
			BorderForeground(pal.ink).
			BorderBackground(pal.screen).
			Foreground(pal.ink).
			Background(pal.screen).
			Padding(0, 1)

	stTitle  = lipgloss.NewStyle().Bold(true).Foreground(pal.ink).Background(pal.screen).Padding(0, 1)
	stDim    = lipgloss.NewStyle().Foreground(pal.dark)
	stYou    = lipgloss.NewStyle().Bold(true).Foreground(pal.ink)
	stOpp    = lipgloss.NewStyle().Bold(true).Foreground(pal.screen).Background(pal.ink) // inverse = "highlighted"
	stSys    = lipgloss.NewStyle().Foreground(pal.dark)
	stWarn   = lipgloss.NewStyle().Bold(true).Foreground(pal.screen).Background(pal.ink)
	stKey    = lipgloss.NewStyle().Bold(true).Foreground(pal.screen).Background(pal.ink) // menu-cursor chip
	stStatus = lipgloss.NewStyle().Foreground(pal.ink)
	stWin    = lipgloss.NewStyle().Bold(true).Foreground(pal.ink)
	stLose   = lipgloss.NewStyle().Bold(true).Foreground(pal.screen).Background(pal.ink)
)
