package main

import (
	"fmt"
	"strings"

	"github.com/ironpark/gostty/examples/hypercat/fonts"
	"github.com/ironpark/gostty/examples/hypercat/thecat"
	"github.com/ironpark/gostty/examples/hypercat/ui"
)

// wrap brings an index back into [0, n) from either side.
func wrap(i, n int) int { return (i%n + n) % n }

// tabSettings are the terminal configuration a tab carries and a new tab
// inherits; panel navigation lives in ui. Everything the settings panel can
// change is here, so it is one value to copy.
type tabSettings struct {
	families []*fonts.Family
	family   int
	size     float64
	theme    int
	cat      thecat.Mode
}

// defaultSettings discovers the system fonts once, for the first tab.
func defaultSettings() tabSettings {
	families := fonts.Discover()
	_, index := fonts.DefaultFamily(families)
	return tabSettings{families: families, family: index, size: fonts.DefaultSize}
}

// clampFamily is the one place an out-of-range family index is repaired.
func (s tabSettings) clampFamily(i int) int {
	return min(max(i, 0), len(s.families)-1)
}

// currentFamily is nil when there are no system fonts, which is what asks
// fonts.Load for the bundled bitmap.
func (s tabSettings) currentFamily() *fonts.Family {
	if len(s.families) == 0 {
		return nil
	}
	return s.families[s.clampFamily(s.family)]
}

// settingsAdjust applies one step of the settings panel's current row.
func (tab *terminalTab) settingsAdjust(delta int) error {
	switch tab.panels.Settings.Row {
	case ui.SettingFont:
		if len(tab.settings.families) == 0 {
			return nil
		}
		next := wrap(tab.settings.family+delta, len(tab.settings.families))
		return tab.setFont(next, tab.settings.size)
	case ui.SettingTheme:
		tab.settings.theme = wrap(tab.settings.theme+delta, ui.ThemeCount())
		tab.redraw.markAll()
		// A program that subscribed with mode 2031 is told now, not the next
		// time it thinks to ask.
		return tab.stream.ColorSchemeChanged(tab.colorScheme())
	case ui.SettingCat:
		tab.setCatMode(tab.settings.cat.Step(delta))
		return nil
	default:
		return tab.setFont(tab.settings.family, tab.settings.size+float64(delta))
	}
}

// settingsValues are the labels the panel shows next to each row.
func (tab *terminalTab) settingsValues() ui.SettingsValues {
	return ui.SettingsValues{
		tab.fontLabel(), fmt.Sprintf("%.0f px", tab.settings.size), tab.themeLabel(), tab.catLabel(),
	}
}

func (tab *terminalTab) themeLabel() string {
	return fmt.Sprintf("%s  (%d/%d)", tab.currentTheme().Name, tab.settings.theme+1, ui.ThemeCount())
}

// fontLabel says which faces the chosen family actually has, because that is
// what decides whether bold and italic text looks any different.
func (tab *terminalTab) fontLabel() string {
	family := tab.settings.currentFamily()
	if family == nil {
		return tab.fonts.FamilyName + " (bundled)"
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
	label := fmt.Sprintf("%s  (%d/%d)", family.Name, tab.settings.family+1, len(tab.settings.families))
	if len(have) == 0 {
		return label + "  regular only"
	}
	return label + "  + " + strings.Join(have, ", ")
}

// catLabel says what the cat is up to, which is the only way to tell a cat that
// is switched off from one asleep behind the prompt.
func (tab *terminalTab) catLabel() string {
	if tab.cat == nil {
		return "unavailable"
	}
	return tab.cat.Mode().String()
}
