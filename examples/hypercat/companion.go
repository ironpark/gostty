package main

import (
	"log"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/ironpark/gostty/examples/hypercat/thecat"
)

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
