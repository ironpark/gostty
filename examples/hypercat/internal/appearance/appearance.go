// Package appearance owns the window's shared font resources and settings.
package appearance

import (
	"fmt"
	"strings"

	"github.com/ironpark/gostty/examples/hypercat/fonts"
	"github.com/ironpark/gostty/examples/hypercat/thecat"
	"github.com/ironpark/gostty/examples/hypercat/ui"
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
	s.fonts = fonts.Load(s.currentFamily(), s.size*dsf)
}

// clampFamily is the one place an out-of-range family index is repaired.
func (s *State) clampFamily(i int) int {
	return min(max(i, 0), len(s.families)-1)
}

// currentFamily is nil when there are no system fonts, which is what asks
// fonts.Load for the bundled bitmap.
func (s *State) currentFamily() *fonts.Family {
	if len(s.families) == 0 {
		return nil
	}
	return s.families[s.clampFamily(s.family)]
}

// New loads the supplied font families; an empty list uses the bitmap fallback.
func New(families []*fonts.Family, size, scale float64) *State {
	_, index := fonts.DefaultFamily(families)
	s := &State{families: families, family: max(index, 0), size: min(max(size, fonts.MinSize), fonts.MaxSize)}
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

// Reload rebuilds shared faces after a display scale change.
func (s *State) Reload(scale float64) { s.loadFonts(scale) }

// SetFont changes family and logical size, returning whether faces were rebuilt.
func (s *State) SetFont(family int, size float64) bool {
	size = min(max(size, fonts.MinSize), fonts.MaxSize)
	if len(s.families) == 0 {
		family = 0
	} else {
		family = s.clampFamily(family)
	}
	if family == s.family && size == s.size {
		return false
	}
	s.family, s.size = family, size
	s.loadFonts(s.dsf)
	return true
}

func (s *State) themeLabel() string {
	return fmt.Sprintf("%s  (%d/%d)", ui.ThemeAt(s.theme).Name, s.theme+1, ui.ThemeCount())
}

// fontLabel says which faces the chosen family actually has, because that is
// what decides whether bold and italic text looks any different.
func (s *State) fontLabel() string {
	family := s.currentFamily()
	if family == nil {
		return s.fonts.FamilyName + " (bundled)"
	}
	var have []string
	if family.Has(true, false) {
		have = append(have, "bold")
	}
	if family.Has(false, true) {
		have = append(have, "italic")
	}
	if family.Has(true, true) {
		have = append(have, "bold italic")
	}
	label := fmt.Sprintf("%s  (%d/%d)", family.Name, s.family+1, len(s.families))
	if len(have) == 0 {
		return label + "  regular only"
	}
	return label + "  + " + strings.Join(have, ", ")
}

// Values formats the panel labels; the caller reports whether sprites loaded.
func (s *State) Values(catAvailable bool) ui.SettingsValues {
	cat := "unavailable"
	if catAvailable {
		cat = s.cat.String()
	}
	return ui.SettingsValues{s.fontLabel(), fmt.Sprintf("%.0f px", s.size), s.themeLabel(), cat}
}
