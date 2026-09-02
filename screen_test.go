package gostty

import (
	"errors"
	"strings"
	"testing"
)

// The alternate screen is what full-screen programs switch into; the primary
// screen's contents and scrollback survive underneath.
func TestSwitchScreen(t *testing.T) {
	term := newTerm(t, 20, 3)
	if err := term.PrintString([]byte("primary")); err != nil {
		t.Fatalf("PrintString: %v", err)
	}

	key, err := term.ActiveScreenKey()
	if err != nil || key != ScreenKeyPrimary {
		t.Fatalf("ActiveScreenKey() = %v, %v; want primary, nil", key, err)
	}

	if err := term.SwitchScreen(ScreenKeyAlternate); err != nil {
		t.Fatalf("SwitchScreen(alternate): %v", err)
	}
	if key, err := term.ActiveScreenKey(); err != nil || key != ScreenKeyAlternate {
		t.Fatalf("ActiveScreenKey() = %v, %v; want alternate, nil", key, err)
	}
	if got := screen(t, term); strings.TrimSpace(got) != "" {
		t.Errorf("alternate screen = %q, want empty", got)
	}

	if err := term.PrintString([]byte("alternate")); err != nil {
		t.Fatalf("PrintString: %v", err)
	}
	if got, want := screen(t, term), "alternate"; got != want {
		t.Errorf("alternate screen = %q, want %q", got, want)
	}

	if err := term.SwitchScreen(ScreenKeyPrimary); err != nil {
		t.Fatalf("SwitchScreen(primary): %v", err)
	}
	if got, want := screen(t, term), "primary"; got != want {
		t.Errorf("primary screen after switching back = %q, want %q", got, want)
	}

	// Switching to the screen already active is a no-op, not an error.
	if err := term.SwitchScreen(ScreenKeyPrimary); err != nil {
		t.Errorf("SwitchScreen to the active screen: %v", err)
	}
}

// PrintAttributesInto writes into a buffer the caller owns and reports the
// count, so the buffer can be reused across calls.
func TestPrintAttributesInto(t *testing.T) {
	term, stream := newStreamPair(t, 20, 3)
	buf := make([]byte, 64)

	n, err := term.PrintAttributesInto(buf)
	if err != nil {
		t.Fatalf("PrintAttributesInto: %v", err)
	}
	if got, want := string(buf[:n]), "0"; got != want {
		t.Errorf("default attributes = %q, want %q", got, want)
	}

	// Bold plus red foreground.
	feed(t, stream, "\x1b[1;31m")
	n, err = term.PrintAttributesInto(buf)
	if err != nil {
		t.Fatalf("PrintAttributesInto: %v", err)
	}
	got := string(buf[:n])
	if !strings.HasPrefix(got, "0;") {
		t.Errorf("attributes = %q, want a DECRPSS body starting with %q", got, "0;")
	}
	if !strings.Contains(got, "1") {
		t.Errorf("attributes = %q, want it to report bold", got)
	}
}

func TestHistoryAndScreenString(t *testing.T) {
	term := newTerm(t, 10, 2)
	// Two rows fit on screen, so the first rows scroll into history.
	for _, line := range []string{"one", "two", "three", "four"} {
		if err := term.PrintString([]byte(line)); err != nil {
			t.Fatalf("PrintString: %v", err)
		}
		if err := term.Index(); err != nil {
			t.Fatalf("Index: %v", err)
		}
		if err := term.CarriageReturn(); err != nil {
			t.Fatalf("CarriageReturn: %v", err)
		}
	}

	history, err := term.HistoryString()
	if err != nil {
		t.Fatalf("HistoryString: %v", err)
	}
	if !strings.Contains(string(history), "one") {
		t.Errorf("HistoryString() = %q, want it to contain the scrolled-off rows", history)
	}
	if strings.Contains(string(history), "four") {
		t.Errorf("HistoryString() = %q, should not contain the active area", history)
	}

	full, err := term.ScreenString()
	if err != nil {
		t.Fatalf("ScreenString: %v", err)
	}
	for _, want := range []string{"one", "four"} {
		if !strings.Contains(string(full), want) {
			t.Errorf("ScreenString() = %q, want it to contain %q", full, want)
		}
	}
}

// A Screen handle is borrowed from its terminal: it has no Close, stays valid
// while the terminal is open, and stops working once the terminal is closed.
func TestBorrowedScreen(t *testing.T) {
	term, err := NewTerminal(20, 3)
	if err != nil {
		t.Fatalf("NewTerminal: %v", err)
	}
	if err := term.PrintString([]byte("hello")); err != nil {
		t.Fatalf("PrintString: %v", err)
	}

	sc, err := term.ActiveScreen()
	if err != nil {
		t.Fatalf("ActiveScreen: %v", err)
	}

	ok, err := sc.SelectAll()
	if err != nil {
		t.Fatalf("SelectAll: %v", err)
	}
	if !ok {
		t.Fatal("SelectAll() = false on a screen with text")
	}
	if has, err := sc.HasSelection(); err != nil || !has {
		t.Fatalf("HasSelection() = %v, %v; want true, nil", has, err)
	}
	text, err := sc.SelectionString()
	if err != nil {
		t.Fatalf("SelectionString: %v", err)
	}
	if got, want := strings.TrimSpace(string(text)), "hello"; got != want {
		t.Errorf("SelectionString() = %q, want %q", got, want)
	}

	if err := sc.ClearSelection(); err != nil {
		t.Fatalf("ClearSelection: %v", err)
	}
	if has, err := sc.HasSelection(); err != nil || has {
		t.Fatalf("HasSelection() after clear = %v, %v; want false, nil", has, err)
	}
	if text, err := sc.SelectionString(); err != nil || len(text) != 0 {
		t.Errorf("SelectionString() with no selection = %q, %v; want empty, nil", text, err)
	}

	// A borrowed handle does not keep the terminal open.
	if err := term.Close(); err != nil {
		t.Fatalf("Close with a screen handle out: %v", err)
	}
	if _, err := sc.HasSelection(); !errors.Is(err, ErrInvalidHandle) {
		t.Errorf("screen call after the terminal closed = %v, want ErrInvalidHandle", err)
	}
}

// The alternate screen does not exist until something switches to it.
func TestOptionalScreen(t *testing.T) {
	term := newTerm(t, 20, 3)

	if _, ok, err := term.Screen(ScreenKeyAlternate); err != nil || ok {
		t.Errorf("Screen(alternate) before use = ok %v, err %v; want false, nil", ok, err)
	}
	if _, ok, err := term.Screen(ScreenKeyPrimary); err != nil || !ok {
		t.Errorf("Screen(primary) = ok %v, err %v; want true, nil", ok, err)
	}

	if err := term.SwitchScreen(ScreenKeyAlternate); err != nil {
		t.Fatalf("SwitchScreen: %v", err)
	}
	alt, ok, err := term.Screen(ScreenKeyAlternate)
	if err != nil || !ok {
		t.Fatalf("Screen(alternate) after switching = ok %v, err %v; want true, nil", ok, err)
	}

	// The primary screen is still reachable while the alternate is active.
	if err := term.PrintString([]byte("on alternate")); err != nil {
		t.Fatalf("PrintString: %v", err)
	}
	if _, err := alt.SelectAll(); err != nil {
		t.Fatalf("SelectAll on the alternate screen: %v", err)
	}
	text, err := alt.SelectionString()
	if err != nil {
		t.Fatalf("SelectionString: %v", err)
	}
	if got, want := strings.TrimSpace(string(text)), "on alternate"; got != want {
		t.Errorf("alternate selection = %q, want %q", got, want)
	}
}
