package main

import "testing"

func TestRedrawSetFollowsTheGrid(t *testing.T) {
	var r redrawSet
	r.resize(3)
	if !r.all {
		t.Fatal("a new grid should be redrawn whole")
	}
	r.clear()
	r.mark(1)
	r.mark(7) // off the grid: ignored rather than a panic
	r.mark(-1)
	if r.all || !r.rows[1] || r.rows[0] || r.rows[2] {
		t.Fatalf("rows = %v all = %v", r.rows, r.all)
	}
	r.resize(3) // same size keeps its marks
	if r.all || !r.take(1) || r.take(1) {
		t.Fatal("resizing to the same size lost the marks, or take did not claim its row")
	}
	r.resize(4)
	if !r.all || len(r.rows) != 4 || len(r.scratch) != 4 {
		t.Fatal("resizing did not follow the grid")
	}
}

func TestMatchChangesMarkOnlyTheRowsThatChanged(t *testing.T) {
	tab := &terminalTab{cols: 4, rows: 3}
	tab.redraw.resize(3)
	tab.redraw.clear()
	prev := []bool{false, false, false, false, true, false, false, false, false, false, false, false}
	tab.matchCells = []bool{false, false, false, false, true, false, false, false, false, false, true, false}
	tab.markMatchChanges(prev)
	if tab.redraw.rows[0] || tab.redraw.rows[1] || !tab.redraw.rows[2] {
		t.Fatalf("rows = %v, want only the third", tab.redraw.rows)
	}
	// A cleared search leaves a shorter (empty) set; the old rows still repaint.
	tab.redraw.clear()
	tab.matchCells = nil
	tab.markMatchChanges(prev)
	if tab.redraw.rows[0] || !tab.redraw.rows[1] || tab.redraw.rows[2] {
		t.Fatalf("rows = %v, want only the second", tab.redraw.rows)
	}
}
