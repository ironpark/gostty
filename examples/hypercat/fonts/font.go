package fonts

import (
	"math"
	"os"

	"github.com/hajimehoshi/bitmapfont/v4"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
)

const (
	// The size of the monospace face, in device-independent pixels. What is
	// actually asked of the face is this times the display's scale factor, so
	// the text is the same size on a HiDPI screen and drawn at its resolution.
	DefaultSize = 14
	MinSize     = 8
	MaxSize     = 40
	// The height of the bundled bitmap font, which is what the fallback's
	// whole-number scale is worked out from.
	bitmapFontHeight = 12
)

// A family's four faces, indexed by the two bits that pick between them. The
// terminal asks for bold and italic separately, so they are stored the way they
// are asked for.
const (
	faceBold   = 1
	faceItalic = 2
)

// Family is one typeface as the settings UI offers it: a name, and however
// many of the four faces the system actually has.
//
// Grouped by the family name the font itself reports rather than by filename.
// Both shapes exist -- macOS ships Menlo as one .ttc holding all four, while a
// font installed by hand is usually four files -- and metadata is the only
// thing they agree on.
type Family struct {
	Name    string
	sources [4]*text.GoTextFaceSource
}

// source picks the closest face the family has. A family with no bold is
// common (a variable font ships one file), and drawing its bold cells in the
// regular face would lose the distinction, so `synthetic` asks the renderer to
// double-strike instead.
func (f *Family) source(bold, italic bool) (src *text.GoTextFaceSource, synthetic bool) {
	want := 0
	if bold {
		want |= faceBold
	}
	if italic {
		want |= faceItalic
	}
	if src := f.sources[want]; src != nil {
		return src, false
	}
	// Drop italic first: a slanted regular is a worse lie than a missing slant,
	// and bold is the more visible of the two.
	if italic {
		if src := f.sources[want&^faceItalic]; src != nil {
			return src, false
		}
	}
	if bold {
		if src := f.sources[want&^faceBold]; src != nil {
			return src, true
		}
	}
	return f.sources[0], bold
}

// Has reports whether the family contains the requested style without synthesis.
func (f *Family) Has(bold, italic bool) bool {
	want := 0
	if bold {
		want |= faceBold
	}
	if italic {
		want |= faceItalic
	}
	return f.sources[want] != nil
}

// Set is the faces the grid is drawn with, plus every measurement derived
// from them. The drawing code reads these fields; it does not measure.
//
// The wide face is separate from the four narrow ones because the terminal has
// already decided how many columns each character gets. Picking the face from
// that decision keeps the glyph and the cell in agreement, which a fallback
// chain that resolves by coverage cannot promise: a font whose Hangul is not
// exactly twice the Latin advance would drift across the row.
// Treat a loaded Set and its faces as read-only; call Load to change size.
type Set struct {
	FamilyName string
	Size       float64

	// Indexed by faceBold|faceItalic.
	narrow [4]text.Face
	// True where the face at that index is the regular one struck twice.
	synthetic [4]bool

	Wide text.Face

	// How far the wide face must move to share the narrow face's baseline, and
	// to sit centred in its two columns.
	WideDX, WideDY float64

	// Whole-number scale for the bitmap fallback; 1 for a scalable face.
	Scale float64

	CellWidth, CellHeight float64

	// Where the decorations the font does not draw go.
	LineHeight, UnderlineY, Underline2Y, StrikeY float64
}

// Face returns the face for a cell and whether it has to be double-struck to
// look bold.
func (f *Set) Face(bold, italic bool) (text.Face, bool) {
	i := 0
	if bold {
		i |= faceBold
	}
	if italic {
		i |= faceItalic
	}
	return f.narrow[i], f.synthetic[i]
}

// Load builds the faces for one family at one size, with a companion for
// the wide scripts. A nil family means there was nothing to load, and the
// bundled bitmap font takes over.
//
// The size is in device pixels: the caller has already multiplied by the
// display's scale factor, so on a HiDPI screen this is a bigger number for the
// same apparent size.
//
// GOSTTY_FONT_CJK overrides the wide face with a .ttf, .otf or .ttc path.
// Discover and DefaultFamily apply GOSTTY_FONT when selecting the narrow family.
func Load(family *Family, size float64) *Set {
	if family == nil {
		return bitmapFonts(size)
	}
	set := &Set{FamilyName: family.Name, Size: size, Scale: 1}
	for i := range set.narrow {
		src, synthetic := family.source(i&faceBold != 0, i&faceItalic != 0)
		set.narrow[i] = &text.GoTextFace{Source: src, Size: size}
		set.synthetic[i] = synthetic
	}
	// The regular face defines the grid; the others are the same family at the
	// same size, so they share it.
	set.CellWidth, set.CellHeight = cellSize(set.narrow[0], 1)
	set.Wide = set.narrow[0]

	if wideSrc := openFirst(os.Getenv("GOSTTY_FONT_CJK"), wideCandidates()); wideSrc != nil {
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
		wide := &text.GoTextFace{Source: wideSrc, Size: size}
		span := 2 * set.CellWidth
		adv := text.Advance("안", wide)
		if adv > span {
			// Too wide even at the shared size: shrink until it fits. Advance
			// is linear in size, so one correction is exact.
			wide.Size = size * span / adv
			adv = span
		}
		// Centre it in the two columns, and line it up on the narrow face's
		// baseline. The monospace face defines the grid, its height included:
		// sizing the row to whichever face is taller would leave the Latin text
		// swimming in a cell far bigger than it needs, so the wide face is
		// allowed to overflow the row a little instead.
		set.WideDX = (span - adv) / 2
		set.WideDY = set.narrow[0].Metrics().HAscent - wide.Metrics().HAscent
		set.Wide = wide
	}

	set.deriveDecorations()
	return set
}

// bitmapFonts is the fallback: a 12px bitmap covering Hangul, kana and CJK as
// well as Latin on one 6x12 grid.
//
// A bitmap can only be enlarged by a whole number without turning to mush, so
// the size asked for is rounded to one. That is also what makes it work on a
// HiDPI screen: twice the device pixels is twice the scale.
func bitmapFonts(size float64) *Set {
	// The plain Face rather than FaceEA: FaceEA draws the East Asian
	// ambiguous-width characters wide, but ghostty counts them as narrow, and
	// the grid has to agree with the terminal that owns it.
	face := text.NewGoXFace(bitmapfont.Face)
	scale := math.Max(1, math.Round(size/bitmapFontHeight))
	set := &Set{FamilyName: "bitmap", Size: scale * bitmapFontHeight, Scale: scale, Wide: face}
	for i := range set.narrow {
		set.narrow[i] = face
		// One weight only, so bold is double-struck.
		set.synthetic[i] = i&faceBold != 0
	}
	set.CellWidth, set.CellHeight = cellSize(face, scale)
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
func (f *Set) deriveDecorations() {
	f.LineHeight = math.Max(1, math.Round(f.CellHeight/16))
	f.UnderlineY = f.CellHeight - 2*f.LineHeight
	f.Underline2Y = f.CellHeight - 4*f.LineHeight
	f.StrikeY = math.Round(f.CellHeight / 2)
}
