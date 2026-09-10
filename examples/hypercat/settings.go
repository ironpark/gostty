package main

import (
	"fmt"
	"strings"

	"github.com/ironpark/gostty/examples/hypercat/thecat"
	"github.com/ironpark/gostty/examples/hypercat/ui"
)

// settingsAdjust applies one step of the settings panel's current row. The
// panel is opened in a tab, but what it changes is the window's, so each of
// these fans the change out to every tab.
func (win *window) settingsAdjust(row, delta int) error {
	switch row {
	case ui.SettingFont:
		if win.settings.FamilyCount() == 0 {
			return nil
		}
		if win.settings.SetFont(wrap(win.settings.FamilyIndex()+delta, win.settings.FamilyCount()), win.settings.Size()) {
			win.invalidateTabs()
		}
		return nil
	case ui.SettingTheme:
		return win.setTheme(wrap(win.settings.ThemeIndex()+delta, ui.ThemeCount()))
	case ui.SettingCat:
		win.setCatMode(win.settings.CatMode().Step(delta))
		return nil
	default:
		if win.settings.SetFont(win.settings.FamilyIndex(), win.settings.Size()+float64(delta)) {
			win.invalidateTabs()
		}
		return nil
	}
}

// setTheme repaints every tab in the new colors.
func (win *window) setTheme(index int) error {
	win.settings.SetTheme(index)
	for _, tab := range win.tabs {
		if err := tab.themeChanged(); err != nil {
			return err
		}
	}
	return nil
}

// setCatMode keeps the setting and every tab's companion in step. The setting
// is what a new tab starts its cat in; the companions are what draw.
func (win *window) setCatMode(mode thecat.Mode) {
	win.settings.SetCatMode(mode)
	for _, tab := range win.tabs {
		tab.cat.SetMode(mode)
	}
}

// applyFont reloads resources once, then invalidates every tab's geometry.
func (win *window) applyFont() {
	win.settings.Reload(win.dsf)
	win.invalidateTabs()
}

// invalidateTabs tells every tab the shared faces it draws with have changed.
func (win *window) invalidateTabs() {
	for _, tab := range win.tabs {
		tab.fontsChanged()
	}
}

// settingsValues formats the panel labels. It lives here rather than in
// appearance because the cat row's truth is the current tab's, and this is the
// only layer holding both the window's settings and that tab.
func (win *window) settingsValues() ui.SettingsValues {
	tab := win.current()
	cat := "unavailable"
	if tab != nil && tab.cat != nil {
		cat = win.settings.CatMode().String()
	}
	return ui.SettingsValues{
		ui.SettingFont:  win.fontLabel(),
		ui.SettingSize:  fmt.Sprintf("%.0f px", win.settings.Size()),
		ui.SettingTheme: win.themeLabel(),
		ui.SettingCat:   cat,
	}
}

func (win *window) themeLabel() string {
	index := win.settings.ThemeIndex()
	return fmt.Sprintf("%s  (%d/%d)", ui.ThemeAt(index).Name, index+1, ui.ThemeCount())
}

// fontLabel says which faces the chosen family actually has, because that is
// what decides whether bold and italic text looks any different.
func (win *window) fontLabel() string {
	family := win.settings.CurrentFamily()
	if family == nil {
		return win.settings.Fonts().FamilyName + " (bundled)"
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
	label := fmt.Sprintf("%s  (%d/%d)", family.Name, win.settings.FamilyIndex()+1, win.settings.FamilyCount())
	if len(have) == 0 {
		return label + "  regular only"
	}
	return label + "  + " + strings.Join(have, ", ")
}
