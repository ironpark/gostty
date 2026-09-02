package gostty

import (
	"errors"
	"strings"
	"testing"
)

func newTerm(t *testing.T, cols, rows uint16) *Terminal {
	t.Helper()
	term, err := NewTerminal(cols, rows)
	if err != nil {
		t.Fatalf("NewTerminal: %v", err)
	}
	t.Cleanup(func() { term.Close() })
	return term
}

func TestTerminalSize(t *testing.T) {
	term := newTerm(t, 80, 24)
	if got, err := term.Cols(); err != nil || got != 80 {
		t.Errorf("Cols() = %d, %v; want 80, nil", got, err)
	}
	if got, err := term.Rows(); err != nil || got != 24 {
		t.Errorf("Rows() = %d, %v; want 24, nil", got, err)
	}
}

func TestPrintStringAndDump(t *testing.T) {
	term := newTerm(t, 20, 3)
	if err := term.PrintString([]byte("hello")); err != nil {
		t.Fatalf("PrintString: %v", err)
	}
	if err := term.CarriageReturn(); err != nil {
		t.Fatalf("CarriageReturn: %v", err)
	}
	if err := term.Linefeed(); err != nil {
		t.Fatalf("Linefeed: %v", err)
	}
	if err := term.PrintString([]byte("world")); err != nil {
		t.Fatalf("PrintString: %v", err)
	}

	got, err := term.PlainString()
	if err != nil {
		t.Fatalf("PlainString: %v", err)
	}
	if want := "hello\nworld"; strings.TrimRight(string(got), "\n") != want {
		t.Errorf("PlainString() = %q, want %q", got, want)
	}

	if x, err := term.CursorX(); err != nil || x != 5 {
		t.Errorf("CursorX() = %d, %v; want 5, nil", x, err)
	}
	if y, err := term.CursorY(); err != nil || y != 1 {
		t.Errorf("CursorY() = %d, %v; want 1, nil", y, err)
	}
}

func TestPrintStringUTF8(t *testing.T) {
	term := newTerm(t, 20, 2)
	if err := term.PrintString([]byte("안녕😀")); err != nil {
		t.Fatalf("PrintString: %v", err)
	}
	got, err := term.PlainString()
	if err != nil {
		t.Fatalf("PlainString: %v", err)
	}
	if !strings.HasPrefix(string(got), "안녕😀") {
		t.Errorf("PlainString() = %q, want prefix %q", got, "안녕😀")
	}
	// Each of these is a wide cell, so the cursor advanced by 6 columns.
	if x, err := term.CursorX(); err != nil || x != 6 {
		t.Errorf("CursorX() = %d, %v; want 6, nil", x, err)
	}
}

func TestCursorStyleRoundTrip(t *testing.T) {
	term := newTerm(t, 10, 2)
	// SetCursorStyle takes a DECSCUSR request; CursorStyle reports the
	// resolved screen style, so the two enums are distinct types.
	for _, tc := range []struct {
		req  CursorStyleReq
		want CursorStyle
	}{
		{CursorStyleReqSteadyBar, CursorStyleBar},
		{CursorStyleReqBlinkingUnderline, CursorStyleUnderline},
		{CursorStyleReqSteadyBlock, CursorStyleBlock},
	} {
		if err := term.SetCursorStyle(tc.req); err != nil {
			t.Fatalf("SetCursorStyle(%v): %v", tc.req, err)
		}
		got, err := term.CursorStyle()
		if err != nil {
			t.Fatalf("CursorStyle: %v", err)
		}
		if got != tc.want {
			t.Errorf("after SetCursorStyle(%v), CursorStyle() = %v, want %v", tc.req, got, tc.want)
		}
	}
	if got := CursorStyleBlockHollow.String(); got != "block_hollow" {
		t.Errorf("String() = %q, want %q", got, "block_hollow")
	}
	if got := CursorStyleReqBlinkingBar.String(); got != "blinking_bar" {
		t.Errorf("String() = %q, want %q", got, "blinking_bar")
	}
}

func TestFullReset(t *testing.T) {
	term := newTerm(t, 10, 2)
	if err := term.PrintString([]byte("dirty")); err != nil {
		t.Fatalf("PrintString: %v", err)
	}
	if err := term.FullReset(); err != nil {
		t.Fatalf("FullReset: %v", err)
	}
	got, err := term.PlainString()
	if err != nil {
		t.Fatalf("PlainString: %v", err)
	}
	if strings.TrimSpace(string(got)) != "" {
		t.Errorf("PlainString() after FullReset = %q, want empty", got)
	}
}

func TestUseAfterClose(t *testing.T) {
	term, err := NewTerminal(10, 2)
	if err != nil {
		t.Fatalf("NewTerminal: %v", err)
	}
	if err := term.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Close is idempotent.
	if err := term.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if _, err := term.Cols(); !errors.Is(err, ErrInvalidHandle) {
		t.Errorf("Cols() after Close = %v, want ErrInvalidHandle", err)
	}
}

func TestBackspace(t *testing.T) {
	term := newTerm(t, 10, 2)
	if err := term.PrintString([]byte("abc")); err != nil {
		t.Fatalf("PrintString: %v", err)
	}
	if err := term.Backspace(); err != nil {
		t.Fatalf("Backspace: %v", err)
	}
	if err := term.PrintString([]byte("X")); err != nil {
		t.Fatalf("PrintString: %v", err)
	}
	got, err := term.PlainString()
	if err != nil {
		t.Fatalf("PlainString: %v", err)
	}
	if want := "abX"; strings.TrimSpace(string(got)) != want {
		t.Errorf("PlainString() = %q, want %q", got, want)
	}
}
