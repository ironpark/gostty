package main

import (
	"github.com/ironpark/gostty"
	"github.com/ironpark/gostty/examples/hypercat/ui"
)

// themeChanged and fontsChanged are what a tab does about a settings change the
// window made for all of them.
func (tab *terminal) themeChanged() error {
	tab.frame.redraw.MarkAll()
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
func (tab *terminal) applyPalette() error {
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
func (tab *terminal) restyle() error {
	if tab.state == nil {
		return nil
	}
	state, err := gostty.NewRenderState()
	if err != nil {
		return err
	}
	_ = tab.state.Close()
	tab.state = state
	tab.frame.redraw.MarkAll()
	return nil
}

func (tab *terminal) fontsChanged() {
	tab.frame.redraw.MarkAll()
	// The grid is measured in cells and the cell just changed shape, so the
	// window holds a different number of them. Layout is where that is worked
	// out; this only has to say that the answer it cached is stale, because the
	// column count can survive a size change while the pixel geometry the image
	// protocol measures in does not.
	tab.relayout = true
}

func (tab *terminal) currentTheme() ui.Theme { return ui.ThemeAt(tab.settings.ThemeIndex()) }
func (tab *terminal) colorScheme() gostty.ColorScheme {
	if tab.currentTheme().Light(tab.frame.colors.terminalBg) {
		return gostty.ColorSchemeLight
	}
	return gostty.ColorSchemeDark
}
