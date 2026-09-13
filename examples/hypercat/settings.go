package main

import (
	"fmt"
	"strings"

	"github.com/ironpark/gostty/examples/hypercat/fonts"
	"github.com/ironpark/gostty/examples/hypercat/thecat"
	"github.com/ironpark/gostty/examples/hypercat/ui"
)

// settings is everything the settings panel can change. It belongs to the
// window, not to a tab: one window draws with one set of faces and one theme,
// so every tab holds a pointer to the window's copy.
type settings struct {
	families []*fonts.Family
	family   int
	// The size the panel shows, in device-independent pixels: what a user
	// means by "14px". The faces are built at size × dsf.
	size  float64
	theme int // index into ui's themes
	cat   thecat.Mode

	// The faces built from the family and size above, at scale dsf. Every
	// tab draws with these, and their cell metrics are what the grid is laid
	// out on.
	fonts *fonts.Set
	emoji *fonts.Emoji
	dsf   float64
}

// defaultSettings discovers the system fonts once, when the window opens.
func defaultSettings(dsf float64) *settings {
	s := newSettings(fonts.Discover(), fonts.DefaultSize, dsf)
	s.emoji = fonts.LoadEmoji()
	return s
}

// newSettings loads the given families; an empty list uses the bitmap fallback.
func newSettings(families []*fonts.Family, size, dsf float64) *settings {
	_, index := fonts.DefaultFamily(families)
	s := &settings{families: families}
	s.family, s.size = s.clampFamily(index), clampSize(size)
	s.load(dsf)
	return s
}

// load rebuilds the faces for the chosen family at the display's scale.
func (s *settings) load(dsf float64) {
	s.dsf = dsf
	s.fonts = fonts.Load(s.currentFamily(), s.size*dsf)
}

// currentFamily is nil when there are no system fonts, which asks fonts.Load
// for the bundled bitmap.
func (s *settings) currentFamily() *fonts.Family {
	if len(s.families) == 0 {
		return nil
	}
	return s.families[s.clampFamily(s.family)]
}

func (s *settings) clampFamily(i int) int { return min(max(i, 0), max(len(s.families)-1, 0)) }
func clampSize(size float64) float64      { return min(max(size, fonts.MinSize), fonts.MaxSize) }

// setFont changes the family and logical size, and reports whether the faces
// were rebuilt.
func (s *settings) setFont(family int, size float64) bool {
	size, family = clampSize(size), s.clampFamily(family)
	if family == s.family && size == s.size {
		return false
	}
	s.family, s.size = family, size
	s.load(s.dsf)
	return true
}

// The panel is opened in a tab, but what it changes is the window's, so each
// change below fans out to every tab.

// settingsAdjust applies one step of the settings panel's current row.
func (win *window) settingsAdjust(row, delta int) error {
	s := win.settings
	switch row {
	case ui.SettingFont:
		if len(s.families) > 0 && s.setFont(wrap(s.family+delta, len(s.families)), s.size) {
			win.invalidateTabs()
		}
	case ui.SettingTheme:
		return win.setTheme(wrap(s.theme+delta, ui.ThemeCount()))
	case ui.SettingCat:
		win.setCatMode(s.cat.Step(delta))
	default:
		if s.setFont(s.family, s.size+float64(delta)) {
			win.invalidateTabs()
		}
	}
	return nil
}

// setTheme repaints every tab in the new colours.
func (win *window) setTheme(index int) error {
	win.settings.theme = index
	for _, tab := range win.tabs {
		if err := tab.themeChanged(); err != nil {
			return err
		}
	}
	return nil
}

func (win *window) setCatMode(mode thecat.Mode) {
	win.settings.cat = mode
	win.cat.SetMode(mode)
}

// applyFont rebuilds the shared faces for the current scale, then tells every
// tab its grid needs remeasuring.
func (win *window) applyFont() {
	win.settings.load(win.dsf)
	win.invalidateTabs()
}

func (win *window) invalidateTabs() {
	for _, tab := range win.tabs {
		tab.fontsChanged()
	}
}

// settingsValues formats the panel's labels.
func (win *window) settingsValues() ui.SettingsValues {
	s := win.settings
	cat := "unavailable"
	if win.cat != nil {
		cat = s.cat.String()
	}
	return ui.SettingsValues{
		ui.SettingFont:  win.fontLabel(),
		ui.SettingSize:  fmt.Sprintf("%.0f px", s.size),
		ui.SettingTheme: fmt.Sprintf("%s  (%d/%d)", ui.ThemeAt(s.theme).Name, s.theme+1, ui.ThemeCount()),
		ui.SettingCat:   cat,
	}
}

// fontLabel says which faces the chosen family has, which is what decides
// whether bold and italic text look any different.
func (win *window) fontLabel() string {
	s := win.settings
	family := s.currentFamily()
	if family == nil {
		return s.fonts.FamilyName + " (bundled)"
	}
	var have []string
	for _, face := range []struct {
		bold, italic bool
		name         string
	}{{true, false, "bold"}, {false, true, "italic"}, {true, true, "bold italic"}} {
		if family.Has(face.bold, face.italic) {
			have = append(have, face.name)
		}
	}
	label := fmt.Sprintf("%s  (%d/%d)", family.Name, s.family+1, len(s.families))
	if len(have) == 0 {
		return label + "  regular only"
	}
	return label + "  + " + strings.Join(have, ", ")
}
