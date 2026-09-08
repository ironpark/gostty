package main

import (
	"fmt"
	"image/color"

	"github.com/ironpark/gostty"
)

// refresh pulls the viewport out of the render state. This is the only place
// cell data crosses the boundary, and it is one call for the whole grid.
func (tab *terminalTab) refresh() error {
	if err := tab.state.Update(tab.vt); err != nil {
		return fmt.Errorf("render update: %w", err)
	}
	n, err := tab.state.CellCount()
	if err != nil {
		return err
	}
	if uint(cap(tab.cells)) < n {
		tab.cells = make([]gostty.RenderCell, n)
	}
	tab.cells = tab.cells[:n]
	if _, err := tab.state.Cells(tab.cells); err != nil {
		return fmt.Errorf("render cells: %w", err)
	}
	if err := tab.tickSearch(); err != nil {
		return err
	}
	if err := tab.refreshMatches(); err != nil {
		return err
	}
	if err := tab.refreshDirty(); err != nil {
		return err
	}
	if err := tab.refreshImages(); err != nil {
		return err
	}
	prevBg, prevFg := tab.bg, tab.fg
	bg, err := tab.state.Background()
	if err != nil {
		return err
	}
	tab.terminalBg = rgb(bg)
	fg, err := tab.state.Foreground()
	if err != nil {
		return err
	}
	tab.terminalFg = rgb(fg)
	theme := tab.currentTheme()
	if theme.Terminal {
		tab.bg, tab.fg = tab.terminalBg, tab.terminalFg
	} else {
		tab.bg, tab.fg = theme.Background, theme.Foreground
	}
	if tab.bg != prevBg || tab.fg != prevFg {
		tab.redrawAll = true
	}
	return tab.refreshCursor()
}

// refreshDirty takes the render state's dirty rows into the redraw set and
// marks the state clean, so the next Update reports only what changes from
// here. Full dirt -- colors or size changed -- redraws every row.
func (tab *terminalTab) refreshDirty() error {
	if len(tab.rowDirty) != tab.rows {
		tab.rowDirty = make([]bool, tab.rows)
		tab.dirtyRows = make([]uint16, tab.rows)
		tab.redrawAll = true
	}
	dirty, err := tab.state.Dirty()
	if err != nil {
		return err
	}
	if dirty == gostty.RenderDirtyFull {
		tab.redrawAll = true
	}
	n, err := tab.state.DirtyRows(tab.dirtyRows)
	if err != nil {
		return err
	}
	for _, y := range tab.dirtyRows[:n] {
		if int(y) < len(tab.rowDirty) {
			tab.rowDirty[y] = true
		}
	}
	return tab.state.Clean()
}

// refreshCursor reads the cursor out of the render state, so Draw does not have
// to reach across the boundary from a place that cannot report a failure.
func (tab *terminalTab) refreshCursor() error {
	visible, err := tab.state.CursorVisible()
	if err != nil {
		return err
	}
	x, onScreen, err := tab.state.CursorX()
	if err != nil {
		return err
	}
	y, _, err := tab.state.CursorY()
	if err != nil {
		return err
	}
	style, err := tab.state.CursorStyle()
	if err != nil {
		return err
	}
	// `onScreen` is false when the viewport has been scrolled away from it.
	tab.cursor = cursorState{x: x, y: y, visible: visible && onScreen, style: style}
	return nil
}

func rgb(v uint32) color.RGBA {
	return color.RGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xff}
}
