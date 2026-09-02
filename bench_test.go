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
		_, _ = term.Cols()
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
		_ = stream.Feed(data)
	}
}

// One printable line into a terminal with scrollback disabled.
func BenchmarkFeedLine(b *testing.B) {
	term, _ := NewTerminal(80, 24)
	stream, _ := term.NewStream(0)
	defer func() { stream.Close(); term.Close() }()
	zero := uint(0)
	_ = term.SetScrollbackMaxBytes(&zero)
	data := []byte("hello \x1b[31mworld\x1b[0m\r\n")
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for b.Loop() {
		_ = stream.Feed(data)
	}
}

// CarriageReturn returns only an error: no out parameter, no value to marshal.
func BenchmarkCarriageReturn(b *testing.B) {
	term, _ := NewTerminal(80, 24)
	defer term.Close()
	b.ResetTimer()
	for b.Loop() {
		_ = term.CarriageReturn()
	}
}
