package gostty

import (
	"bytes"
	"testing"
)

// A snapshot round-trips the screen, scrollback and an unfinished sequence.
func TestSnapshotRoundTrip(t *testing.T) {
	term, err := NewTerminal(10, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	// Continuation tracking is opt-in: a positive cap turns it on.
	stream, err := term.NewStream(64)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	// Three lines into a two-row terminal: one goes to scrollback. Then the
	// start of an escape sequence, left unfinished.
	feed(t, stream, "one\r\ntwo\r\nthree\x1b[3")

	var snap bytes.Buffer
	if err := stream.WriteSnapshot(&snap); err != nil {
		t.Fatal(err)
	}
	if snap.Len() == 0 {
		t.Fatal("WriteSnapshot wrote nothing")
	}

	decoded, err := DecodeSnapshot(bytes.NewReader(snap.Bytes()), 1024)
	if err != nil {
		t.Fatal(err)
	}
	defer decoded.Close()
	if cont, err := decoded.Continuation(); err != nil || string(cont) != "\x1b[3" {
		t.Errorf("Continuation() = %q, %v; want the unfinished CSI", cont, err)
	}

	restored, err := NewTerminal(80, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if err := decoded.RestoreInto(restored); err != nil {
		t.Fatal(err)
	}
	if err := decoded.RestoreInto(restored); err == nil {
		t.Error("second RestoreInto succeeded")
	}
	if cols, _ := restored.Cols(); cols != 10 {
		t.Errorf("restored Cols() = %d, want 10", cols)
	}
	if got, err := restored.PlainString(); err != nil || got != "two\nthree" {
		t.Errorf("restored PlainString() = %q, %v", got, err)
	}
	if got, err := restored.HistoryString(); err != nil || got != "one" {
		t.Errorf("restored HistoryString() = %q, %v", got, err)
	}

	// Resume: a new stream takes the continuation, then the rest of the
	// sequence -- `\x1b[3D` moves the cursor left three -- lands whole.
	rs, err := restored.NewStream(0)
	if err != nil {
		t.Fatal(err)
	}
	defer rs.Close()
	cont, _ := decoded.Continuation()
	if err := rs.Feed(cont); err != nil {
		t.Fatal(err)
	}
	if err := rs.Feed([]byte("DX")); err != nil {
		t.Fatal(err)
	}
	if got, _ := restored.PlainString(); got != "two\nthXee" {
		t.Errorf("after resume PlainString() = %q, want %q", got, "two\nthXee")
	}
}
