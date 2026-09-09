package main

import (
	"fmt"
	"image/color"
	"strings"

	"github.com/ironpark/gostty"
	"github.com/ironpark/gostty/examples/hypercat/fonts"
	"github.com/ironpark/gostty/examples/hypercat/thecat"
	"github.com/ironpark/gostty/examples/hypercat/ui"
)

// wrap brings an index back into [0, n) from either side.
func wrap(i, n int) int { return (i%n + n) % n }

// appearance is everything the settings panel can change. It belongs to the
// window, not to a tab: one window draws with one set of faces and one theme,
// so a change made from any tab is a change every tab is drawn with. Each tab
// holds a pointer to the app's copy rather than a copy of its own.
type appearance struct {
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

// defaultAppearance discovers the system fonts once, when the window opens.
func defaultAppearance(dsf float64) *appearance {
	families := fonts.Discover()
	_, index := fonts.DefaultFamily(families)
	settings := &appearance{families: families, family: index, size: fonts.DefaultSize}
	settings.loadFonts(dsf)
	settings.emoji = fonts.LoadEmoji()
	return settings
}

// loadFonts rebuilds the faces for the chosen family at the display's scale.
// The size in the panel is device-independent; what the face is asked for is
// that times the scale factor.
func (s *appearance) loadFonts(dsf float64) {
	s.dsf = dsf
	s.fonts = fonts.Load(s.currentFamily(), s.size*dsf)
}

// clampFamily is the one place an out-of-range family index is repaired.
func (s *appearance) clampFamily(i int) int {
	return min(max(i, 0), len(s.families)-1)
}

// currentFamily is nil when there are no system fonts, which is what asks
// fonts.Load for the bundled bitmap.
func (s *appearance) currentFamily() *fonts.Family {
	if len(s.families) == 0 {
		return nil
	}
	return s.families[s.clampFamily(s.family)]
}

// settingsAdjust applies one step of the settings panel's current row. The
// panel is opened in a tab, but what it changes is the window's, so each of
// these fans the change out to every tab.
func (app *terminalApp) settingsAdjust(row, delta int) error {
	switch row {
	case ui.SettingFont:
		if len(app.settings.families) == 0 {
			return nil
		}
		app.setFont(wrap(app.settings.family+delta, len(app.settings.families)), app.settings.size)
		return nil
	case ui.SettingTheme:
		return app.setTheme(wrap(app.settings.theme+delta, ui.ThemeCount()))
	case ui.SettingCat:
		app.setCatMode(app.settings.cat.Step(delta))
		return nil
	default:
		app.setFont(app.settings.family, app.settings.size+float64(delta))
		return nil
	}
}

// setTheme repaints every tab in the new colors.
func (app *terminalApp) setTheme(index int) error {
	app.settings.theme = index
	for _, tab := range app.tabs {
		if err := tab.themeChanged(); err != nil {
			return err
		}
	}
	return nil
}

// setCatMode keeps the setting and every tab's companion in step. The setting
// is what a new tab starts its cat in; the companions are what draw.
func (app *terminalApp) setCatMode(mode thecat.Mode) {
	app.settings.cat = mode
	for _, tab := range app.tabs {
		tab.cat.SetMode(mode)
	}
}

// settingsValues are the labels the panel shows next to each row.
func (app *terminalApp) settingsValues() ui.SettingsValues {
	return ui.SettingsValues{
		app.fontLabel(), fmt.Sprintf("%.0f px", app.settings.size), app.themeLabel(), app.catLabel(),
	}
}

func (app *terminalApp) themeLabel() string {
	return fmt.Sprintf("%s  (%d/%d)", ui.ThemeAt(app.settings.theme).Name, app.settings.theme+1, ui.ThemeCount())
}

// fontLabel says which faces the chosen family actually has, because that is
// what decides whether bold and italic text looks any different.
func (app *terminalApp) fontLabel() string {
	family := app.settings.currentFamily()
	if family == nil {
		return app.settings.fonts.FamilyName + " (bundled)"
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
	label := fmt.Sprintf("%s  (%d/%d)", family.Name, app.settings.family+1, len(app.settings.families))
	if len(have) == 0 {
		return label + "  regular only"
	}
	return label + "  + " + strings.Join(have, ", ")
}

// catLabel says what the cat is up to, which is the only way to tell a cat that
// is switched off from one asleep behind the prompt.
func (app *terminalApp) catLabel() string {
	tab := app.current()
	if tab == nil || tab.cat == nil {
		return "unavailable"
	}
	return tab.cat.Mode().String()
}

// setFont swaps the faces the window draws with and lets every tab follow.
//
// Only the faces change here. The cell size comes out of them, so the next
// Layout works out how many columns and rows the window now holds and resizes
// each terminal and its pty to match -- which is the same path a window resize
// takes, so the programs are told the way they expect.
func (app *terminalApp) setFont(family int, size float64) {
	size = min(max(size, fonts.MinSize), fonts.MaxSize)
	if len(app.settings.families) == 0 {
		return
	}
	family = app.settings.clampFamily(family)
	if family == app.settings.family && size == app.settings.size {
		return
	}
	app.settings.family, app.settings.size = family, size
	app.applyFont()
}

// applyFont rebuilds the faces from what the settings hold, once for the window,
// and tells every tab that what it drew last frame no longer matches them.
//
// This is also called when the window moves to a display with a different scale
// factor, since the faces are built in device pixels.
func (app *terminalApp) applyFont() {
	app.settings.loadFonts(app.dsf)
	for _, tab := range app.tabs {
		tab.fontsChanged()
	}
}

// themeChanged and fontsChanged are what a tab does about a settings change the
// window made for all of them.
func (tab *terminalTab) themeChanged() error {
	tab.frame.redraw.markAll()
	if err := tab.applyPalette(); err != nil {
		return err
	}
	// A program that subscribed with mode 2031 is told now, not the next time
	// it thinks to ask. The scheme is resolved per tab, since a theme that
	// defers to the terminal takes the colors that tab's program set.
	return tab.stream.ColorSchemeChanged(tab.colorScheme())
}

// applyPalette hands the theme's sixteen ANSI colours to the terminal.
//
// A theme that only replaced the default foreground and background would leave
// everything a program coloured by name -- every ls, every prompt, every diff
// -- in the colours of whatever theme it was not using. The palette is where
// those live, and it belongs to the terminal: a program can set it too, with
// OSC 4, and it is resolved per cell as the screen is read.
//
// The theme sets the defaults rather than the current values, so what a
// program asked for is what a reset comes back to. `ResetPalette` then makes
// the new defaults the current colours, which does drop an OSC 4 palette a
// program set for itself -- the same thing every emulator does when its
// configuration is reloaded.
func (tab *terminalTab) applyPalette() error {
	palette := tab.currentTheme().Palette
	if palette == nil {
		// The terminal theme, which is the terminal's own colours: put back
		// whatever the defaults were before a theme was applied.
		if err := tab.vt.ResetDefaultPalette(); err != nil {
			return err
		}
		return tab.vt.ResetPalette()
	}
	for i, c := range palette {
		if err := tab.vt.SetDefaultPaletteColor(uint8(i), ui.Packed(c)); err != nil {
			return err
		}
	}
	if err := tab.vt.ResetPalette(); err != nil {
		return err
	}
	return tab.restyle()
}

// restyle rebuilds the render state after a change the terminal does not mark
// any row dirty for.
//
// A cell keeps the palette entry it was written with and the colour is
// resolved as the state is read, so every cell on screen changes colour when
// the palette does -- but nothing about the screen changed, so an existing
// state reports the rows it has as clean and hands back the colours it
// resolved last time. A fresh one resolves them again.
func (tab *terminalTab) restyle() error {
	if tab.state == nil {
		return nil
	}
	state, err := gostty.NewRenderState()
	if err != nil {
		return err
	}
	_ = tab.state.Close()
	tab.state = state
	tab.frame.redraw.markAll()
	return nil
}

func (tab *terminalTab) fontsChanged() {
	tab.frame.redraw.markAll()
	// The grid is measured in cells and the cell just changed shape, so the
	// window holds a different number of them. Layout is where that is worked
	// out; this only has to say that the answer it cached is stale, because the
	// column count can survive a size change while the pixel geometry the image
	// protocol measures in does not.
	tab.relayout = true
}

func (tab *terminalTab) currentTheme() ui.Theme { return ui.ThemeAt(tab.settings.theme) }
func (tab *terminalTab) colorScheme() gostty.ColorScheme {
	if tab.currentTheme().Light(tab.frame.colors.terminalBg) {
		return gostty.ColorSchemeLight
	}
	return gostty.ColorSchemeDark
}
func (tab *terminalTab) themeColor(c color.RGBA) color.RGBA {
	return tab.currentTheme().ResolveColor(c, tab.frame.colors.terminalBg, tab.frame.colors.terminalFg)
}
