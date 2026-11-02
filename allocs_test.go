package gostty

import (
	"strings"
	"testing"
)

// A renderer calls these every frame, so they must not allocate: a 200x60
// viewport's Cells once took a fresh 192 KB scratch on every call.
func TestRenderFrameAllocs(t *testing.T) {
	if testing.Short() {
		t.Skip("allocation counts are measured in the full run")
	}
	const cols, rows = 200, 60
	term, stream := newStreamPair(t, cols, rows)
	state, err := NewRenderState()
	if err != nil {
		t.Fatalf("NewRenderState: %v", err)
	}
	defer state.Close()
	feed(t, stream, strings.Repeat("\x1b[31;1mhello\x1b[m world ", 400))
	if err := state.Update(term); err != nil {
		t.Fatalf("Update: %v", err)
	}
	cells := make([]RenderCell, state.CellCount())
	row := make([]RenderCell, cols)
	dirty := make([]uint16, rows)

	for _, c := range []struct {
		name string
		call func()
	}{
		{"Update", func() { _ = state.Update(term) }},
		{"Cells", func() { _, _ = state.Cells(cells) }},
		{"RowCells", func() { _, _ = state.RowCells(3, row) }},
		{"DirtyRows", func() { _, _ = state.DirtyRows(dirty) }},
		{"Rows+Cols", func() { _, _ = state.Rows(), state.Cols() }},
		{"Dirty", func() { _ = state.Dirty() }},
		{"Cursor", func() { _ = state.Cursor() }},
		{"Colors", func() { _ = state.Colors() }},
		{"Clean", func() { state.Clean() }},
		{"Terminal.Cols", func() { _ = term.Cols() }},
	} {
		c.call() // grows the scratch the first call needs
		if got := testing.AllocsPerRun(100, c.call); got != 0 {
			t.Errorf("%s: %v allocations per call, want 0", c.name, got)
		}
	}

	if written, err := state.Cells(cells); err != nil || written != uint(len(cells)) {
		t.Fatalf("Cells = %d, %v; want %d", written, err, len(cells))
	}
	if cells[0].Codepoint != 'h' || !cells[0].Flags.Bold {
		t.Errorf("cell 0 = %+v, want a bold 'h'", cells[0])
	}
}

// ReleaseScratch drops the buffer Cells kept, and the next call takes a
// new one rather than failing.
func TestRenderStateReleaseScratch(t *testing.T) {
	term, stream := newStreamPair(t, 10, 3)
	state, err := NewRenderState()
	if err != nil {
		t.Fatalf("NewRenderState: %v", err)
	}
	defer state.Close()
	feed(t, stream, "hi")
	if err := state.Update(term); err != nil {
		t.Fatalf("Update: %v", err)
	}
	cells := make([]RenderCell, state.CellCount())
	for range 2 {
		if _, err := state.Cells(cells); err != nil {
			t.Fatalf("Cells: %v", err)
		}
		if cells[0].Codepoint != 'h' || cells[1].Codepoint != 'i' {
			t.Fatalf("row 0 = %q%q, want \"hi\"", cells[0].Codepoint, cells[1].Codepoint)
		}
		state.ReleaseScratch()
		clear(cells)
	}
}
