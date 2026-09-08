package main

import (
	"log"

	"github.com/creack/pty"
	"github.com/hajimehoshi/ebiten/v2"
)

// layout resizes a tab to the content area below the tab bar.
func (tab *terminalTab) layout(width, height, dsf float64) {
	if dsf != tab.dsf {
		tab.dsf = dsf
		tab.applyFont()
	}
	cols := max(int(width/tab.fonts.CellWidth), 1)
	rows := max(int(height/tab.fonts.CellHeight), 1)
	if cols != tab.cols || rows != tab.rows || tab.relayout {
		if err := tab.resize(cols, rows); err != nil {
			log.Printf("resize to %dx%d: %v", cols, rows, err)
			return
		}
		tab.relayout = false
	}
}

func (tab *terminalTab) cursorPosition() (int, int) {
	x, y := ebiten.CursorPosition()
	return x, y - tab.offsetY
}

// resize moves the emulated screen and the pty together. They have to agree:
// the program asks the pty how big it is and writes for the terminal.
func (tab *terminalTab) resize(cols, rows int) error {
	tab.cols, tab.rows = cols, rows
	cellW, cellH := uint16(tab.fonts.CellWidth), uint16(tab.fonts.CellHeight)
	// `ResizeCells` rather than `Resize`: a Kitty image sized in cells is
	// measured in pixels through the cell size, and the terminal stores the
	// pixel size of the whole grid, so it goes stale on every column change.
	if err := tab.vt.ResizeCells(uint16(cols), uint16(rows), uint32(cellW), uint32(cellH)); err != nil {
		return err
	}
	// The pty carries the same two sizes, which is where a program that has not
	// asked the terminal directly reads them from.
	return pty.Setsize(tab.shell.pty, tab.ptySize())
}

// deviceScale is how many pixels the display has per device-independent pixel.
//
// One before there is a window to ask about, which is the case for the first
// font load: the first LayoutF picks up the real answer and reloads, so the
// only cost of guessing is one frame at the wrong size.
func deviceScale() float64 {
	monitor := ebiten.Monitor()
	if monitor == nil {
		return 1
	}
	if scale := monitor.DeviceScaleFactor(); scale > 0 {
		return scale
	}
	return 1
}

// ptySize uses the same cell metrics at startup and on window changes.
func (tab *terminalTab) ptySize() *pty.Winsize {
	return &pty.Winsize{
		Cols: uint16(tab.cols), Rows: uint16(tab.rows),
		X: uint16(tab.fonts.CellWidth) * uint16(tab.cols),
		Y: uint16(tab.fonts.CellHeight) * uint16(tab.rows),
	}
}
