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
	if tab.panels.Mode == ui.Search {
		tab.chars = ebiten.AppendInputChars(tab.chars[:0])
	} else {
		tab.chars = tab.chars[:0]
	}
	return ui.Input{
		OpenSearch:   m.Shortcut() && inpututil.IsKeyJustPressed(ebiten.KeyF),
		OpenSettings: m.Shortcut() && inpututil.IsKeyJustPressed(ebiten.KeyComma),
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

// applyUIActions does the half of a panel result that is the terminal's: the
// native search. A settings step is left for `terminalApp.applyPanel`, which is
// the only place that can fan it out to every tab.
func (tab *terminalTab) applyUIActions(result ui.Actions) (bool, error) {
	if result.ResetSearch {
		tab.closeSearch()
	}
	if result.QueryChanged {
		if err := tab.runSearch(); err != nil {
			return true, err
		}
	}
	if result.MatchStep != 0 {
		direction := gostty.SearchDirectionNext
		if result.MatchStep < 0 {
			direction = gostty.SearchDirectionPrev
		}
		if err := tab.moveMatch(direction); err != nil {
			return true, err
		}
	}
	return result.Consumed, nil
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

func (tab *terminalTab) canvas(screen *ebiten.Image) ui.Canvas {
	g := tab.grid()
	return ui.Canvas{
		Screen: screen, CellWidth: g.cellW, CellHeight: g.cellH,
		Width: g.width(), Height: g.height(),
		Scale: tab.settings.dsf, Theme: tab.currentTheme(), DrawText: tab.drawText, RuneWidth: runeWidth,
	}
}

// drawText writes a line in the grid's own cell width, so the panels line up
// with the terminal behind them, and returns where it ended.
func (tab *terminalTab) drawText(screen *ebiten.Image, s string, x, y float64, fg color.RGBA) float64 {
	cellW := tab.grid().cellW
	for _, r := range s {
		wide := runeWidth(r) == 2
		tab.glyph(screen, glyphString(r), x, y, wide, false, false, fg)
		x += cellW
		if wide {
			x += cellW
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
