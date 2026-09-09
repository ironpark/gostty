package main

import (
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/ironpark/gostty"
	"github.com/ironpark/gostty/examples/hypercat/keys"
	"github.com/ironpark/gostty/examples/hypercat/ui"
)

// panelInput describes this frame's keyboard to the panels. Runes are only
// collected while the search bar is open, since it is the one component that
// wants text rather than key presses.
func (tab *terminalTab) panelInput(m keys.Mods) ui.Input {
	shortcut := (m.Ctrl && m.Shift) || m.Super
	if tab.panels.Mode == ui.Search {
		tab.chars = ebiten.AppendInputChars(tab.chars[:0])
	} else {
		tab.chars = tab.chars[:0]
	}
	return ui.Input{
		OpenSearch:   shortcut && inpututil.IsKeyJustPressed(ebiten.KeyF),
		OpenSettings: shortcut && inpututil.IsKeyJustPressed(ebiten.KeyComma),
		Close:        inpututil.IsKeyJustPressed(ebiten.KeyEscape),
		Enter:        inpututil.IsKeyJustPressed(ebiten.KeyEnter), Shift: m.Shift,
		Chars:     tab.chars,
		Backspace: keys.Repeating(inpututil.KeyPressDuration(ebiten.KeyBackspace)),
		Up:        inpututil.IsKeyJustPressed(ebiten.KeyArrowUp),
		Down:      inpututil.IsKeyJustPressed(ebiten.KeyArrowDown),
		Left:      inpututil.IsKeyJustPressed(ebiten.KeyArrowLeft) || inpututil.IsKeyJustPressed(ebiten.KeyMinus),
		Right:     inpututil.IsKeyJustPressed(ebiten.KeyArrowRight) || inpututil.IsKeyJustPressed(ebiten.KeyEqual),
	}
}

// settingsStep is one step of the settings panel's current row, reported rather
// than applied: the font, the theme and the cat it moves through belong to the
// window, and a tab does not know the window. The zero value is no step.
type settingsStep struct{ row, delta int }

// applyUIActions is the boundary between components and native terminal work.
// Everything the terminal owns happens here; the settings step goes back up to
// `terminalApp.applyPanel`, which is the only place that can fan it out to
// every tab.
func (tab *terminalTab) applyUIActions(result ui.Actions) (bool, settingsStep, error) {
	if result.ResetSearch {
		tab.closeSearch()
	}
	if result.QueryChanged {
		if err := tab.runSearch(); err != nil {
			return true, settingsStep{}, err
		}
	}
	if result.MatchStep != 0 {
		direction := gostty.SearchDirectionNext
		if result.MatchStep < 0 {
			direction = gostty.SearchDirectionPrev
		}
		if err := tab.moveMatch(direction); err != nil {
			return true, settingsStep{}, err
		}
	}
	if result.SettingsDelta != 0 {
		return true, settingsStep{row: tab.panels.Settings.Row, delta: result.SettingsDelta}, nil
	}
	return result.Consumed, settingsStep{}, nil
}

func (tab *terminalTab) currentTheme() ui.Theme { return ui.ThemeAt(tab.settings.theme) }
func (tab *terminalTab) colorScheme() gostty.ColorScheme {
	if tab.currentTheme().Light(tab.terminalBg) {
		return gostty.ColorSchemeLight
	}
	return gostty.ColorSchemeDark
}
func (tab *terminalTab) themeColor(c color.RGBA) color.RGBA {
	return tab.currentTheme().ResolveColor(c, tab.terminalBg, tab.terminalFg)
}

func (tab *terminalTab) canvas(screen *ebiten.Image) ui.Canvas {
	return ui.Canvas{
		Screen: screen, CellWidth: tab.fonts().CellWidth, CellHeight: tab.fonts().CellHeight,
		Width: float64(tab.cols) * tab.fonts().CellWidth, Height: float64(tab.rows) * tab.fonts().CellHeight,
		Scale: tab.settings.dsf, Theme: tab.currentTheme(), DrawText: tab.drawText, RuneWidth: runeWidth,
	}
}

// drawUI paints whichever panel is open. The labels come from the window, since
// what they describe -- the font, the theme, the cat -- is the window's.
func (tab *terminalTab) drawUI(screen *ebiten.Image, values ui.SettingsValues) {
	tab.panels.Draw(tab.canvas(screen), values)
}

// drawText writes a line in the grid's own cell width, so the panels line up
// with the terminal behind them, and returns where it ended.
func (tab *terminalTab) drawText(screen *ebiten.Image, s string, x, y float64, fg color.RGBA) float64 {
	for _, r := range s {
		wide := runeWidth(r) == 2
		tab.glyph(screen, r, x, y, wide, false, false, fg)
		x += tab.fonts().CellWidth
		if wide {
			x += tab.fonts().CellWidth
		}
	}
	return x
}

// runeWidth asks the binding how many columns a rune takes, which is the same
// answer the terminal used when it laid the grid out.
func runeWidth(r rune) int {
	w, err := gostty.CodepointWidth(r)
	if err != nil {
		return 1
	}
	return int(w)
}
