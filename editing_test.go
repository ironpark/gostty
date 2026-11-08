package gostty

import (
	"errors"
	"strings"
	"testing"
)

func screen(t *testing.T, term *Terminal) string {
	t.Helper()
	got, err := term.PlainString()
	if err != nil {
		t.Fatalf("PlainString: %v", err)
	}
	return strings.TrimRight(got, "\n")
}

// The single-axis moves keep the other coordinate, and the relative ones stop
// only at the edge of the screen.
func TestCursorSingleAxis(t *testing.T) {
	term := newTerm(t, 20, 6)
	term.SetCursorPos(3, 4)
	term.SetCursorCol(10)
	if x, y := term.CursorX(), term.CursorY(); x != 9 || y != 2 {
		t.Errorf("after SetCursorCol = (%d,%d), want (9,2)", x, y)
	}
	term.SetCursorRow(1)
	if x, y := term.CursorX(), term.CursorY(); x != 9 || y != 0 {
		t.Errorf("after SetCursorRow = (%d,%d), want (9,0)", x, y)
	}
	term.CursorRowRelative(2)
	term.CursorColRelative(100)
	if x, y := term.CursorX(), term.CursorY(); x != 19 || y != 2 {
		t.Errorf("after relative moves = (%d,%d), want (19,2)", x, y)
	}
}

func TestCursorMovement(t *testing.T) {
	term := newTerm(t, 20, 6)
	term.SetCursorPos(4, 10)
	term.CursorUp(2)
	term.CursorLeft(3)
	x := term.CursorX()
	y := term.CursorY()
	if x != 6 || y != 1 {
		t.Errorf("cursor = (%d,%d), want (6,1)", x, y)
	}

	term.SaveCursor()
	term.SetCursorPos(1, 1)
	term.RestoreCursor()
	x = term.CursorX()
	y = term.CursorY()
	if x != 6 || y != 1 {
		t.Errorf("cursor after restore = (%d,%d), want (6,1)", x, y)
	}
}

func TestEraseLine(t *testing.T) {
	term := newTerm(t, 10, 2)
	if err := term.PrintString("abcdef"); err != nil {
		t.Fatalf("PrintString: %v", err)
	}
	term.SetCursorPos(1, 3)
	term.EraseLine(EraseLineRight, false)
	if got, want := screen(t, term), "ab"; got != want {
		t.Errorf("screen = %q, want %q", got, want)
	}
}

func TestEraseDisplay(t *testing.T) {
	term := newTerm(t, 10, 3)
	if err := term.PrintString("keep"); err != nil {
		t.Fatalf("PrintString: %v", err)
	}
	if err := term.Index(); err != nil {
		t.Fatalf("Index: %v", err)
	}
	term.CarriageReturn()
	if err := term.PrintString("drop"); err != nil {
		t.Fatalf("PrintString: %v", err)
	}
	term.SetCursorPos(2, 1)
	term.EraseDisplay(EraseDisplayBelow, false)
	if got, want := screen(t, term), "keep"; got != want {
		t.Errorf("screen = %q, want %q", got, want)
	}
}

func TestInsertAndDeleteChars(t *testing.T) {
	term := newTerm(t, 12, 2)
	if err := term.PrintString("abcd"); err != nil {
		t.Fatalf("PrintString: %v", err)
	}
	term.SetCursorPos(1, 2)
	term.InsertBlanks(2)
	if got, want := screen(t, term), "a  bcd"; got != want {
		t.Errorf("after InsertBlanks: screen = %q, want %q", got, want)
	}
	term.DeleteChars(2)
	if got, want := screen(t, term), "abcd"; got != want {
		t.Errorf("after DeleteChars: screen = %q, want %q", got, want)
	}
}

func TestPrintAndRepeat(t *testing.T) {
	term := newTerm(t, 10, 2)
	if err := term.Print('x'); err != nil {
		t.Fatalf("Print: %v", err)
	}
	if err := term.PrintRepeat(3); err != nil {
		t.Fatalf("PrintRepeat: %v", err)
	}
	if got, want := screen(t, term), "xxxx"; got != want {
		t.Errorf("screen = %q, want %q", got, want)
	}
}

func TestTabStops(t *testing.T) {
	term := newTerm(t, 40, 2)
	term.HorizontalTab()
	x := term.CursorX()
	if x != 8 {
		t.Errorf("CursorX after tab = %d, want 8", x)
	}
	term.TabClear(TabClearAll)
	term.SetCursorPos(1, 1)
	term.HorizontalTab()
	x = term.CursorX()
	if x != 39 {
		t.Errorf("CursorX after tab with no stops = %d, want 39", x)
	}
}

// ClearScrollbackMaxBytes removes the limit; SetScrollbackMaxBytes sets one.
func TestOptionalScalarParam(t *testing.T) {
	term := newTerm(t, 10, 2)
	term.ClearScrollbackMaxBytes()
	limit := uint(4096)
	term.SetScrollbackMaxBytes(limit)
}

func TestFormatUnwrap(t *testing.T) {
	term := newTerm(t, 4, 3)
	// Longer than a row, so the text wraps.
	if err := term.PrintString("abcdef"); err != nil {
		t.Fatalf("PrintString: %v", err)
	}
	wrapped, err := term.PlainString()
	if err != nil {
		t.Fatalf("PlainString: %v", err)
	}
	unwrapped, err := formatString(term, FormatOptions{Unwrap: true})
	if err != nil {
		t.Fatalf("Format(unwrap): %v", err)
	}
	if !strings.Contains(wrapped, "\n") {
		t.Errorf("PlainString() = %q, expected a wrap newline", wrapped)
	}
	if got, want := strings.TrimRight(unwrapped, "\n"), "abcdef"; got != want {
		t.Errorf("Format(unwrap) = %q, want %q", got, want)
	}
}

// GetPwd and GetTitle return `?[:0]const u8`, so Go gets a presence flag
// alongside the value.
func TestPwdAndTitleRoundTrip(t *testing.T) {
	term := newTerm(t, 10, 2)

	if _, ok := term.Pwd(); ok {
		t.Errorf("GetPwd() on a fresh terminal = ok %v; want false", ok)
	}

	if err := term.SetPwd("/tmp"); err != nil {
		t.Fatalf("SetPwd: %v", err)
	}
	if err := term.SetTitle("shell"); err != nil {
		t.Fatalf("SetTitle: %v", err)
	}

	pwd, ok := term.Pwd()
	if !ok || pwd != "/tmp" {
		t.Errorf("GetPwd() = %q, %v; want \"/tmp\", true", pwd, ok)
	}
	title, ok := term.Title()
	if !ok || title != "shell" {
		t.Errorf("GetTitle() = %q, %v; want \"shell\", true", title, ok)
	}
}

// EraseLine and TabClear are non-exhaustive in ghostty so a number that came
// off a pty never fails to convert. Values outside the named set survive the
// round trip and print as Type(N).
func TestOpenEnums(t *testing.T) {
	if got := EraseLine(7).String(); got != "EraseLine(7)" {
		t.Errorf("EraseLine(7).String() = %q, want %q", got, "EraseLine(7)")
	}
	if got := TabClear(9).String(); got != "TabClear(9)" {
		t.Errorf("TabClear(9).String() = %q, want %q", got, "TabClear(9)")
	}
	if got := EraseLineComplete.String(); got != "complete" {
		t.Errorf("EraseLineComplete.String() = %q, want %q", got, "complete")
	}

	term := newTerm(t, 10, 2)
	if err := term.PrintString("abc"); err != nil {
		t.Fatalf("PrintString: %v", err)
	}
	// An unnamed mode reaches the terminal, which ignores it rather than
	// crossing an invalid enum value.
	term.EraseLine(EraseLine(7), false)
	if got, want := screen(t, term), "abc"; got != want {
		t.Errorf("screen = %q, want %q", got, want)
	}
}

// EraseDisplay is exhaustive; scroll_complete is a named Kitty extension.
func TestEraseDisplayScrollComplete(t *testing.T) {
	if got := EraseDisplayScrollComplete.String(); got != "scroll_complete" {
		t.Errorf("String() = %q, want %q", got, "scroll_complete")
	}
}

func TestResize(t *testing.T) {
	term := newTerm(t, 20, 5)
	if err := term.Resize(40, 10); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	cols := term.Cols()
	rows := term.Rows()
	if cols != 40 || rows != 10 {
		t.Errorf("size after resize = %dx%d, want 40x10", cols, rows)
	}
	if err := term.Resize(0, 10); err == nil {
		t.Error("Resize(0, 10) succeeded, want an error")
	}
}

func TestGraphemeWidth(t *testing.T) {
	for _, tc := range []struct {
		name string
		cps  []rune
		want uint8
	}{
		{"ascii", []rune{'a'}, 1},
		{"hangul", []rune{0xAC00}, 2},
		{"emoji zwj", []rune{0x1F468, 0x200D, 0x1F4BB}, 2},
	} {
		got, err := GraphemeWidth(tc.cps)
		if err != nil {
			t.Fatalf("GraphemeWidth(%s): %v", tc.name, err)
		}
		if got != tc.want {
			t.Errorf("GraphemeWidth(%s) = %d, want %d", tc.name, got, tc.want)
		}
	}
	if _, err := GraphemeWidth([]rune{0x110000}); !errors.Is(err, ErrOutOfRange) {
		t.Errorf("GraphemeWidth(0x110000) err = %v, want ErrOutOfRange", err)
	}
}

func TestPrintSlice(t *testing.T) {
	term := newTerm(t, 10, 2)
	if err := term.PrintSlice([]rune{'h', 'i', 0xAC00}); err != nil {
		t.Fatalf("PrintSlice: %v", err)
	}
	if got, want := screen(t, term), "hi가"; got != want {
		t.Errorf("screen = %q, want %q", got, want)
	}
}

func TestDeccolm(t *testing.T) {
	term := newTerm(t, 80, 5)
	// DECCOLM is ignored unless DEC mode 40 is enabled, so this only checks
	// that the call crosses the boundary and leaves the terminal usable.
	if err := term.Deccolm(DeccolmMode132Cols); err != nil {
		t.Fatalf("Deccolm(132): %v", err)
	}
	if err := term.Deccolm(DeccolmMode80Cols); err != nil {
		t.Fatalf("Deccolm(80): %v", err)
	}
	if cols := term.Cols(); cols != 80 {
		t.Errorf("Cols() = %d; want 80", cols)
	}
}

func TestScrollViewport(t *testing.T) {
	term := newTerm(t, 10, 2)
	for i := range 6 {
		if err := term.PrintString(string(rune('a' + i))); err != nil {
			t.Fatalf("PrintString: %v", err)
		}
		if err := term.Index(); err != nil {
			t.Fatalf("Index: %v", err)
		}
		term.CarriageReturn()
	}

	term.ScrollViewport(ScrollViewportTop())
	top := screen(t, term)
	if !strings.Contains(top, "a") {
		t.Errorf("viewport at top = %q, want it to show the oldest row", top)
	}

	term.ScrollViewport(ScrollViewportBottom())
	bottom := screen(t, term)
	if strings.Contains(bottom, "a") {
		t.Errorf("viewport at bottom = %q, want the oldest row scrolled off", bottom)
	}

	term.ScrollViewport(ScrollViewportDelta(-2))
	if screen(t, term) == bottom {
		t.Error("ScrollViewport(delta -2) did not move the viewport")
	}
	term.ScrollViewport(ScrollViewportRow(0))
	if got := screen(t, term); got != top {
		t.Errorf("ScrollViewport(row 0) = %q, want the same as top %q", got, top)
	}
}

func TestSetDefaultCursor(t *testing.T) {
	term := newTerm(t, 10, 2)
	term.SetDefaultCursorStyle(CursorStyleUnderline)
	got := term.CursorStyle()
	if got != CursorStyleUnderline {
		t.Errorf("CursorStyle() = %v, want %v", got, CursorStyleUnderline)
	}

	// ?bool: nil selects the emulator default.
	term.ResetDefaultCursorBlink()
	blink := true
	term.SetDefaultCursorBlink(blink)
}

// Charset, CharsetSlot and CursorStyle are all four-tag comptime enums whose
// truncated @typeName is one string; each parameter must still get its own type.
func TestCharsets(t *testing.T) {
	term := newTerm(t, 10, 2)
	term.ConfigureCharset(CharsetSlotG1, CharsetDecSpecial)
	term.InvokeCharset(CharsetActiveSlotGl, CharsetSlotG1, false)
	// DEC special graphics maps `q` to a horizontal line.
	if err := term.PrintString("q"); err != nil {
		t.Fatalf("PrintString: %v", err)
	}
	if got, want := screen(t, term), "\u2500"; got != want {
		t.Errorf("screen = %q, want %q", got, want)
	}
}

func TestScrollViewportTag(t *testing.T) {
	if got := ScrollViewportDelta(-3).Tag(); got != ScrollViewportTagDelta {
		t.Errorf("Tag() = %v, want %v", got, ScrollViewportTagDelta)
	}
	if got := ScrollViewportTop().Tag().String(); got != "top" {
		t.Errorf("Tag().String() = %q, want %q", got, "top")
	}
	if got := ScrollViewportDeltaPrompt(1).Tag(); got != ScrollViewportTagDeltaPrompt {
		t.Errorf("Tag() = %v, want %v", got, ScrollViewportTagDeltaPrompt)
	}
	if val, ok := ScrollViewportDeltaPrompt(-2).AsDeltaPrompt(); !ok || val != -2 {
		t.Errorf("AsDeltaPrompt() = (%v, %v), want (-2, true)", val, ok)
	}
	if got := ScrollViewportDeltaPrompt(1).Tag().String(); got != "delta_prompt" {
		t.Errorf("Tag().String() = %q, want %q", got, "delta_prompt")
	}
}

// DECIC and DECDC shift every row of the scrolling region at the cursor
// column, and leave the cursor where it was.
func TestInsertDeleteColumns(t *testing.T) {
	term := newTerm(t, 6, 2)
	term.PrintString("abcdef")
	term.SetCursorPos(2, 1)
	term.PrintString("ghijkl")
	term.SetCursorPos(1, 3)

	term.InsertColumns(2)
	if got, want := screen(t, term), "ab  cd\ngh  ij"; got != want {
		t.Errorf("after InsertColumns(2) = %q, want %q", got, want)
	}
	if x, y := term.CursorX(), term.CursorY(); x != 2 || y != 0 {
		t.Errorf("cursor after InsertColumns = (%d,%d), want (2,0)", x, y)
	}

	term.DeleteColumns(3)
	if got, want := screen(t, term), "abd\nghj"; got != want {
		t.Errorf("after DeleteColumns(3) = %q, want %q", got, want)
	}
}

// DECBI and DECFI move the cursor inside the screen and shift the region only
// at its margins.
func TestBackForwardIndex(t *testing.T) {
	term := newTerm(t, 4, 1)
	term.PrintString("abcd")

	term.SetCursorPos(1, 2)
	term.BackIndex()
	if x := term.CursorX(); x != 0 {
		t.Errorf("cursor after BackIndex = %d, want 0", x)
	}
	term.BackIndex()
	if got, want := screen(t, term), " abc"; got != want {
		t.Errorf("BackIndex at the left margin = %q, want %q", got, want)
	}

	term.SetCursorPos(1, 3)
	term.ForwardIndex()
	if x := term.CursorX(); x != 3 {
		t.Errorf("cursor after ForwardIndex = %d, want 3", x)
	}
	term.ForwardIndex()
	if got, want := screen(t, term), "abc"; got != want {
		t.Errorf("ForwardIndex at the right margin = %q, want %q", got, want)
	}
}

// The relative moves are signed: a negative delta moves back, and both
// directions stop at the edge of the screen.
func TestCursorRelativeNegative(t *testing.T) {
	term := newTerm(t, 20, 6)
	term.SetCursorPos(4, 10)
	term.CursorRowRelative(-2)
	term.CursorColRelative(-3)
	if x, y := term.CursorX(), term.CursorY(); x != 6 || y != 1 {
		t.Errorf("after negative moves = (%d,%d), want (6,1)", x, y)
	}
	term.CursorRowRelative(-100)
	term.CursorColRelative(-100)
	if x, y := term.CursorX(), term.CursorY(); x != 0 || y != 0 {
		t.Errorf("after moving past the edge = (%d,%d), want (0,0)", x, y)
	}
}

// DECRQCRA: the DEC checksum is negated, so the Positive variant of the same
// rectangle is its two's complement, and text changes the sum.
func TestRectXtChecksum(t *testing.T) {
	term := newTerm(t, 10, 3)
	whole := XtChecksumRequest{}
	blank := term.RectXtChecksum(whole)

	term.PrintString("hello")
	negated := term.RectXtChecksum(whole)
	if negated == blank {
		t.Fatalf("checksum did not change after printing: %#04x", negated)
	}

	term.SetDefaultXtChecksum(XtChecksumFlags{Positive: true})
	positive := term.RectXtChecksum(whole)
	if negated+positive != 0 {
		t.Errorf("negated %#04x + positive %#04x != 0", negated, positive)
	}

	// Only the first row holds text, so a rectangle below it sums like blank
	// cells do.
	term.SetDefaultXtChecksum(XtChecksumFlags{})
	if got := term.RectXtChecksum(XtChecksumRequest{Top: 2, Left: 1, Bottom: 3, Right: 10}); got != blank {
		t.Errorf("rows 2-3 = %#04x, want the blank sum %#04x", got, blank)
	}
}
