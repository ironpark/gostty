// Package appearance owns the window's shared font resources and settings.
package appearance

import (
	"github.com/ironpark/gostty/examples/hypercat/fonts"
	"github.com/ironpark/gostty/examples/hypercat/thecat"
)

// State is everything the settings panel can change. It belongs to the
// window, not to a tab: one window draws with one set of faces and one theme,
// so a change made from any tab is a change every tab is drawn with. Each tab
// holds a pointer to the window's copy rather than a copy of its own.
type State struct {
	families []*fonts.Family
	family   int
	// The size the panel shows, in device-independent pixels, because that is
	// what a user means by "14px".
	size  float64
	theme int
	cat   thecat.Mode

	// The faces built from the family and the size above, at scale dsf. Every
	// tab draws with these, and the cell metrics they carry are what the grid
	// is laid out on.
	fonts *fonts.Set
	emoji *fonts.Emoji
	dsf   float64
}

// Default discovers the system fonts once, when the window opens.
func Default(dsf float64) *State {
	settings := New(fonts.Discover(), fonts.DefaultSize, dsf)
	settings.emoji = fonts.LoadEmoji()
	return settings
}

// loadFonts rebuilds the faces for the chosen family at the display's scale.
// The size in the panel is device-independent; what the face is asked for is
// that times the scale factor.
func (s *State) loadFonts(dsf float64) {
	s.dsf = dsf
	s.fonts = fonts.Load(s.CurrentFamily(), s.size*dsf)
}

// clampFamily is the one place an out-of-range family index is repaired. It is
// total: an empty family list repairs to 0, which currentFamily reads as the
// bundled bitmap.
func (s *State) clampFamily(i int) int {
	return min(max(i, 0), max(len(s.families)-1, 0))
}

// clampSize is the one place a size outside the supported range is repaired.
func clampSize(size float64) float64 {
	return min(max(size, fonts.MinSize), fonts.MaxSize)
}

// CurrentFamily is nil when there are no system fonts, which is what asks
// fonts.Load for the bundled bitmap.
func (s *State) CurrentFamily() *fonts.Family {
	if len(s.families) == 0 {
		return nil
	}
	return s.families[s.clampFamily(s.family)]
}

// New loads the supplied font families; an empty list uses the bitmap fallback.
func New(families []*fonts.Family, size, scale float64) *State {
	_, index := fonts.DefaultFamily(families)
	s := &State{families: families}
	s.family, s.size = s.clampFamily(index), clampSize(size)
	s.loadFonts(scale)
	return s
}

func (s *State) Fonts() *fonts.Set           { return s.fonts }
func (s *State) Emoji() *fonts.Emoji         { return s.emoji }
func (s *State) Size() float64               { return s.size }
func (s *State) Scale() float64              { return s.dsf }
func (s *State) FamilyIndex() int            { return s.family }
func (s *State) FamilyCount() int            { return len(s.families) }
func (s *State) ThemeIndex() int             { return s.theme }
func (s *State) CatMode() thecat.Mode        { return s.cat }
func (s *State) SetTheme(index int)          { s.theme = index }
func (s *State) SetCatMode(mode thecat.Mode) { s.cat = mode }

// SetMetrics replaces the built faces with a fixed set. It exists so tests can
// pin deterministic cell metrics without system font discovery; production code
// changes faces through SetFont and Reload, which keep them consistent with the
// chosen family and size.
func (s *State) SetMetrics(set *fonts.Set) { s.fonts = set }

// Reload rebuilds shared faces after a display scale change.
func (s *State) Reload(scale float64) { s.loadFonts(scale) }

// SetFont changes family and logical size, returning whether faces were rebuilt.
func (s *State) SetFont(family int, size float64) bool {
	size, family = clampSize(size), s.clampFamily(family)
	if family == s.family && size == s.size {
		return false
	}
	s.family, s.size = family, size
	s.loadFonts(s.dsf)
	return true
}
