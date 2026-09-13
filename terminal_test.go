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
	if got := term.Cols(); got != 80 {
		t.Errorf("Cols() = %d; want 80", got)
	}
	if got := term.Rows(); got != 24 {
		t.Errorf("Rows() = %d; want 24", got)
	}
}

func TestPrintStringAndDump(t *testing.T) {
	term := newTerm(t, 20, 3)
	if err := term.PrintString("hello"); err != nil {
		t.Fatalf("PrintString: %v", err)
	}
	if err := term.CarriageReturn(); err != nil {
		t.Fatalf("CarriageReturn: %v", err)
	}
	if err := term.Linefeed(); err != nil {
		t.Fatalf("Linefeed: %v", err)
	}
	if err := term.PrintString("world"); err != nil {
		t.Fatalf("PrintString: %v", err)
	}

	got, err := term.PlainString()
	if err != nil {
		t.Fatalf("PlainString: %v", err)
	}
	if want := "hello\nworld"; strings.TrimRight(got, "\n") != want {
		t.Errorf("PlainString() = %q, want %q", got, want)
	}

	if x := term.CursorX(); x != 5 {
		t.Errorf("CursorX() = %d; want 5", x)
	}
	if y := term.CursorY(); y != 1 {
		t.Errorf("CursorY() = %d; want 1", y)
	}
}

func TestPrintStringUTF8(t *testing.T) {
	term := newTerm(t, 20, 2)
	if err := term.PrintString("안녕😀"); err != nil {
		t.Fatalf("PrintString: %v", err)
	}
	got, err := term.PlainString()
	if err != nil {
		t.Fatalf("PlainString: %v", err)
	}
	if !strings.HasPrefix(got, "안녕😀") {
		t.Errorf("PlainString() = %q, want prefix %q", got, "안녕😀")
	}
	// Each of these is a wide cell, so the cursor advanced by 6 columns.
	if x := term.CursorX(); x != 6 {
		t.Errorf("CursorX() = %d; want 6", x)
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
			t.Fatalf("SetCursorStyle(%v):", tc.req)
		}
		got := term.CursorStyle()
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
	if err := term.PrintString("dirty"); err != nil {
		t.Fatalf("PrintString: %v", err)
	}
	if err := term.FullReset(); err != nil {
		t.Fatalf("FullReset: %v", err)
	}
	got, err := term.PlainString()
	if err != nil {
		t.Fatalf("PlainString: %v", err)
	}
	if strings.TrimSpace(got) != "" {
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
	// Cols has no error to return any more, so a read through a closed handle
	// panics rather than reporting. That is the trade the plain name buys: the
	// error was a defect, and a defect is not a value to branch on.
	func() {
		defer func() {
			switch r := recover().(type) {
			case nil:
				t.Error("Cols() after Close did not panic")
			case error:
				if !errors.Is(r, ErrInvalidHandle) {
					t.Errorf("Cols() after Close panicked with %v, want ErrInvalidHandle", r)
				}
			default:
				t.Errorf("Cols() after Close panicked with %T", r)
			}
		}()
		_ = term.Cols()
	}()
}

func TestBackspace(t *testing.T) {
	term := newTerm(t, 10, 2)
	if err := term.PrintString("abc"); err != nil {
		t.Fatalf("PrintString: %v", err)
	}
	if err := term.Backspace(); err != nil {
		t.Fatalf("Backspace: %v", err)
	}
	if err := term.PrintString("X"); err != nil {
		t.Fatalf("PrintString: %v", err)
	}
	got, err := term.PlainString()
	if err != nil {
		t.Fatalf("PlainString: %v", err)
	}
	if want := "abX"; strings.TrimSpace(got) != want {
		t.Errorf("PlainString() = %q, want %q", got, want)
	}
}
