package gostty

import (
	"bytes"
	"testing"
)

// WriteContinuation takes an io.Writer, which zigo adapts to a *std.Io.Writer
// on the Zig side. The stream must be built with continuation tracking on.
func TestWriteContinuation(t *testing.T) {
	term, err := NewTerminal(20, 3)
	if err != nil {
		t.Fatalf("NewTerminal: %v", err)
	}
	stream, err := term.NewStream(4096)
	if err != nil {
		term.Close()
		t.Fatalf("NewStream: %v", err)
	}
	t.Cleanup(func() {
		stream.Close()
		term.Close()
	})

	// Leave the parser mid-sequence.
	if err := stream.Feed([]byte("ok\x1b[3")); err != nil {
		t.Fatalf("Feed: %v", err)
	}

	var buf bytes.Buffer
	if err := stream.WriteContinuation(&buf); err != nil {
		t.Fatalf("WriteContinuation: %v", err)
	}
	if !bytes.Contains(buf.Bytes(), []byte("\x1b[3")) {
		t.Errorf("continuation = %q, want it to carry the unfinished sequence", buf.Bytes())
	}
	if bytes.Contains(buf.Bytes(), []byte("ok")) {
		t.Errorf("continuation = %q, should not repeat committed output", buf.Bytes())
	}
}
