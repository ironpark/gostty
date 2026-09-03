package gostty

import (
	"errors"
	"strings"
	"testing"
)

// newStreamPair returns a terminal and a stream feeding it. The stream is
// closed first, as ghostty's handler reaches through the terminal on deinit.
// The binding declares that relationship, so closing them in the other order
// is refused rather than undefined.
func newStreamPair(t *testing.T, cols, rows uint16) (*Terminal, *Stream) {
	t.Helper()
	term, err := NewTerminal(cols, rows)
	if err != nil {
		t.Fatalf("NewTerminal: %v", err)
	}
	stream, err := term.NewStream(0)
	if err != nil {
		term.Close()
		t.Fatalf("NewStream: %v", err)
	}
	t.Cleanup(func() {
		stream.Close()
		term.Close()
	})
	return term, stream
}

func feed(t *testing.T, s *Stream, data string) {
	t.Helper()
	if err := s.Feed([]byte(data)); err != nil {
		t.Fatalf("Feed(%q): %v", data, err)
	}
}

func TestStreamPlainText(t *testing.T) {
	term, stream := newStreamPair(t, 20, 3)
	feed(t, stream, "hello\r\nworld")

	got, err := term.PlainString()
	if err != nil {
		t.Fatalf("PlainString: %v", err)
	}
	if want := "hello\nworld"; strings.TrimRight(got, "\n") != want {
		t.Errorf("PlainString() = %q, want %q", got, want)
	}
}

func TestStreamCursorPosition(t *testing.T) {
	term, stream := newStreamPair(t, 20, 5)
	// CUP: move to row 3, column 7 (1-based).
	feed(t, stream, "\x1b[3;7H")

	x, err := term.CursorX()
	if err != nil {
		t.Fatalf("CursorX: %v", err)
	}
	y, err := term.CursorY()
	if err != nil {
		t.Fatalf("CursorY: %v", err)
	}
	if x != 6 || y != 2 {
		t.Errorf("cursor = (%d,%d), want (6,2)", x, y)
	}
}

func TestStreamSGRIsParsedNotPrinted(t *testing.T) {
	term, stream := newStreamPair(t, 20, 2)
	feed(t, stream, "\x1b[31mred\x1b[0m")

	got, err := term.PlainString()
	if err != nil {
		t.Fatalf("PlainString: %v", err)
	}
	if want := "red"; strings.TrimSpace(got) != want {
		t.Errorf("PlainString() = %q, want %q", got, want)
	}
}

func TestStreamEraseDisplay(t *testing.T) {
	term, stream := newStreamPair(t, 20, 3)
	feed(t, stream, "dirty")
	// ED 2: erase the whole display.
	feed(t, stream, "\x1b[2J")

	got, err := term.PlainString()
	if err != nil {
		t.Fatalf("PlainString: %v", err)
	}
	if strings.TrimSpace(got) != "" {
		t.Errorf("PlainString() after ED 2 = %q, want empty", got)
	}
}

// Parser state must survive across Feed calls, which is the reason the stream
// is a handle rather than a per-call helper.
func TestStreamSplitEscapeSequence(t *testing.T) {
	term, stream := newStreamPair(t, 20, 5)
	feed(t, stream, "\x1b[3")
	feed(t, stream, ";7H")

	x, err := term.CursorX()
	if err != nil {
		t.Fatalf("CursorX: %v", err)
	}
	y, err := term.CursorY()
	if err != nil {
		t.Fatalf("CursorY: %v", err)
	}
	if x != 6 || y != 2 {
		t.Errorf("cursor = (%d,%d), want (6,2); parser state lost across Feed", x, y)
	}
}

func TestStreamDECSCUSR(t *testing.T) {
	term, stream := newStreamPair(t, 10, 2)
	// DECSCUSR 3: blinking underline.
	feed(t, stream, "\x1b[3 q")

	got, err := term.CursorStyle()
	if err != nil {
		t.Fatalf("CursorStyle: %v", err)
	}
	if got != CursorStyleUnderline {
		t.Errorf("CursorStyle() = %v, want %v", got, CursorStyleUnderline)
	}
}

func TestStreamNoSemanticFailure(t *testing.T) {
	_, stream := newStreamPair(t, 20, 3)
	feed(t, stream, "\x1b[31mhello\x1b[0m\r\n\x1b[2Jworld")

	failed, err := stream.Failed()
	if err != nil {
		t.Fatalf("Failed: %v", err)
	}
	if failed {
		t.Error("stream reported a semantic failure on well-formed input")
	}
}

// The stream is a child handle of its terminal, so the terminal cannot be
// closed while the stream is open.
func TestStreamKeepsTerminalOpen(t *testing.T) {
	term, err := NewTerminal(10, 2)
	if err != nil {
		t.Fatalf("NewTerminal: %v", err)
	}
	stream, err := term.NewStream(0)
	if err != nil {
		term.Close()
		t.Fatalf("NewStream: %v", err)
	}

	err = term.Close()
	if !errors.Is(err, ErrHandleInUse) {
		t.Fatalf("Close with an open stream = %v, want ErrHandleInUse", err)
	}
	var inUse *HandleInUseError
	if !errors.As(err, &inUse) {
		t.Fatalf("error is not *HandleInUseError: %v", err)
	}

	// The terminal is still usable after the refusal.
	if _, err := term.Cols(); err != nil {
		t.Errorf("Cols() after a refused Close: %v", err)
	}

	if err := stream.Close(); err != nil {
		t.Fatalf("stream Close: %v", err)
	}
	if err := term.Close(); err != nil {
		t.Fatalf("terminal Close after the stream closed: %v", err)
	}
}
