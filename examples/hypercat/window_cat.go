package main

import (
	"log"

	"github.com/ironpark/gostty/examples/hypercat/thecat"
)

// The active terminal supplies occupied cells to the window's companion.
func (tab *terminal) catGrid() thecat.GridWorld {
	g := tab.grid()
	return thecat.GridWorld{
		Cols: g.cols, Rows: g.rows,
		CellWidth: g.cellW, CellHeight: g.cellH,
		// The grid is captured rather than rebuilt, because this is asked per
		// cell as the cat looks for ground to walk on.
		HasInk: func(col, row int) bool { return tab.catCell(g, col, row) },
	}
}

func (tab *terminal) catCell(g grid, col, row int) bool {
	if !g.holds(len(tab.frame.cells)) {
		return false
	}
	cell := tab.frame.cells[g.index(col, row)]
	return cell.Codepoint > ' ' && !cell.Flags.Invisible
}

// startCat creates the window's companion after the first terminal opens.
func (win *window) startCat() {
	tab := win.current()
	if tab == nil || win.cat != nil {
		return
	}
	cat, err := thecat.NewCompanion(tab.catGrid())
	if err != nil {
		log.Printf("no cat: %v", err)
		return
	}
	win.cat = cat
	win.cat.SetMode(win.settings.CatMode())
}

// syncCatWorld follows the visible tab without resetting position or animation.
// Clearing the world when the last tab closes releases its cached cells too.
func (win *window) syncCatWorld() {
	if win.cat == nil {
		return
	}
	if tab := win.current(); tab != nil {
		win.cat.GridWorld = tab.catGrid()
	} else {
		win.cat.GridWorld = thecat.GridWorld{}
	}
}

func (win *window) updateCat() {
	tab := win.current()
	x, y := tab.cursorPosition()
	win.cat.Update(tab.catGrid(), x, y, win.input.DeltaSeconds)
}

// pokeCat opens settings in the active tab without starting a selection.
func (win *window) pokeCat() bool {
	if !win.input.Left.Pressed {
		return false
	}
	tab := win.current()
	x, y := tab.cursorPosition()
	if y < 0 || !win.cat.PokeAt(x, y) {
		return false
	}
	tab.panels.OpenSettings()
	return true
}
