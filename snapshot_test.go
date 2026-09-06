package gostty

import (
	"bytes"
	"strings"
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

// The incremental decoder is what the format is shaped for: the active state
// comes first, so a terminal is drawable before its scrollback has been read.
func TestSnapshotDecoderIncremental(t *testing.T) {
	term, err := NewTerminal(10, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	stream, err := term.NewStream(64)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if err := term.SetScrollbackMaxBytes(1 << 20); err != nil {
		t.Fatal(err)
	}
	// SCREEN carries the page the active area starts in; only whole pages
	// before it become HISTORY sequences. So this has to be enough output to
	// fill more than one page.
	for range 20000 {
		feed(t, stream, "line\r\n")
	}
	feed(t, stream, "tail\x1b[3")

	var snap bytes.Buffer
	if err := stream.WriteSnapshot(&snap); err != nil {
		t.Fatal(err)
	}

	dec, err := NewSnapshotDecoder(snap.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()

	restored, err := NewTerminal(80, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()

	// Nothing has been read yet, so history cannot be applied.
	if _, _, err := dec.Next(restored); err == nil {
		t.Error("Next before Ready succeeded")
	}

	if err := dec.Ready(1024); err != nil {
		t.Fatalf("Ready: %v", err)
	}
	if cont, err := dec.Continuation(); err != nil || string(cont) != "\x1b[3" {
		t.Errorf("Continuation() = %q, %v; want the unfinished CSI", cont, err)
	}
	if err := dec.RestoreInto(restored); err != nil {
		t.Fatalf("RestoreInto: %v", err)
	}

	// The terminal is drawable now, with the active area but no scrollback.
	if cols, _ := restored.Cols(); cols != 10 {
		t.Errorf("restored Cols() = %d, want 10", cols)
	}
	if got, err := restored.PlainString(); err != nil || got != "line\ntail" {
		t.Errorf("active area after Ready = %q, %v; want the last two rows", got, err)
	}
	afterReady, err := restored.HistoryString()
	if err != nil {
		t.Fatal(err)
	}

	// Then the scrollback arrives a page at a time, oldest last.
	pages, rows := 0, uint64(0)
	for {
		progress, ok, err := dec.Next(restored)
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		if !ok {
			break
		}
		pages++
		rows += progress.Rows
		if pages > 1000 {
			t.Fatal("Next never finished")
		}
	}
	if pages == 0 {
		t.Fatal("the snapshot carried no history pages")
	}
	if rows == 0 {
		t.Fatal("no history rows were applied")
	}
	// Past the end it just keeps saying it is done.
	if _, ok, err := dec.Next(restored); err != nil || ok {
		t.Errorf("Next after the end = ok %v, %v; want false, nil", ok, err)
	}

	history, err := restored.HistoryString()
	if err != nil {
		t.Fatal(err)
	}
	// Streaming added scrollback above what READY already carried.
	if len(history) <= len(afterReady) {
		t.Errorf("history did not grow: %d bytes after Ready, %d after streaming",
			len(afterReady), len(history))
	}
	if got := strings.Count(history, "line"); got == 0 {
		t.Errorf("history after streaming = %q, want the scrolled-off lines", history)
	}
	// Same content the one-shot decoder produces.
	oneShot, err := NewTerminal(80, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer oneShot.Close()
	decoded, err := DecodeSnapshot(bytes.NewReader(snap.Bytes()), 1024)
	if err != nil {
		t.Fatal(err)
	}
	defer decoded.Close()
	if err := decoded.RestoreInto(oneShot); err != nil {
		t.Fatal(err)
	}
	want, err := oneShot.HistoryString()
	if err != nil {
		t.Fatal(err)
	}
	if history != want {
		t.Errorf("incremental history differs from the one-shot decode:\n got %q\nwant %q", history, want)
	}
}

// Ready is once: a second call would read past the marker it already consumed.
func TestSnapshotDecoderReadyOnce(t *testing.T) {
	term, err := NewTerminal(10, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	stream, err := term.NewStream(0)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	feed(t, stream, "hello")

	var snap bytes.Buffer
	if err := stream.WriteSnapshot(&snap); err != nil {
		t.Fatal(err)
	}
	dec, err := NewSnapshotDecoder(snap.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()
	if err := dec.Ready(0); err != nil {
		t.Fatalf("Ready: %v", err)
	}
	if err := dec.Ready(0); err == nil {
		t.Error("second Ready succeeded")
	}
}
