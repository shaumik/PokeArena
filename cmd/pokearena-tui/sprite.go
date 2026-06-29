package main

import (
	"bytes"
	"embed"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	_ "image/png" // register PNG decoder for the back sprites
	"strings"
	"sync"

	"github.com/charmbracelet/lipgloss"

	"pokearena/internal/domain"
)

// Game Boy sprites, embedded into the binary so the TUI has no runtime asset
// dependency. Two sources, both quantised into the active four-shade palette:
//
//	front_anim/<dex>.gif  Gen-2 Crystal animated front (the idle wiggle) — the
//	                      original RBY sprites never animated, so the motion
//	                      comes from Crystal and is recoloured to DMG green.
//	back/<dex>.png        Gen-1 RB static back (the player-side view; Crystal
//	                      has no animated backs).
//
// Sprites are drawn with the upper-half-block trick: one cell stacks two
// vertical pixels (foreground = top pixel, background = bottom), so a terminal
// cell's ~2:1 aspect renders the square sprite ~square. Each sprite's four
// luminance levels map one-to-one onto ink→screen, which is why a recoloured
// Crystal sprite still reads as authentic Game Boy.

//go:embed assets
var assetsFS embed.FS

// Target pixel sizes (square). Front is the foe you stare at; back is your own,
// smaller in the original games too. Both even so half-block pairs divide clean.
const (
	frontPx = 40
	backPx  = 32
)

// sprite is a decoded, pre-rendered sprite: each frame is a slice of ANSI lines
// ready to print. Static sprites have exactly one frame.
type sprite struct {
	frames []frameLines
	cols   int // display width in cells
	rows   int // display height in cells
}

type frameLines struct {
	lines   []string
	delayMs int // inter-frame delay for animation (0 for static)
}

// frame returns frame i's lines, wrapping the index so an animation loop can
// advance without bounds checks. Empty sprite yields nil.
func (s *sprite) frame(i int) []string {
	if s == nil || len(s.frames) == 0 {
		return nil
	}
	return s.frames[i%len(s.frames)].lines
}

func (s *sprite) frameCount() int {
	if s == nil {
		return 0
	}
	return len(s.frames)
}

// delayMs returns frame i's display duration in milliseconds, clamped so a
// pathological 0-delay GIF can't spin the render loop.
func (s *sprite) delayMs(i int) int {
	if s == nil || len(s.frames) == 0 {
		return 100
	}
	d := s.frames[i%len(s.frames)].delayMs
	if d < 20 {
		d = 100
	}
	return d
}

var (
	spriteMu    sync.Mutex
	spriteCache = map[string]*sprite{}
)

// foeSprite returns the animated front sprite for a dex number, or nil if it
// can't be loaded (the caller draws a placeholder).
func foeSprite(dexNo int) *sprite { return cachedSprite(fmt.Sprintf("f%d", dexNo), dexNo, loadFront) }

// selfSprite returns the static back sprite for a dex number, or nil.
func selfSprite(dexNo int) *sprite { return cachedSprite(fmt.Sprintf("b%d", dexNo), dexNo, loadBack) }

func cachedSprite(key string, dexNo int, load func(int) (*sprite, error)) *sprite {
	spriteMu.Lock()
	defer spriteMu.Unlock()
	if sp, ok := spriteCache[key]; ok {
		return sp // cached, including a nil miss
	}
	sp, err := load(dexNo)
	if err != nil {
		sp = nil
	}
	spriteCache[key] = sp
	return sp
}

func loadFront(dexNo int) (*sprite, error) {
	data, err := assetsFS.ReadFile(fmt.Sprintf("assets/front_anim/%d.gif", dexNo))
	if err != nil {
		return nil, err
	}
	g, err := gif.DecodeAll(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	imgs, delays := gifFrames(g)
	sp := &sprite{cols: frontPx, rows: frontPx / 2}
	for i, im := range imgs {
		scaled := scaleNearest(im, frontPx, frontPx)
		sp.frames = append(sp.frames, frameLines{lines: halfBlock(scaled), delayMs: delays[i]})
	}
	if len(sp.frames) == 0 {
		return nil, fmt.Errorf("no frames in #%d", dexNo)
	}
	return sp, nil
}

func loadBack(dexNo int) (*sprite, error) {
	data, err := assetsFS.ReadFile(fmt.Sprintf("assets/back/%d.png", dexNo))
	if err != nil {
		return nil, err
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	scaled := scaleNearest(toRGBA(src), backPx, backPx)
	return &sprite{
		frames: []frameLines{{lines: halfBlock(scaled)}},
		cols:   backPx,
		rows:   backPx / 2,
	}, nil
}

// gifFrames composites a GIF's (possibly partial, disposal-driven) frames onto
// a persistent canvas and returns one full RGBA snapshot per frame, with each
// frame's delay in milliseconds. draw.Over handles the transparent palette
// index, so transparency survives into the snapshots.
func gifFrames(g *gif.GIF) ([]*image.RGBA, []int) {
	w, h := g.Config.Width, g.Config.Height
	if w == 0 || h == 0 {
		b := g.Image[0].Bounds()
		w, h = b.Dx(), b.Dy()
	}
	canvas := image.NewRGBA(image.Rect(0, 0, w, h))
	var out []*image.RGBA
	var delays []int
	for i, pf := range g.Image {
		var backup *image.RGBA
		if i < len(g.Disposal) && g.Disposal[i] == gif.DisposalPrevious {
			backup = cloneRGBA(canvas)
		}
		draw.Draw(canvas, pf.Bounds(), pf, pf.Bounds().Min, draw.Over)
		out = append(out, cloneRGBA(canvas))
		d := 100
		if i < len(g.Delay) {
			d = g.Delay[i] * 10 // centiseconds -> ms
		}
		delays = append(delays, d)
		if i < len(g.Disposal) {
			switch g.Disposal[i] {
			case gif.DisposalBackground:
				draw.Draw(canvas, pf.Bounds(), image.Transparent, image.Point{}, draw.Src)
			case gif.DisposalPrevious:
				if backup != nil {
					draw.Draw(canvas, canvas.Bounds(), backup, canvas.Bounds().Min, draw.Src)
				}
			}
		}
	}
	return out, delays
}

func cloneRGBA(src *image.RGBA) *image.RGBA {
	dst := image.NewRGBA(src.Bounds())
	copy(dst.Pix, src.Pix)
	return dst
}

func toRGBA(src image.Image) *image.RGBA {
	if r, ok := src.(*image.RGBA); ok {
		return r
	}
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Src)
	return dst
}

// scaleNearest box-samples src down (or up) to tw x th with nearest-neighbour,
// preserving alpha so transparent regions stay transparent.
func scaleNearest(src *image.RGBA, tw, th int) *image.RGBA {
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, tw, th))
	if sw == 0 || sh == 0 {
		return dst
	}
	for y := 0; y < th; y++ {
		sy := y * sh / th
		for x := 0; x < tw; x++ {
			sx := x * sw / tw
			dst.SetRGBA(x, y, src.RGBAAt(sb.Min.X+sx, sb.Min.Y+sy))
		}
	}
	return dst
}

// halfBlock renders an RGBA image as upper-half-block (▀) cells: foreground is
// the top pixel's shade, background the bottom pixel's. Runs of identical
// (fg,bg) are coalesced into one styled span to keep the ANSI compact enough to
// repaint every animation frame.
func halfBlock(img *image.RGBA) []string {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	lines := make([]string, 0, (h+1)/2)
	for y := 0; y < h; y += 2 {
		var sb strings.Builder
		var runFg, runBg lipgloss.Color
		runN := 0
		flush := func() {
			if runN == 0 {
				return
			}
			sb.WriteString(lipgloss.NewStyle().Foreground(runFg).Background(runBg).Render(strings.Repeat("▀", runN)))
			runN = 0
		}
		for x := 0; x < w; x++ {
			fg := shadeOf(img.RGBAAt(b.Min.X+x, b.Min.Y+y))
			bg := pal.screen
			if y+1 < h {
				bg = shadeOf(img.RGBAAt(b.Min.X+x, b.Min.Y+y+1))
			}
			if runN > 0 && fg == runFg && bg == runBg {
				runN++
				continue
			}
			flush()
			runFg, runBg, runN = fg, bg, 1
		}
		flush()
		lines = append(lines, sb.String())
	}
	return lines
}

// shadeOf quantises one pixel to the active palette: transparent pixels become
// the LCD background (so the sprite sits on a continuous green field), opaque
// pixels bucket by luminance into the four greens, darkest→lightest.
func shadeOf(c color.RGBA) lipgloss.Color {
	if c.A < 128 {
		return pal.screen
	}
	l := (299*int(c.R) + 587*int(c.G) + 114*int(c.B)) / 1000
	shades := pal.shades() // [ink, dark, light, screen]
	switch {
	case l < 64:
		return shades[0]
	case l < 128:
		return shades[1]
	case l < 192:
		return shades[2]
	default:
		return shades[3]
	}
}

// dexNoByName resolves a species name to its national dex number using the
// loaded dex. The foe arrives name-only on the wire (species is public info),
// so this is how the foe sprite is found. Names are unique in the gen-1 dex.
func dexNoByName(dex *domain.Dex, name string) (int, bool) {
	if dex == nil {
		return 0, false
	}
	for id, sp := range dex.Species {
		if sp.Name == name {
			return id, true
		}
	}
	return 0, false
}
