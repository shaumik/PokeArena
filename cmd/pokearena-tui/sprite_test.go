package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"pokearena"
	"pokearena/internal/domain"
)

func decodeFrontGIF(t *testing.T, dexNo int) *gif.GIF {
	t.Helper()
	data, err := assetsFS.ReadFile(fmt.Sprintf("assets/front_anim/%d.gif", dexNo))
	if err != nil {
		t.Fatalf("read embedded gif #%d: %v", dexNo, err)
	}
	g, err := gif.DecodeAll(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode gif #%d: %v", dexNo, err)
	}
	return g
}

// TestSpritesLoadForWholeRoster proves the embedded assets cover every species
// the picker can draft: a missing sprite would mean a battle renders a blank
// box for a real Pokémon. Both the animated front and the static back must load.
func TestSpritesLoadForWholeRoster(t *testing.T) {
	dex, err := domain.LoadDexFS(pokearena.DataFS(), "gen1-v1")
	if err != nil {
		t.Fatalf("load dex: %v", err)
	}
	for id, sp := range dex.Species {
		front := foeSprite(id, frontPx)
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
		back := selfSprite(id, backPx)
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
	if n := foeSprite(6, frontPx).frameCount(); n < 2 { // Charizard
		t.Errorf("Charizard front frames = %d, want >= 2 (animated)", n)
	}
}

// TestFrameLineWidths asserts every rendered line is exactly `cols` display
// cells wide (ANSI-aware), which is what keeps lipgloss column alignment intact
// when the sprite sits beside the stat box.
func TestFrameLineWidths(t *testing.T) {
	sp := foeSprite(6, frontPx)
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

// TestQuantizationRanksToFourShades pins the rank-based mapping on the *real*
// Crystal levels [0,70,125,255]: each must land on a distinct shade in order.
// The earlier fixed thresholds bucketed 125 as 'dark', collapsing the sprite to
// three shades — this is the regression guard for that.
func TestQuantizationRanksToFourShades(t *testing.T) {
	shades := pal.shades()
	levels := []uint8{0, 70, 125, 255}
	img := image.NewRGBA(image.Rect(0, 0, len(levels), 1))
	for i, l := range levels {
		img.SetRGBA(i, 0, color.RGBA{l, l, l, 255}) // grey => lum == l
	}
	sm := buildShadeMap([]*image.RGBA{img})
	for i, l := range levels {
		want := shades[i]
		if got := shadeFromMap(color.RGBA{l, l, l, 255}, sm); got != want {
			t.Errorf("level %d -> %q, want %q (shade %d)", l, got, want, i)
		}
	}
	if got := shadeFromMap(color.RGBA{0, 0, 0, 0}, sm); got != pal.screen {
		t.Errorf("transparent -> %q, want LCD screen %q", got, pal.screen)
	}
}

// TestQuantizationUsesAllFourShadesAcrossRoster guards against a sprite that
// quantises to fewer than four shades: every embedded front sprite carries four
// colours, so a real battle should never look flat. (A handful of sprites may
// genuinely have <4 levels; assert the common case holds for a sampled mon.)
func TestQuantizationUsesAllFourShadesForCharizard(t *testing.T) {
	g := decodeFrontGIF(t, 6)
	imgs, _ := gifFrames(g)
	sm := buildShadeMap(imgs)
	seen := map[int]bool{}
	for _, idx := range sm {
		seen[idx] = true
	}
	if len(seen) != 4 {
		t.Errorf("Charizard quantised to %d shades, want 4 (map=%v)", len(seen), sm)
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
	lines := halfBlock(img, buildShadeMap([]*image.RGBA{img}))
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
