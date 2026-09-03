package main

import (
	"fmt"
	"math"
	"os"
	"runtime"

	"github.com/hajimehoshi/bitmapfont/v4"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
)

const (
	// The size of the monospace face, in pixels. The cell is derived from it.
	fontSize = 14
	// The bitmap fallback is drawn at this integer scale, unfiltered.
	bitmapScale = 2
)

// fontSet is the pair of faces the grid is drawn with, plus every measurement
// derived from them. The drawing code reads these fields; it does not measure.
//
// Two faces rather than a fallback chain because the terminal has already
// decided how many columns each character gets. Picking the face from that
// decision keeps the glyph and the cell in agreement, which a chain that
// resolves by coverage cannot promise: a font whose Hangul is not exactly twice
// the Latin advance would drift across the row.
type fontSet struct {
	narrow, wide text.Face

	// How far the wide face must move to share the narrow face's baseline, and
	// to sit centred in its two columns.
	wideDX, wideDY float64

	// Whole-number scale for the bitmap fallback; 1 for a scalable face.
	scale float64

	cellW, cellH float64

	// Where the decorations the font does not draw go.
	lineH, underlineY, underline2Y, strikeY float64
}

// loadFonts picks the system monospace font and a companion for the wide
// scripts, falling back to a bundled bitmap font when neither can be found.
//
// GOSTTY_FONT and GOSTTY_FONT_CJK override the search with a path to a .ttf,
// .otf or .ttc.
func loadFonts() (*fontSet, error) {
	narrowSrc := openFirst(os.Getenv("GOSTTY_FONT"), monoCandidates())
	wideSrc := openFirst(os.Getenv("GOSTTY_FONT_CJK"), wideCandidates())
	if narrowSrc == nil {
		return bitmapFonts(), nil
	}

	narrow := &text.GoTextFace{Source: narrowSrc, Size: fontSize}
	set := &fontSet{narrow: narrow, wide: narrow, scale: 1}
	set.cellW, set.cellH = cellSize(narrow, 1)

	if wideSrc != nil {
		// Both faces are drawn at the same em size, so a Hangul syllable looks
		// like it belongs next to the Latin text rather than looming over it.
		//
		// Matching the advance to two cells instead -- the obvious thing, since
		// the cell is two columns wide -- makes the wide face far too big: a
		// monospace Latin advance is about 0.6em while a CJK one is near 1em,
		// so forcing the wide advance to twice the narrow one inflates its em
		// by half again, and a CJK glyph fills its em much more fully than a
		// Latin one fills its own. Nothing here needs the advance anyway: every
		// glyph is placed at its own cell's origin, so it only has to fit.
		wide := &text.GoTextFace{Source: wideSrc, Size: fontSize}
		span := 2 * set.cellW
		adv := text.Advance("안", wide)
		if adv > span {
			// Too wide even at the shared size: shrink until it fits. Advance
			// is linear in size, so one correction is exact.
			wide.Size = fontSize * span / adv
			adv = span
		}
		// Centre it in the two columns, and line it up on the narrow face's
		// baseline. The monospace face defines the grid, its height included:
		// sizing the row to whichever face is taller would leave the Latin text
		// swimming in a cell far bigger than it needs, so the wide face is
		// allowed to overflow the row a little instead.
		set.wideDX = (span - adv) / 2
		set.wideDY = narrow.Metrics().HAscent - wide.Metrics().HAscent
		set.wide = wide
	}

	set.deriveDecorations()
	return set, nil
}

// bitmapFonts is the fallback: a 12px bitmap covering Hangul, kana and CJK as
// well as Latin on one 6x12 grid, scaled by a whole number so it stays crisp.
func bitmapFonts() *fontSet {
	// The plain Face rather than FaceEA: FaceEA draws the East Asian
	// ambiguous-width characters wide, but ghostty counts them as narrow, and
	// the grid has to agree with the terminal that owns it.
	face := text.NewGoXFace(bitmapfont.Face)
	set := &fontSet{narrow: face, wide: face, scale: bitmapScale}
	set.cellW, set.cellH = cellSize(face, bitmapScale)
	set.deriveDecorations()
	return set
}

// cellSize is the grid a face implies. A terminal wants whole pixels; a
// fractional advance would let the rounding error accumulate across a row.
func cellSize(face text.Face, scale float64) (w, h float64) {
	m := face.Metrics()
	return math.Ceil(text.Advance("M", face) * scale), math.Ceil((m.HAscent + m.HDescent) * scale)
}

// deriveDecorations places the lines the font does not draw. They are font
// metrics, so they are worked out once here rather than guessed at with a
// literal offset at each drawing site.
func (f *fontSet) deriveDecorations() {
	f.lineH = math.Max(1, math.Round(f.cellH/16))
	f.underlineY = f.cellH - 2*f.lineH
	f.underline2Y = f.cellH - 4*f.lineH
	f.strikeY = math.Round(f.cellH / 2)
}

func openFirst(override string, candidates []string) *text.GoTextFaceSource {
	if override != "" {
		src, err := openFont(override)
		if err == nil {
			return src
		}
		fmt.Fprintf(os.Stderr, "gostty: %s: %v\n", override, err)
	}
	for _, path := range candidates {
		if src, err := openFont(path); err == nil {
			return src
		}
	}
	return nil
}

func openFont(path string) (*text.GoTextFaceSource, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// A .ttc holds several faces; the first is the regular one in every
	// collection this looks at.
	sources, err := text.NewGoTextFaceSourcesFromCollection(f)
	if err != nil {
		return nil, err
	}
	if len(sources) == 0 {
		return nil, fmt.Errorf("%s: no faces", path)
	}
	return sources[0], nil
}

func monoCandidates() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{
			"/System/Library/Fonts/Menlo.ttc",
			"/System/Library/Fonts/SFNSMono.ttf",
			"/System/Library/Fonts/Monaco.ttf",
		}
	case "windows":
		return []string{
			`C:\Windows\Fonts\consola.ttf`,
			`C:\Windows\Fonts\cour.ttf`,
		}
	default:
		return []string{
			"/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf",
			"/usr/share/fonts/TTF/DejaVuSansMono.ttf",
			"/usr/share/fonts/truetype/liberation/LiberationMono-Regular.ttf",
			"/usr/share/fonts/google-noto/NotoSansMono-Regular.ttf",
			"/usr/share/fonts/truetype/noto/NotoSansMono-Regular.ttf",
		}
	}
}

func wideCandidates() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{
			"/System/Library/Fonts/AppleSDGothicNeo.ttc",
			"/System/Library/Fonts/Supplemental/AppleGothic.ttf",
			"/System/Library/Fonts/Hiragino Sans GB.ttc",
		}
	case "windows":
		return []string{
			`C:\Windows\Fonts\malgun.ttf`,
			`C:\Windows\Fonts\msgothic.ttc`,
		}
	default:
		return []string{
			"/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc",
			"/usr/share/fonts/noto-cjk/NotoSansCJK-Regular.ttc",
			"/usr/share/fonts/truetype/noto/NotoSansKR-Regular.otf",
		}
	}
}
