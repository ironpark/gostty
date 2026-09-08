package main

import (
	"log"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/ironpark/gostty/examples/hypercat/thecat"
)

// The terminal only supplies occupied cells and connects cat clicks to settings.
func (tab *terminalTab) catGrid() thecat.GridWorld {
	return thecat.GridWorld{
		Cols: tab.cols, Rows: tab.rows,
		CellWidth: tab.fonts.CellWidth, CellHeight: tab.fonts.CellHeight,
		HasInk: tab.catCell,
	}
}

func (tab *terminalTab) catCell(col, row int) bool {
	if len(tab.cells) < tab.rows*tab.cols {
		return false
	}
	cell := tab.cells[row*tab.cols+col]
	return cell.Codepoint > ' ' && !cell.Flags.Invisible
}

func (tab *terminalTab) startCat() {
	cat, err := thecat.NewCompanion(tab.catGrid())
	if err != nil {
		log.Printf("no cat: %v", err)
		return
	}
	tab.cat = cat
}

func (tab *terminalTab) updateCat() {
	x, y := tab.cursorPosition()
	tab.cat.Update(tab.catGrid(), x, y, 1.0/float64(ebiten.TPS()))
}

func (tab *terminalTab) pokeCat() bool {
	if !inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft) {
		return false
	}
	x, y := tab.cursorPosition()
	if !tab.cat.PokeAt(x, y) {
		return false
	}
	_ = tab.openSettings()
	return true
}

func (tab *terminalTab) drawCat(screen *ebiten.Image) {
	tab.cat.Draw(screen, tab.currentTheme().Accent)
}
