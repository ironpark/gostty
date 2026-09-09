package main

import (
	"image/color"
	"log"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/ironpark/gostty"
	"github.com/ironpark/gostty/examples/hypercat/fonts"
	"github.com/ironpark/gostty/examples/hypercat/keys"
	"github.com/ironpark/gostty/examples/hypercat/shell"
	"github.com/ironpark/gostty/examples/hypercat/thecat"
	"github.com/ironpark/gostty/examples/hypercat/ui"
)

// terminalTab owns one terminal, shell, and viewport. Shared appearance and
// clipboard come from the app; UI actions flow back without an app reference.
type terminalTab struct {
	offsetY int

	// The terminal side.
	vt     *gostty.Terminal
	stream *gostty.Stream
	state  *gostty.RenderState
	// What the last refresh read out of it, which is all Draw may look at.
	frame frame
	// Kitty graphics: the placements for this frame and their textures.
	images *imageCache
	// The scrollback search and the cells it highlights.
	search tabSearch

	// Partial redraw: the layers the grid is drawn into.
	layers gridCanvas

	// The process side.
	shell *shell.Session

	// The pixel side.
	cols, rows int
	bell       int

	// What the program says it is, for the tab label and the window title.
	title windowTitle

	// The window's clipboard, shared with every other tab.
	clipboard *sharedClipboard

	sel selection

	// The search bar and the settings panel, which take the keyboard while
	// they are open.
	panels ui.Panels
	// The window's fonts, theme, and cat mode, shared with every other tab so
	// that a change made in the settings panel is a change to all of them.
	settings *appearance
	// Set when the faces changed, so Layout redoes the grid even if the window
	// did not move.
	relayout bool

	// The cat that walks on this tab's output.
	cat *thecat.Companion

	// Mouse and focus reporting: what the program is told about the pointer,
	// as opposed to what the window does with it itself.
	reports reportState

	// Per-frame input scratch, reused so a keystroke allocates nothing. The
	// reader answers for the shell; `chars` is the search bar's, which wants
	// the runes themselves rather than key events. `out` is what the keys
	// encode to, `report` what the mouse and focus do.
	keys        keys.Reader
	chars       []rune
	out, report frameBuffer
}

// onScreen borrows the currently active screen for one operation.
// Fetch it each time because programs can switch between primary and alternate screens.
func (tab *terminalTab) onScreen(f func(*gostty.Screen) error) error {
	screen, err := tab.vt.ActiveScreen()
	if err != nil {
		return err
	}
	return f(screen)
}

// fonts and emoji are the window's, shared with every other tab: what a tab
// draws with follows the settings panel wherever it was opened.
func (tab *terminalTab) fonts() *fonts.Set   { return tab.settings.fonts }
func (tab *terminalTab) emoji() *fonts.Emoji { return tab.settings.emoji }

// Tab switches reset interaction state and invalidate the visible grid.
func (tab *terminalTab) activate() {
	tab.reports.focusedFrames = 0
	tab.frame.redraw.markAll()
}

func (tab *terminalTab) deactivate() {
	tab.endGesture()
	tab.reports.mouseGrabbed = false
	tab.cat.ClearHover()
}

// updateInput routes pointer and keyboard input, then refreshes the frame.
// An open panel consumes the keyboard while selection remains available.
func (tab *terminalTab) updateInput(m keys.Mods, panelTook bool) error {
	if tab.reports.focused {
		// The pointer above the grid is the tab bar's; a drag released there
		// ends. A click on the cat is the cat's, not the shell's: it must not
		// also start a selection or be reported to the program.
		_, y := tab.cursorPosition()
		onGrid := y >= 0
		if !onGrid && !ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft) {
			tab.sel.dragging = false
		}
		if onGrid && !tab.pokeCat() {
			if err := tab.handleMouse(m); err != nil {
				return err
			}
		}
		if onGrid {
			if err := tab.handleWheel(m); err != nil {
				return err
			}
			// A drop lands on the tab under the pointer, whichever half of the
			// conversation takes it.
			if err := tab.handleDrop(); err != nil {
				return err
			}
		}
		if !panelTook {
			if err := tab.handleInput(m); err != nil {
				return err
			}
		}
	}
	// Input before the refresh, so a selection made this frame is drawn this
	// frame rather than one behind.
	if err := tab.refresh(); err != nil {
		return err
	}
	// After the refresh: the cat walks on the cells this frame is about to
	// draw, not the ones the last frame drew.
	tab.updateCat()
	return nil
}

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

// The terminal only supplies occupied cells and connects cat clicks to settings.
func (tab *terminalTab) catGrid() thecat.GridWorld {
	g := tab.grid()
	return thecat.GridWorld{
		Cols: g.cols, Rows: g.rows,
		CellWidth: g.cellW, CellHeight: g.cellH,
		// The grid is captured rather than rebuilt, because this is asked per
		// cell as the cat looks for ground to walk on.
		HasInk: func(col, row int) bool { return tab.catCell(g, col, row) },
	}
}

func (tab *terminalTab) catCell(g grid, col, row int) bool {
	if !g.holds(len(tab.frame.cells)) {
		return false
	}
	cell := tab.frame.cells[g.index(col, row)]
	return cell.Codepoint > ' ' && !cell.Flags.Invisible
}

// startCat gives the tab a companion in the mode the window is set to, so a
// tab opened after the mode was changed does not start in the old one.
func (tab *terminalTab) startCat() {
	cat, err := thecat.NewCompanion(tab.catGrid())
	if err != nil {
		log.Printf("no cat: %v", err)
		return
	}
	tab.cat = cat
	tab.cat.SetMode(tab.settings.cat)
}

func (tab *terminalTab) updateCat() {
	x, y := tab.cursorPosition()
	tab.cat.Update(tab.catGrid(), x, y, 1.0/float64(ebiten.TPS()))
}

// pokeCat reports whether this frame's click landed on the cat, which opens
// the settings rather than starting a selection.
func (tab *terminalTab) pokeCat() bool {
	if !inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft) {
		return false
	}
	x, y := tab.cursorPosition()
	if !tab.cat.PokeAt(x, y) {
		return false
	}
	tab.panels.OpenSettings()
	return true
}

func (tab *terminalTab) drawCat(screen *ebiten.Image) {
	tab.cat.Draw(screen, tab.currentTheme().Accent)
}
