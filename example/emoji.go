package main

import (
	"bytes"
	"image"
	"image/png"
	"math"
	"os"
	"runtime"

	"github.com/go-text/typesetting/font"
	"github.com/hajimehoshi/ebiten/v2"
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

type emojiFont struct {
	face *font.Face
	// The size the cache was built at, in pixels. A font size change empties
	// it: these are bitmaps, so a picture for one cell height is not a picture
	// for another.
	ppem uint16
	// One image per rune, nil where the font has no picture for it, so a rune
	// that is not an emoji is only looked up once.
	cache map[rune]*ebiten.Image
}

// loadEmoji finds the system's colour emoji font, or returns nil, which leaves
// emoji to the ordinary faces.
//
// GOSTTY_FONT_EMOJI overrides the search.
func loadEmoji() *emojiFont {
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
		return &emojiFont{face: faces[0], cache: map[rune]*ebiten.Image{}}
	}
	return nil
}

// glyph is the picture for a rune at a cell of this height, and whether the
// font has one.
func (e *emojiFont) glyph(r rune, height float64) (*ebiten.Image, bool) {
	ppem := uint16(math.Max(1, math.Round(height)))
	if ppem != e.ppem {
		clear(e.cache)
		e.ppem = ppem
		e.face.SetPpem(ppem, ppem)
	}
	if img, ok := e.cache[r]; ok {
		return img, img != nil
	}

	img := e.render(r)
	e.cache[r] = img
	return img, img != nil
}

func (e *emojiFont) render(r rune) *ebiten.Image {
	gid, ok := e.face.Font.NominalGlyph(r)
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

// drawEmoji paints the picture for a cell, and reports whether there was one.
// The caller has already decided the cell is two columns wide.
func (g *game) drawEmoji(screen *ebiten.Image, r rune, x, y float64) bool {
	if g.emoji == nil {
		return false
	}
	img, ok := g.emoji.glyph(r, g.fonts.cellH)
	if !ok {
		return false
	}

	// Fit inside the two cells it was given, keeping it square: emoji are
	// drawn square and a stretched one is worse than a small one.
	box, line := 2*g.fonts.cellW, g.fonts.cellH
	w := float64(img.Bounds().Dx())
	h := float64(img.Bounds().Dy())
	scale := math.Min(box/w, line/h)

	op := &ebiten.DrawImageOptions{Filter: ebiten.FilterLinear}
	op.GeoM.Scale(scale, scale)
	op.GeoM.Translate(x+(box-w*scale)/2, y+(line-h*scale)/2)
	screen.DrawImage(img, op)
	return true
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
