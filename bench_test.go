package gostty

import "testing"

func BenchmarkCodepointWidth(b *testing.B) {
	for b.Loop() {
		_, _ = CodepointWidth('A')
	}
}

func BenchmarkTerminalCols(b *testing.B) {
	term, _ := NewTerminal(80, 24)
	defer term.Close()
	b.ResetTimer()
	for b.Loop() {
		_ = term.Cols()
	}
}

// Empty input isolates the boundary from any work the terminal does.
func BenchmarkFeedEmpty(b *testing.B) {
	term, _ := NewTerminal(80, 24)
	stream, _ := term.NewStream(0)
	defer func() { stream.Close(); term.Close() }()
	var data []byte
	b.ResetTimer()
	for b.Loop() {
		_, _ = stream.Write(data)
	}
}

// One printable line into a terminal with scrollback disabled.
func BenchmarkFeedLine(b *testing.B) {
	term, _ := NewTerminal(80, 24)
	stream, _ := term.NewStream(0)
	defer func() { stream.Close(); term.Close() }()
	term.SetScrollbackMaxBytes(0)
	data := []byte("hello \x1b[31mworld\x1b[0m\r\n")
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for b.Loop() {
		_, _ = stream.Write(data)
	}
}

// CarriageReturn returns nothing: no out parameter, no value to marshal.
func BenchmarkCarriageReturn(b *testing.B) {
	term, _ := NewTerminal(80, 24)
	defer term.Close()
	b.ResetTimer()
	for b.Loop() {
		term.CarriageReturn()
	}
}

// A 200x60 frame's cells, whole and one row at a time.
func BenchmarkRenderCells(b *testing.B) {
	term, _ := NewTerminal(200, 60)
	defer term.Close()
	state, _ := NewRenderState()
	defer state.Close()
	_ = state.Update(term)
	cells := make([]RenderCell, state.CellCount())
	row := make([]RenderCell, 200)
	b.Run("Cells", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_, _ = state.Cells(cells)
		}
	})
	b.Run("RowCells", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_, _ = state.RowCells(3, row)
		}
	})
}
