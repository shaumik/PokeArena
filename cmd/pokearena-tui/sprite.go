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
	"sort"
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

// Default/native pixel sizes (square). Front is the foe you stare at; back is
// your own, smaller in the original games too. These are the fallbacks and the
// upper bound — the live size is chosen per terminal (see model.spriteSizes) and
// passed to foeSprite/selfSprite, which cache one rendering per (species, size).
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

// delayMs returns frame i's display duration in milliseconds. A genuinely
// non-positive delay (which would spin the render loop) is replaced with a
// sensible default; legitimate fast frames are only floored, not rewritten —
// many real Crystal sprites have a 10ms frame, and replacing it with 100ms
// (rather than flooring to 20ms) produced a visible 10x hitch in their loops.
func (s *sprite) delayMs(i int) int {
	if s == nil || len(s.frames) == 0 {
		return 100
	}
	d := s.frames[i%len(s.frames)].delayMs
	switch {
	case d <= 0:
		d = 100
	case d < 20:
		d = 20
	}
	return d
}

var (
	spriteMu    sync.Mutex
	spriteCache = map[string]*sprite{}
)

// foeSprite returns the animated front sprite at the given pixel size, or nil if
// it can't be loaded (the caller draws a placeholder).
func foeSprite(dexNo, px int) *sprite {
	return cachedSprite(fmt.Sprintf("f%d@%d", dexNo, px), dexNo, px, loadFront)
}

// selfSprite returns the static back sprite at the given pixel size, or nil.
func selfSprite(dexNo, px int) *sprite {
	return cachedSprite(fmt.Sprintf("b%d@%d", dexNo, px), dexNo, px, loadBack)
}

func cachedSprite(key string, dexNo, px int, load func(int, int) (*sprite, error)) *sprite {
	spriteMu.Lock()
	defer spriteMu.Unlock()
	if sp, ok := spriteCache[key]; ok {
		return sp // cached, including a nil miss
	}
	sp, err := load(dexNo, px)
	if err != nil {
		sp = nil
	}
	spriteCache[key] = sp
	return sp
}

func loadFront(dexNo, px int) (*sprite, error) {
	data, err := assetsFS.ReadFile(fmt.Sprintf("assets/front_anim/%d.gif", dexNo))
	if err != nil {
		return nil, err
	}
	g, err := gif.DecodeAll(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	imgs, delays := gifFrames(g)
	sm := buildShadeMap(imgs) // one map for the whole animation, from full-res frames
	sp := &sprite{cols: px, rows: px / 2}
	for i, im := range imgs {
		scaled := scaleNearest(im, px, px)
		sp.frames = append(sp.frames, frameLines{lines: halfBlock(scaled, sm), delayMs: delays[i]})
	}
	if len(sp.frames) == 0 {
		return nil, fmt.Errorf("no frames in #%d", dexNo)
	}
	return sp, nil
}

func loadBack(dexNo, px int) (*sprite, error) {
	data, err := assetsFS.ReadFile(fmt.Sprintf("assets/back/%d.png", dexNo))
	if err != nil {
		return nil, err
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	rgba := toRGBA(src)
	sm := buildShadeMap([]*image.RGBA{rgba})
	scaled := scaleNearest(rgba, px, px)
	return &sprite{
		frames: []frameLines{{lines: halfBlock(scaled, sm)}},
		cols:   px,
		rows:   px / 2,
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
// the top pixel's shade, background the bottom pixel's, quantised through the
// sprite's own shade map. Runs of identical (fg,bg) are coalesced into one
// styled span to keep the ANSI compact enough to repaint every animation frame.
func halfBlock(img *image.RGBA, sm map[int]int) []string {
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
			fg := shadeFromMap(img.RGBAAt(b.Min.X+x, b.Min.Y+y), sm)
			bg := pal.screen
			if y+1 < h {
				bg = shadeFromMap(img.RGBAAt(b.Min.X+x, b.Min.Y+y+1), sm)
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

// lum is the Rec.601 luma of an 8-bit colour.
func lum(c color.RGBA) int { return (299*int(c.R) + 587*int(c.G) + 114*int(c.B)) / 1000 }

// buildShadeMap maps every distinct opaque luminance across the given frames to
// a palette shade *by rank*: darkest level -> ink (0), lightest -> screen (3),
// the rest spread between. Game Boy sprites carry exactly four colours, so a
// 4-level sprite lands cleanly on the four greens whatever its actual luminance
// values are — fixed thresholds misbucket sprites whose levels sit near a
// boundary (e.g. #12's 194 vs #6's 125). Built over all frames so an animation
// quantises consistently frame to frame.
func buildShadeMap(imgs []*image.RGBA) map[int]int {
	set := map[int]struct{}{}
	for _, im := range imgs {
		b := im.Bounds()
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				if c := im.RGBAAt(x, y); c.A >= 128 {
					set[lum(c)] = struct{}{}
				}
			}
		}
	}
	levels := make([]int, 0, len(set))
	for l := range set {
		levels = append(levels, l)
	}
	sort.Ints(levels)
	m := make(map[int]int, len(levels))
	n := len(levels)
	for rank, l := range levels {
		idx := 0
		if n > 1 {
			idx = (rank*3 + (n-1)/2) / (n - 1) // round(rank * 3 / (n-1))
		}
		if idx > 3 {
			idx = 3
		}
		m[l] = idx
	}
	return m
}

// shadeFromMap quantises one pixel: transparent pixels become the LCD
// background (so the sprite sits on a continuous green field), opaque pixels use
// the sprite's rank map. The luminance bucket fallback only fires for a value
// not seen at build time, which nearest-neighbour scaling shouldn't produce.
func shadeFromMap(c color.RGBA, sm map[int]int) lipgloss.Color {
	if c.A < 128 {
		return pal.screen
	}
	shades := pal.shades() // [ink, dark, light, screen]
	if idx, ok := sm[lum(c)]; ok {
		return shades[idx]
	}
	switch l := lum(c); {
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
