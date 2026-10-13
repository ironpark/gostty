package gostty

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// The alternate screen is what full-screen programs switch into; the primary
// screen's contents and scrollback survive underneath.
func TestSwitchScreen(t *testing.T) {
	term := newTerm(t, 20, 3)
	if err := term.PrintString("primary"); err != nil {
		t.Fatalf("PrintString: %v", err)
	}

	key := term.ActiveScreenKey()
	if key != ScreenKeyPrimary {
		t.Fatalf("ActiveScreenKey() = %v; want primary", key)
	}

	// The screen being left comes back, so a caller can keep a handle to the
	// primary screen without asking for it again.
	primary, ok, err := term.SwitchScreen(ScreenKeyAlternate)
	if err != nil || !ok || primary == nil {
		t.Fatalf("SwitchScreen(alternate) = %v, %v, %v; want screen, true, nil", primary, ok, err)
	}
	if key := term.ActiveScreenKey(); key != ScreenKeyAlternate {
		t.Fatalf("ActiveScreenKey() = %v; want alternate", key)
	}
	if got := screen(t, term); strings.TrimSpace(got) != "" {
		t.Errorf("alternate screen = %q, want empty", got)
	}

	if err := term.PrintString("alternate"); err != nil {
		t.Fatalf("PrintString: %v", err)
	}
	if got, want := screen(t, term), "alternate"; got != want {
		t.Errorf("alternate screen = %q, want %q", got, want)
	}

	if _, ok, err := term.SwitchScreen(ScreenKeyPrimary); err != nil || !ok {
		t.Fatalf("SwitchScreen(primary) = ok %v, %v; want true, nil", ok, err)
	}
	if got, want := screen(t, term), "primary"; got != want {
		t.Errorf("primary screen after switching back = %q, want %q", got, want)
	}

	// Switching to the screen already active is a no-op: no screen is left,
	// and it is not an error.
	if left, ok, err := term.SwitchScreen(ScreenKeyPrimary); err != nil || ok || left != nil {
		t.Errorf("SwitchScreen to the active screen = %v, %v, %v; want nil, false, nil", left, ok, err)
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

func TestHistoryStringAndFormat(t *testing.T) {
	term := newTerm(t, 10, 2)
	// Two rows fit on screen, so the first rows scroll into history.
	for _, line := range []string{"one", "two", "three", "four"} {
		if err := term.PrintString(line); err != nil {
			t.Fatalf("PrintString: %v", err)
		}
		if err := term.Index(); err != nil {
			t.Fatalf("Index: %v", err)
		}
		term.CarriageReturn()
	}

	history, err := term.HistoryString()
	if err != nil {
		t.Fatalf("HistoryString: %v", err)
	}
	if !strings.Contains(history, "one") {
		t.Errorf("HistoryString() = %q, want it to contain the scrolled-off rows", history)
	}
	if strings.Contains(history, "four") {
		t.Errorf("HistoryString() = %q, should not contain the active area", history)
	}

	// The screen formatter covers scrollback and the active area together.
	screen := term.ActiveScreen()
	var buf bytes.Buffer
	if err := screen.Format(FormatOptions{}, &buf); err != nil {
		t.Fatalf("Screen.Format: %v", err)
	}
	if got, want := buf.String(), "one\ntwo\nthree\nfour"; got != want {
		t.Errorf("Screen.Format() = %q, want %q", got, want)
	}
}

// A Screen handle is borrowed from its terminal: it has no Close, stays valid
// while the terminal is open, and stops working once the terminal is closed.
func TestBorrowedScreen(t *testing.T) {
	term, err := NewTerminal(20, 3)
	if err != nil {
		t.Fatalf("NewTerminal: %v", err)
	}
	if err := term.PrintString("hello"); err != nil {
		t.Fatalf("PrintString: %v", err)
	}

	sc := term.ActiveScreen()

	ok, err := sc.SelectAll()
	if err != nil {
		t.Fatalf("SelectAll: %v", err)
	}
	if !ok {
		t.Fatal("SelectAll() = false on a screen with text")
	}
	if has := sc.HasSelection(); !has {
		t.Fatalf("HasSelection() = %v; want true", has)
	}
	text, ok, err := sc.SelectionString()
	if err != nil || !ok {
		t.Fatalf("SelectionString() = ok %v, %v; want true, nil", ok, err)
	}
	if got, want := strings.TrimSpace(text), "hello"; got != want {
		t.Errorf("SelectionString() = %q, want %q", got, want)
	}

	sc.ClearSelection()
	if has := sc.HasSelection(); has {
		t.Fatalf("HasSelection() after clear = %v; want false", has)
	}
	if text, ok, err := sc.SelectionString(); err != nil || ok {
		t.Errorf("SelectionString() with no selection = %q, ok %v, %v; want absent, nil", text, ok, err)
	}

	// A borrowed handle does not keep the terminal open.
	if err := term.Close(); err != nil {
		t.Fatalf("Close with a screen handle out: %v", err)
	}
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on closed Screen")
		}
		var handleErr *HandleError
		if err, ok := r.(error); !ok || !errors.As(err, &handleErr) {
			t.Errorf("panic = %v, want *HandleError", r)
		}
	}()
	sc.HasSelection()
}

// The alternate screen does not exist until something switches to it.
func TestOptionalScreen(t *testing.T) {
	term := newTerm(t, 20, 3)

	if _, ok := term.Screen(ScreenKeyAlternate); ok {
		t.Errorf("Screen(alternate) before use = ok %v; want false", ok)
	}
	if _, ok := term.Screen(ScreenKeyPrimary); !ok {
		t.Errorf("Screen(primary) = ok %v; want true", ok)
	}

	if _, _, err := term.SwitchScreen(ScreenKeyAlternate); err != nil {
		t.Fatalf("SwitchScreen: %v", err)
	}
	alt, ok := term.Screen(ScreenKeyAlternate)
	if !ok {
		t.Fatalf("Screen(alternate) after switching = ok %v; want true", ok)
	}

	// The primary screen is still reachable while the alternate is active.
	if err := term.PrintString("on alternate"); err != nil {
		t.Fatalf("PrintString: %v", err)
	}
	if _, err := alt.SelectAll(); err != nil {
		t.Fatalf("SelectAll on the alternate screen: %v", err)
	}
	text, ok, err := alt.SelectionString()
	if err != nil || !ok {
		t.Fatalf("SelectionString() = ok %v, %v; want true, nil", ok, err)
	}
	if got, want := strings.TrimSpace(text), "on alternate"; got != want {
		t.Errorf("alternate selection = %q, want %q", got, want)
	}
}

// SelectAll covers the whole screen history, not only the rows currently in
// the viewport.
func TestSelectAllIncludesScrollback(t *testing.T) {
	term, stream := newStreamPair(t, 20, 3)
	term.SetScrollbackMaxBytes(1 << 20)
	for range 10 {
		feed(t, stream, "line\r\n")
	}
	feed(t, stream, "last")

	screen := term.ActiveScreen()
	if _, err := screen.SelectAll(); err != nil {
		t.Fatalf("SelectAll: %v", err)
	}
	text, ok, err := screen.SelectionString()
	if err != nil || !ok {
		t.Fatalf("SelectionString() = ok %v, %v; want true, nil", ok, err)
	}
	if !strings.Contains(text, "last") || strings.Count(text, "line") <= int(term.Rows()) {
		t.Errorf("selection = %q, want the viewport and its scrollback", text)
	}
}

// What a GUI needs to draw a scrollbar, in rows. ghostty keeps the total
// incrementally and caches the offset, so this is cheap enough per frame.
func TestScreenScrollbar(t *testing.T) {
	term, stream := newStreamPair(t, 20, 4)
	term.SetScrollbackMaxBytes(1 << 20)
	screen := term.ActiveScreen()

	// Nothing scrolled yet: the whole scrollable area is the viewport.
	bar := screen.Scrollbar()
	if bar.Len != 4 || bar.Offset != 0 {
		t.Errorf("Scrollbar() on a fresh screen = %+v; want Len 4, Offset 0", bar)
	}

	for range 20 {
		feed(t, stream, "line\r\n")
	}
	bar = screen.Scrollbar()
	if bar.Len != 4 {
		t.Errorf("Scrollbar().Len = %d, want the viewport height 4", bar.Len)
	}
	if bar.Total <= bar.Len {
		t.Errorf("Scrollbar() = %+v; Total should have grown past the viewport", bar)
	}
	// The viewport is at the bottom, so the thumb is at the end.
	if bar.Offset+bar.Len != bar.Total {
		t.Errorf("Scrollbar() = %+v; want Offset+Len == Total at the bottom", bar)
	}

	// Scrolling up moves the thumb and nothing else.
	term.ScrollViewport(ScrollViewportTop())
	top := screen.Scrollbar()
	if top.Offset != 0 || top.Total != bar.Total || top.Len != bar.Len {
		t.Errorf("Scrollbar() at the top = %+v, want Offset 0 and the same Total/Len as %+v", top, bar)
	}
}

func TestScreenScrollClear(t *testing.T) {
	term := newTerm(t, 20, 4)
	scr := term.ActiveScreen()
	if err := term.PrintString("line1\nline2\nline3\n"); err != nil {
		t.Fatalf("PrintString: %v", err)
	}

	if err := scr.ScrollClear(); err != nil {
		t.Fatalf("ScrollClear: %v", err)
	}

	// ScrollClear resets the cursor to top.
	if y := term.CursorY(); y != 0 {
		t.Errorf("CursorY() after ScrollClear = %d, want 0", y)
	}
	if x := term.CursorX(); x != 0 {
		t.Errorf("CursorX() after ScrollClear = %d, want 0", x)
	}
}
