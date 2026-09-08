package main

import (
	"fmt"
	"strings"

	"github.com/ironpark/gostty/examples/hypercat/fonts"
	"github.com/ironpark/gostty/examples/hypercat/ui"
)

// tabSettings are terminal configuration; panel navigation lives in ui.
type tabSettings struct {
	families []*fonts.Family
	family   int
	size     float64
	theme    int
}

func (tab *terminalTab) settingsAdjust(delta int) error {
	switch tab.panels.Settings.Row {
	case ui.SettingFont:
		if len(tab.settings.families) == 0 {
			return nil
		}
		next := (tab.settings.family + delta + len(tab.settings.families)) % len(tab.settings.families)
		return tab.setFont(next, tab.settings.size)
	case ui.SettingTheme:
		tab.settings.theme = (tab.settings.theme + delta + ui.ThemeCount()) % ui.ThemeCount()
		tab.redrawAll = true
		// A program that subscribed with mode 2031 is told now, not the next
		// time it thinks to ask.
		return tab.stream.ColorSchemeChanged(tab.colorScheme())
	case ui.SettingCat:
		if tab.cat == nil {
			return nil
		}
		mode := 0 // off
		if tab.cat.Enabled() {
			mode = 1 // on
			if tab.cat.Hyper() {
				mode = 2 // hyper
			}
		}
		mode = (mode + delta + 3) % 3
		tab.cat.SetEnabled(mode != 0)
		tab.cat.SetHyper(mode == 2)
		return nil
	default:
		return tab.setFont(tab.settings.family, tab.settings.size+float64(delta))
	}
}

func (tab *terminalTab) themeLabel() string {
	return fmt.Sprintf("%s  (%d/%d)", tab.currentTheme().Name, tab.settings.theme+1, ui.ThemeCount())
}

// fontLabel says which faces the chosen family actually has, because that is
// what decides whether bold and italic text looks any different.
func (tab *terminalTab) fontLabel() string {
	if len(tab.settings.families) == 0 {
		return tab.fonts.FamilyName + " (bundled)"
	}
	family := tab.settings.families[tab.settings.family]
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
	switch {
	case tab.cat == nil:
		return "unavailable"
	case !tab.cat.Enabled():
		return "off"
	case tab.cat.Hyper():
		return "hyper"
	default:
		return "on"
	}
}
