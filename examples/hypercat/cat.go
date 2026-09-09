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
		HasInk: tab.catCell,
	}
}

func (tab *terminalTab) catCell(col, row int) bool {
	if len(tab.frame.cells) < tab.rows*tab.cols {
		return false
	}
	cell := tab.frame.cells[row*tab.cols+col]
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
