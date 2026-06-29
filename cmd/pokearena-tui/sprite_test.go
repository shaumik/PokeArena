package main

import (
	"image"
	"image/color"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"pokearena"
	"pokearena/internal/domain"
)

// TestSpritesLoadForWholeRoster proves the embedded assets cover every species
// the picker can draft: a missing sprite would mean a battle renders a blank
// box for a real Pokémon. Both the animated front and the static back must load.
func TestSpritesLoadForWholeRoster(t *testing.T) {
	dex, err := domain.LoadDexFS(pokearena.DataFS(), "gen1-v1")
	if err != nil {
		t.Fatalf("load dex: %v", err)
	}
	for id, sp := range dex.Species {
		front := foeSprite(id)
		if front == nil {
			t.Errorf("#%d %s: no animated front sprite", id, sp.Name)
			continue
		}
		if front.cols != frontPx || front.rows != frontPx/2 {
			t.Errorf("#%d front dims = %dx%d, want %dx%d", id, front.cols, front.rows, frontPx, frontPx/2)
		}
		if front.frameCount() < 1 {
			t.Errorf("#%d front has no frames", id)
		}
		back := selfSprite(id)
		if back == nil {
			t.Errorf("#%d %s: no back sprite", id, sp.Name)
			continue
		}
		if back.cols != backPx || back.rows != backPx/2 {
			t.Errorf("#%d back dims = %dx%d, want %dx%d", id, back.cols, back.rows, backPx, backPx/2)
		}
	}
}

// TestFrontSpriteIsAnimated checks a known mon really has multiple frames, so
// the animation tier has something to cycle (not a static GIF).
func TestFrontSpriteIsAnimated(t *testing.T) {
	if n := foeSprite(6).frameCount(); n < 2 { // Charizard
		t.Errorf("Charizard front frames = %d, want >= 2 (animated)", n)
	}
}

// TestFrameLineWidths asserts every rendered line is exactly `cols` display
// cells wide (ANSI-aware), which is what keeps lipgloss column alignment intact
// when the sprite sits beside the stat box.
func TestFrameLineWidths(t *testing.T) {
	sp := foeSprite(6)
	if sp == nil {
		t.Fatal("no sprite")
	}
	lines := sp.frame(0)
	if len(lines) != sp.rows {
		t.Fatalf("frame line count = %d, want %d", len(lines), sp.rows)
	}
	for i, ln := range lines {
		if w := lipgloss.Width(ln); w != sp.cols {
			t.Errorf("line %d width = %d, want %d", i, w, sp.cols)
		}
	}
}

// TestShadeOfQuantization pins the luminance->palette mapping: transparent
// becomes the LCD background, and opaque pixels bucket into the four greens
// darkest->lightest. Only palette colours may ever be emitted.
func TestShadeOfQuantization(t *testing.T) {
	shades := pal.shades()
	cases := []struct {
		name string
		in   color.RGBA
		want lipgloss.Color
	}{
		{"transparent", color.RGBA{0, 0, 0, 0}, pal.screen},
		{"black", color.RGBA{0, 0, 0, 255}, shades[0]},
		{"dark", color.RGBA{70, 70, 70, 255}, shades[1]},
		{"light", color.RGBA{150, 150, 150, 255}, shades[2]},
		{"white", color.RGBA{255, 255, 255, 255}, shades[3]},
	}
	for _, c := range cases {
		if got := shadeOf(c.in); got != c.want {
			t.Errorf("%s: shadeOf = %q, want %q", c.name, got, c.want)
		}
	}
}

// TestHalfBlockDimensions: a hand-built image renders to rows = ceil(h/2) lines,
// each `w` cells wide.
func TestHalfBlockDimensions(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 6, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 6; x++ {
			img.SetRGBA(x, y, color.RGBA{0, 0, 0, 255})
		}
	}
	lines := halfBlock(img)
	if len(lines) != 2 {
		t.Fatalf("line count = %d, want 2", len(lines))
	}
	for i, ln := range lines {
		if w := lipgloss.Width(ln); w != 6 {
			t.Errorf("line %d width = %d, want 6", i, w)
		}
	}
}

// TestDexNoByName resolves the foe's public species name back to its dex number
// (how the foe sprite is found, since the foe is name-only on the wire).
func TestDexNoByName(t *testing.T) {
	dex, err := domain.LoadDexFS(pokearena.DataFS(), "gen1-v1")
	if err != nil {
		t.Fatalf("load dex: %v", err)
	}
	want := 6
	name := dex.Species[want].Name
	got, ok := dexNoByName(dex, name)
	if !ok || got != want {
		t.Errorf("dexNoByName(%q) = %d,%v; want %d,true", name, got, ok, want)
	}
	if _, ok := dexNoByName(dex, "Missingno"); ok {
		t.Error("dexNoByName matched a non-existent species")
	}
}
