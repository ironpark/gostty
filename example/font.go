package main

import (
	"fmt"
	"math"
	"os"
	"runtime"

	"github.com/hajimehoshi/bitmapfont/v4"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
)

// The size of the monospace face, in pixels. The cell is derived from it.
const fontSize = 14

// fontSet is the pair of faces the grid is drawn with: one for the single-width
// cells and one for the double-width ones.
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

	cellW, cellH float64
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
	// A terminal grid wants whole pixels; a fractional advance would let the
	// rounding error accumulate across a row.
	cellW := math.Ceil(text.Advance("M", narrow))
	nm := narrow.Metrics()
	cellH := math.Ceil(nm.HAscent + nm.HDescent)

	set := &fontSet{narrow: narrow, cellW: cellW, cellH: cellH}
	if wideSrc == nil {
		// Better a wide character drawn from the monospace font, and probably
		// missing, than a crash. The narrow face covers Latin either way.
		set.wide = narrow
		return set, nil
	}

	// Both faces are drawn at the same em size, so a Hangul syllable looks like
	// it belongs next to the Latin text rather than looming over it.
	//
	// Matching the advance to two cells instead -- the obvious thing, since the
	// cell is two columns wide -- makes the wide face far too big: a monospace
	// Latin advance is about 0.6em while a CJK one is near 1em, so forcing the
	// wide advance to twice the narrow one inflates its em by half again, and a
	// CJK glyph fills its em much more fully than a Latin one fills its own.
	// Nothing here needs the advance anyway: every glyph is placed at its own
	// cell's origin, so the advance only has to fit.
	wide := &text.GoTextFace{Source: wideSrc, Size: fontSize}
	span := 2 * cellW
	adv := text.Advance("안", wide)
	if adv > span && adv > 0 {
		// Too wide even at the shared size: shrink until it fits. Advance is
		// linear in size, so one correction is exact.
		wide.Size = fontSize * span / adv
		adv = span
	}
	// Centre it in the two columns; a wide glyph is narrower than its span.
	set.wideDX = (span - adv) / 2
	set.wide = wide

	// The monospace face defines the grid, including its height: sizing the
	// row to whichever face is taller would leave the Latin text swimming in a
	// cell far bigger than it needs. The wide face is aligned to the same
	// baseline instead, and is allowed to overflow the row slightly, which is
	// what it would do in any terminal that mixes two fonts.
	wm := wide.Metrics()
	set.wideDY = nm.HAscent - wm.HAscent
	return set, nil
}

// bitmapFonts is the fallback: a 12px bitmap covering Hangul, kana and CJK as
// well as Latin on one 6x12 grid, scaled by a whole number so it stays crisp.
func bitmapFonts() *fontSet {
	// The plain Face rather than FaceEA: FaceEA draws the East Asian
	// ambiguous-width characters wide, but ghostty counts them as narrow, and
	// the grid has to agree with the terminal that owns it.
	face := text.NewGoXFace(bitmapfont.Face)
	m := face.Metrics()
	return &fontSet{
		narrow: face,
		wide:   face,
		cellW:  text.Advance("M", face) * bitmapScale,
		cellH:  math.Round(m.HAscent+m.HDescent) * bitmapScale,
	}
}

// The bitmap fallback is drawn at this integer scale, unfiltered.
const bitmapScale = 2

func (f *fontSet) scale() float64 {
	if _, ok := f.narrow.(*text.GoXFace); ok {
		return bitmapScale
	}
	return 1
}

func openFirst(override string, candidates []string) *text.GoTextFaceSource {
	if override != "" {
		if src, err := openFont(override); err == nil {
			return src
		} else {
			fmt.Fprintf(os.Stderr, "gostty: %s: %v\n", override, err)
		}
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
