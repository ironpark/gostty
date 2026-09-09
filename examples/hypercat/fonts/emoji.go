package fonts

import (
	"bytes"
	"image"
	"image/png"
	"math"
	"os"
	"runtime"

	"github.com/go-text/typesetting/di"
	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/language"
	"github.com/go-text/typesetting/shaping"
	"github.com/hajimehoshi/ebiten/v2"
	"golang.org/x/image/math/fixed"
)

// Colour emoji, which do not come out of the text renderer at all.
//
// A colour emoji font stores pictures rather than outlines -- PNGs, one per
// glyph per size, in the `sbix` table on macOS and `CBDT` elsewhere -- and
// Ebitengine's text drawing rasterises outlines. Handed one of these fonts it
// finds no segments to fill and draws nothing, which is why an emoji came out
// as a blank or as whatever box the CJK face has at that codepoint.
//
// So the pictures are fetched from the font directly and drawn as images. The
// font is read with go-text, the same library the text renderer uses
// underneath, which will hand over a glyph's bitmap at a requested pixel size.
//
// Only for cells the terminal made two columns wide. That is the same rule the
// terminal itself used to lay the line out, and it is what separates an emoji
// from the text-presentation symbols that live near them: U+2705 is two columns
// and a picture, U+2192 is one column and a character in the text font. The
// emoji font has glyphs for some of the latter too -- the keycap bases, the
// copyright sign -- and drawing those as pictures would be wrong.

// Emoji decodes and caches bitmap glyphs. Use it on the render goroutine.
type Emoji struct {
	face *font.Face
	// The size the cache was built at, in pixels. A font size change empties
	// it: these are bitmaps, so a picture for one cell height is not a picture
	// for another.
	ppem uint16
	// One image per grapheme cluster, nil where the font has no picture for
	// it, so text that is not an emoji is only looked up once.
	cache map[string]*ebiten.Image
	// The shaper that turns a multi-codepoint cluster into the one glyph the
	// font draws it with. Kept because it caches its own work per face.
	shaper shaping.HarfbuzzShaper
}

// LoadEmoji finds the system's colour emoji font, or returns nil, which leaves
// emoji to the ordinary faces.
//
// GOSTTY_FONT_EMOJI overrides the search.
func LoadEmoji() *Emoji {
	paths := emojiCandidates()
	if override := os.Getenv("GOSTTY_FONT_EMOJI"); override != "" {
		paths = append([]string{override}, paths...)
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		// Read into memory rather than kept open: the whole file is a few
		// megabytes and the parser reads from it lazily.
		faces, err := font.ParseTTC(bytes.NewReader(data))
		if err != nil || len(faces) == 0 {
			continue
		}
		emoji := &Emoji{face: faces[0], cache: map[string]*ebiten.Image{}}
		// A font is only this one if it answers in pictures. Windows ships
		// Segoe UI Emoji, which is colour but COLR: outlines with a palette,
		// which `render` declines and the text faces draw perfectly well. So
		// the file being there is not the question -- whether it has a strike
		// is, and one emoji is enough to ask.
		if _, ok := emoji.Glyph(string(emojiProbe), 20); !ok {
			continue
		}
		clear(emoji.cache)
		return emoji
	}
	return nil
}

// The rune `LoadEmoji` asks a candidate for: a picture for this one means a
// picture for the rest.
const emojiProbe = '\U0001F600'

// Glyph is the picture for one grapheme cluster at a cell of this height, and
// whether the font has one.
//
// A cluster rather than a rune, because the picture belongs to the
// combination: a flag is two regional indicators, a family is three people
// joined by zero-width joiners, and a skin tone is a modifier after the
// person. The terminal hands over the whole cluster for one cell; asking the
// font about its first codepoint alone would draw a letter, or one stranger.
func (e *Emoji) Glyph(cluster string, height float64) (*ebiten.Image, bool) {
	ppem := uint16(math.Max(1, math.Round(height)))
	if ppem != e.ppem {
		clear(e.cache)
		e.ppem = ppem
		e.face.SetPpem(ppem, ppem)
	}
	if img, ok := e.cache[cluster]; ok {
		return img, img != nil
	}

	img := e.render(cluster)
	e.cache[cluster] = img
	return img, img != nil
}

func (e *Emoji) render(cluster string) *ebiten.Image {
	gid, ok := e.glyphID(cluster)
	if !ok {
		return nil
	}
	bitmap, ok := e.face.GlyphData(gid).(font.GlyphBitmap)
	if !ok {
		// An outline, or nothing: either way this is not a picture, and the
		// text renderer is the right thing to draw it with.
		return nil
	}
	var src image.Image
	switch bitmap.Format {
	case font.PNG:
		decoded, err := png.Decode(bytes.NewReader(bitmap.Data))
		if err != nil {
			return nil
		}
		src = decoded
	default:
		// Black-and-white, TIFF and JPEG strikes exist in the format but not in
		// any emoji font worth the trouble of decoding them for.
		return nil
	}
	return ebiten.NewImageFromImage(src)
}

// glyphID is the one glyph the font draws a cluster with.
//
// A single codepoint is a cmap lookup. Anything longer has to be shaped: the
// substitutions that turn two regional indicators into a flag live in the
// font's GSUB table, and running them is what a shaper is. A cluster that does
// not come out as exactly one glyph is not a picture in this font -- it is
// text the ordinary faces should draw -- so it is refused here.
func (e *Emoji) glyphID(cluster string) (font.GID, bool) {
	runes := []rune(cluster)
	switch len(runes) {
	case 0:
		return 0, false
	case 1:
		return e.face.Font.NominalGlyph(runes[0])
	}
	out := e.shaper.Shape(shaping.Input{
		Text:      runes,
		RunStart:  0,
		RunEnd:    len(runes),
		Direction: di.DirectionLTR,
		Face:      e.face,
		Size:      fixed.I(int(e.ppem)),
		Script:    language.LookupScript(runes[0]),
		Language:  language.NewLanguage("en"),
	})
	if len(out.Glyphs) != 1 || out.Glyphs[0].GlyphID == 0 {
		return 0, false
	}
	return out.Glyphs[0].GlyphID, true
}

func emojiCandidates() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{"/System/Library/Fonts/Apple Color Emoji.ttc"}
	case "windows":
		// COLR rather than a bitmap: go-text hands back an outline, which is
		// declined above, so Windows keeps the text face for now.
		return []string{`C:\Windows\Fonts\seguiemj.ttf`}
	default:
		return []string{
			"/usr/share/fonts/truetype/noto/NotoColorEmoji.ttf",
			"/usr/share/fonts/noto/NotoColorEmoji.ttf",
			"/usr/share/fonts/google-noto-emoji/NotoColorEmoji.ttf",
			"/usr/share/fonts/TTF/NotoColorEmoji.ttf",
		}
	}
}
