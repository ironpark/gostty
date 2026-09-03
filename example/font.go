package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/hajimehoshi/bitmapfont/v4"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
)

const (
	// The size of the monospace face, in device-independent pixels. What is
	// actually asked of the face is this times the display's scale factor, so
	// the text is the same size on a HiDPI screen and drawn at its resolution.
	defaultFontSize = 14
	minFontSize     = 8
	maxFontSize     = 40
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

// fontFamily is one typeface as the settings UI offers it: a name, and however
// many of the four faces the system actually has.
//
// Grouped by the family name the font itself reports rather than by filename.
// Both shapes exist -- macOS ships Menlo as one .ttc holding all four, while a
// font installed by hand is usually four files -- and metadata is the only
// thing they agree on.
type fontFamily struct {
	name    string
	sources [4]*text.GoTextFaceSource
}

// source picks the closest face the family has. A family with no bold is
// common (a variable font ships one file), and drawing its bold cells in the
// regular face would lose the distinction, so `synthetic` asks the renderer to
// double-strike instead.
func (f *fontFamily) source(bold, italic bool) (src *text.GoTextFaceSource, synthetic bool) {
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

func (f *fontFamily) has(bold, italic bool) bool {
	want := 0
	if bold {
		want |= faceBold
	}
	if italic {
		want |= faceItalic
	}
	return f.sources[want] != nil
}

// fontSet is the faces the grid is drawn with, plus every measurement derived
// from them. The drawing code reads these fields; it does not measure.
//
// The wide face is separate from the four narrow ones because the terminal has
// already decided how many columns each character gets. Picking the face from
// that decision keeps the glyph and the cell in agreement, which a fallback
// chain that resolves by coverage cannot promise: a font whose Hangul is not
// exactly twice the Latin advance would drift across the row.
type fontSet struct {
	family string
	size   float64

	// Indexed by faceBold|faceItalic.
	narrow [4]text.Face
	// True where the face at that index is the regular one struck twice.
	synthetic [4]bool

	wide text.Face

	// How far the wide face must move to share the narrow face's baseline, and
	// to sit centred in its two columns.
	wideDX, wideDY float64

	// Whole-number scale for the bitmap fallback; 1 for a scalable face.
	scale float64

	cellW, cellH float64

	// Where the decorations the font does not draw go.
	lineH, underlineY, underline2Y, strikeY float64
}

// face returns the face for a cell and whether it has to be double-struck to
// look bold.
func (f *fontSet) face(bold, italic bool) (text.Face, bool) {
	i := 0
	if bold {
		i |= faceBold
	}
	if italic {
		i |= faceItalic
	}
	return f.narrow[i], f.synthetic[i]
}

// loadFonts builds the faces for one family at one size, with a companion for
// the wide scripts. A nil family means there was nothing to load, and the
// bundled bitmap font takes over.
//
// The size is in device pixels: the caller has already multiplied by the
// display's scale factor, so on a HiDPI screen this is a bigger number for the
// same apparent size.
//
// GOSTTY_FONT and GOSTTY_FONT_CJK override the search with a path to a .ttf,
// .otf or .ttc.
func loadFonts(family *fontFamily, size float64) *fontSet {
	if family == nil {
		return bitmapFonts(size)
	}
	set := &fontSet{family: family.name, size: size, scale: 1}
	for i := range set.narrow {
		src, synthetic := family.source(i&faceBold != 0, i&faceItalic != 0)
		set.narrow[i] = &text.GoTextFace{Source: src, Size: size}
		set.synthetic[i] = synthetic
	}
	// The regular face defines the grid; the others are the same family at the
	// same size, so they share it.
	set.cellW, set.cellH = cellSize(set.narrow[0], 1)
	set.wide = set.narrow[0]

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
		span := 2 * set.cellW
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
		set.wideDX = (span - adv) / 2
		set.wideDY = set.narrow[0].Metrics().HAscent - wide.Metrics().HAscent
		set.wide = wide
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
func bitmapFonts(size float64) *fontSet {
	// The plain Face rather than FaceEA: FaceEA draws the East Asian
	// ambiguous-width characters wide, but ghostty counts them as narrow, and
	// the grid has to agree with the terminal that owns it.
	face := text.NewGoXFace(bitmapfont.Face)
	scale := math.Max(1, math.Round(size/bitmapFontHeight))
	set := &fontSet{family: "bitmap", size: scale * bitmapFontHeight, scale: scale, wide: face}
	for i := range set.narrow {
		set.narrow[i] = face
		// One weight only, so bold is double-struck.
		set.synthetic[i] = i&faceBold != 0
	}
	set.cellW, set.cellH = cellSize(face, scale)
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

// discoverFonts collects the monospace families on this machine, so the
// settings UI has something to offer. The first one is the default.
//
// Every face in every candidate file is opened and filed under the family name
// it reports. That is the only thing a four-file family and a four-face
// collection have in common, and it costs one open per file at startup.
func discoverFonts() []*fontFamily {
	byName := map[string]*fontFamily{}
	var order []string

	add := func(path string) {
		sources, err := openCollection(path)
		if err != nil {
			return
		}
		for _, src := range sources {
			meta := src.Metadata()
			name := displayName(meta.Family)
			if name == "" {
				continue
			}
			family, ok := byName[name]
			if !ok {
				family = &fontFamily{name: name}
				byName[name] = family
				order = append(order, name)
			}
			i := 0
			// Semibold and up counts as the bold face. Below that a family
			// with several light weights would fill the bold slot with one.
			if meta.Weight >= text.WeightSemibold {
				i |= faceBold
			}
			if meta.Style == text.StyleItalic {
				i |= faceItalic
			}
			// First one wins, so a family with several weights keeps the one
			// nearest regular rather than the last file read.
			if family.sources[i] == nil {
				family.sources[i] = src
			}
		}
	}

	for _, path := range fontFiles() {
		add(path)
	}

	families := make([]*fontFamily, 0, len(order))
	for _, name := range order {
		// A family with no regular face is one this cannot draw with.
		if byName[name].sources[0] != nil {
			families = append(families, byName[name])
		}
	}
	// The candidates are in preference order and the override comes first, so
	// only what the directory scan found is sorted, which is everything after
	// the fixed list.
	fixed := len(monoCandidates())
	if os.Getenv("GOSTTY_FONT") != "" {
		fixed++
	}
	if fixed < len(families) {
		rest := families[min(fixed, len(families)):]
		sort.Slice(rest, func(i, j int) bool { return rest[i].name < rest[j].name })
	}
	return families
}

// The families to start with, in the order they are worth having, if any of
// them is on the machine.
//
// JetBrains Mono first because it is a terminal font: it has all four faces, a
// tall x-height and letterforms that stay apart at small sizes, which is more
// than can be said for what most systems ship. The Nerd Font builds come in
// three widths and only the "Mono" one keeps its icons inside a single cell,
// which is the one a grid can use.
var preferredFamilies = []string{
	"JetBrains Mono",
	"JetBrainsMono NFM",
	"JetBrainsMono NF",
	"JetBrainsMonoNL NFM",
	"JetBrainsMonoNL NF",
}

// defaultFamily picks what the window opens with, and where it sits in the list
// the settings panel offers.
//
// GOSTTY_FONT wins outright: someone who named a font meant it. Otherwise the
// preferred families are tried in order, then anything else by the same name,
// and failing all of that the first font found on the system.
func defaultFamily(families []*fontFamily) (*fontFamily, int) {
	if len(families) == 0 {
		return nil, 0
	}
	if os.Getenv("GOSTTY_FONT") != "" {
		return families[0], 0
	}
	for _, want := range preferredFamilies {
		for i, family := range families {
			if sameFamily(family.name, want) {
				return family, i
			}
		}
	}
	// A build of the same family under a name not listed above.
	for i, family := range families {
		if strings.HasPrefix(normalizeFamily(family.name), normalizeFamily(preferredFamilies[0])) {
			return family, i
		}
	}
	return families[0], 0
}

func sameFamily(a, b string) bool {
	return normalizeFamily(a) == normalizeFamily(b)
}

// normalizeFamily takes the case and the spaces out, since the same family is
// written several ways: "JetBrains Mono", "JetBrainsMono NFM".
func normalizeFamily(name string) string {
	return strings.ToLower(strings.ReplaceAll(name, " ", ""))
}

// displayName tidies the name a font reports. macOS hides its system fonts
// behind a leading dot (".SF NS Mono"), which is a packaging detail rather than
// something to show a user.
func displayName(family string) string {
	return strings.TrimPrefix(strings.TrimSpace(family), ".")
}

// fontFiles is where to look for monospace fonts: the known system paths first,
// then whatever the user installed themselves, filtered by name because opening
// every font on the machine to find out is not worth the startup.
func fontFiles() []string {
	var paths []string
	if override := os.Getenv("GOSTTY_FONT"); override != "" {
		paths = append(paths, override)
	}
	paths = append(paths, monoCandidates()...)
	for _, dir := range userFontDirs() {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			name := entry.Name()
			if !fontExtension(name) || !strings.Contains(strings.ToLower(name), "mono") {
				continue
			}
			paths = append(paths, filepath.Join(dir, name))
		}
	}
	return paths
}

func fontExtension(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".ttf", ".otf", ".ttc", ".otc":
		return true
	}
	return false
}

func userFontDirs() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	switch runtime.GOOS {
	case "darwin":
		return []string{filepath.Join(home, "Library/Fonts"), "/Library/Fonts"}
	case "windows":
		return []string{filepath.Join(home, `AppData\Local\Microsoft\Windows\Fonts`)}
	default:
		return []string{
			filepath.Join(home, ".local/share/fonts"),
			filepath.Join(home, ".fonts"),
		}
	}
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
	sources, err := openCollection(path)
	if err != nil {
		return nil, err
	}
	// A .ttc holds several faces; the first is the regular one in every
	// collection this looks at.
	return sources[0], nil
}

func openCollection(path string) ([]*text.GoTextFaceSource, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	sources, err := text.NewGoTextFaceSourcesFromCollection(f)
	if err != nil {
		return nil, err
	}
	if len(sources) == 0 {
		return nil, fmt.Errorf("%s: no faces", path)
	}
	return sources, nil
}

func monoCandidates() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{
			"/System/Library/Fonts/Menlo.ttc",
			"/System/Library/Fonts/SFNSMono.ttf",
			"/System/Library/Fonts/SFNSMonoItalic.ttf",
			"/System/Library/Fonts/Monaco.ttf",
			"/System/Library/Fonts/Supplemental/Andale Mono.ttf",
			"/System/Library/Fonts/Supplemental/PTMono.ttc",
			"/System/Library/Fonts/Supplemental/Courier New.ttf",
			"/System/Library/Fonts/Supplemental/Courier New Bold.ttf",
			"/System/Library/Fonts/Supplemental/Courier New Italic.ttf",
			"/System/Library/Fonts/Supplemental/Courier New Bold Italic.ttf",
		}
	case "windows":
		return []string{
			`C:\Windows\Fonts\consola.ttf`,
			`C:\Windows\Fonts\consolab.ttf`,
			`C:\Windows\Fonts\consolai.ttf`,
			`C:\Windows\Fonts\consolaz.ttf`,
			`C:\Windows\Fonts\cour.ttf`,
			`C:\Windows\Fonts\courbd.ttf`,
			`C:\Windows\Fonts\couri.ttf`,
			`C:\Windows\Fonts\courbi.ttf`,
		}
	default:
		return []string{
			"/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf",
			"/usr/share/fonts/truetype/dejavu/DejaVuSansMono-Bold.ttf",
			"/usr/share/fonts/truetype/dejavu/DejaVuSansMono-Oblique.ttf",
			"/usr/share/fonts/truetype/dejavu/DejaVuSansMono-BoldOblique.ttf",
			"/usr/share/fonts/TTF/DejaVuSansMono.ttf",
			"/usr/share/fonts/truetype/liberation/LiberationMono-Regular.ttf",
			"/usr/share/fonts/truetype/liberation/LiberationMono-Bold.ttf",
			"/usr/share/fonts/truetype/liberation/LiberationMono-Italic.ttf",
			"/usr/share/fonts/truetype/liberation/LiberationMono-BoldItalic.ttf",
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
