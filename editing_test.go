package gostty

import (
	"strings"
	"testing"
)

func screen(t *testing.T, term *Terminal) string {
	t.Helper()
	got, err := term.PlainString()
	if err != nil {
		t.Fatalf("PlainString: %v", err)
	}
	return strings.TrimRight(string(got), "\n")
}

func TestCursorMovement(t *testing.T) {
	term := newTerm(t, 20, 6)
	if err := term.SetCursorPos(4, 10); err != nil {
		t.Fatalf("SetCursorPos: %v", err)
	}
	if err := term.CursorUp(2); err != nil {
		t.Fatalf("CursorUp: %v", err)
	}
	if err := term.CursorLeft(3); err != nil {
		t.Fatalf("CursorLeft: %v", err)
	}
	x, _ := term.CursorX()
	y, _ := term.CursorY()
	if x != 6 || y != 1 {
		t.Errorf("cursor = (%d,%d), want (6,1)", x, y)
	}

	if err := term.SaveCursor(); err != nil {
		t.Fatalf("SaveCursor: %v", err)
	}
	if err := term.SetCursorPos(1, 1); err != nil {
		t.Fatalf("SetCursorPos: %v", err)
	}
	if err := term.RestoreCursor(); err != nil {
		t.Fatalf("RestoreCursor: %v", err)
	}
	x, _ = term.CursorX()
	y, _ = term.CursorY()
	if x != 6 || y != 1 {
		t.Errorf("cursor after restore = (%d,%d), want (6,1)", x, y)
	}
}

func TestEraseLine(t *testing.T) {
	term := newTerm(t, 10, 2)
	if err := term.PrintString([]byte("abcdef")); err != nil {
		t.Fatalf("PrintString: %v", err)
	}
	if err := term.SetCursorPos(1, 3); err != nil {
		t.Fatalf("SetCursorPos: %v", err)
	}
	if err := term.EraseLine(EraseLineRight, false); err != nil {
		t.Fatalf("EraseLine: %v", err)
	}
	if got, want := screen(t, term), "ab"; got != want {
		t.Errorf("screen = %q, want %q", got, want)
	}
}

func TestEraseDisplay(t *testing.T) {
	term := newTerm(t, 10, 3)
	if err := term.PrintString([]byte("keep")); err != nil {
		t.Fatalf("PrintString: %v", err)
	}
	if err := term.Index(); err != nil {
		t.Fatalf("Index: %v", err)
	}
	if err := term.CarriageReturn(); err != nil {
		t.Fatalf("CarriageReturn: %v", err)
	}
	if err := term.PrintString([]byte("drop")); err != nil {
		t.Fatalf("PrintString: %v", err)
	}
	if err := term.SetCursorPos(2, 1); err != nil {
		t.Fatalf("SetCursorPos: %v", err)
	}
	if err := term.EraseDisplay(EraseDisplayBelow, false); err != nil {
		t.Fatalf("EraseDisplay: %v", err)
	}
	if got, want := screen(t, term), "keep"; got != want {
		t.Errorf("screen = %q, want %q", got, want)
	}
}

func TestInsertAndDeleteChars(t *testing.T) {
	term := newTerm(t, 12, 2)
	if err := term.PrintString([]byte("abcd")); err != nil {
		t.Fatalf("PrintString: %v", err)
	}
	if err := term.SetCursorPos(1, 2); err != nil {
		t.Fatalf("SetCursorPos: %v", err)
	}
	if err := term.InsertBlanks(2); err != nil {
		t.Fatalf("InsertBlanks: %v", err)
	}
	if got, want := screen(t, term), "a  bcd"; got != want {
		t.Errorf("after InsertBlanks: screen = %q, want %q", got, want)
	}
	if err := term.DeleteChars(2); err != nil {
		t.Fatalf("DeleteChars: %v", err)
	}
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
	if err := term.HorizontalTab(); err != nil {
		t.Fatalf("HorizontalTab: %v", err)
	}
	x, _ := term.CursorX()
	if x != 8 {
		t.Errorf("CursorX after tab = %d, want 8", x)
	}
	if err := term.TabClear(TabClearAll); err != nil {
		t.Fatalf("TabClear: %v", err)
	}
	if err := term.SetCursorPos(1, 1); err != nil {
		t.Fatalf("SetCursorPos: %v", err)
	}
	if err := term.HorizontalTab(); err != nil {
		t.Fatalf("HorizontalTab: %v", err)
	}
	x, _ = term.CursorX()
	if x != 39 {
		t.Errorf("CursorX after tab with no stops = %d, want 39", x)
	}
}

// SetScrollbackMaxBytes takes ?usize; nil means unlimited.
func TestOptionalScalarParam(t *testing.T) {
	term := newTerm(t, 10, 2)
	if err := term.SetScrollbackMaxBytes(nil); err != nil {
		t.Fatalf("SetScrollbackMaxBytes(nil): %v", err)
	}
	limit := uint(4096)
	if err := term.SetScrollbackMaxBytes(&limit); err != nil {
		t.Fatalf("SetScrollbackMaxBytes(&4096): %v", err)
	}
}

func TestPlainStringUnwrapped(t *testing.T) {
	term := newTerm(t, 4, 3)
	// Longer than a row, so the text wraps.
	if err := term.PrintString([]byte("abcdef")); err != nil {
		t.Fatalf("PrintString: %v", err)
	}
	wrapped, err := term.PlainString()
	if err != nil {
		t.Fatalf("PlainString: %v", err)
	}
	unwrapped, err := term.PlainStringUnwrapped()
	if err != nil {
		t.Fatalf("PlainStringUnwrapped: %v", err)
	}
	if !strings.Contains(string(wrapped), "\n") {
		t.Errorf("PlainString() = %q, expected a wrap newline", wrapped)
	}
	if got, want := strings.TrimRight(string(unwrapped), "\n"), "abcdef"; got != want {
		t.Errorf("PlainStringUnwrapped() = %q, want %q", got, want)
	}
}

// GetPwd and GetTitle return `?[:0]const u8`, so Go gets a presence flag
// alongside the value.
func TestPwdAndTitleRoundTrip(t *testing.T) {
	term := newTerm(t, 10, 2)

	if _, ok, err := term.GetPwd(); err != nil || ok {
		t.Errorf("GetPwd() on a fresh terminal = ok %v, err %v; want false, nil", ok, err)
	}

	if err := term.SetPwd([]byte("/tmp")); err != nil {
		t.Fatalf("SetPwd: %v", err)
	}
	if err := term.SetTitle([]byte("shell")); err != nil {
		t.Fatalf("SetTitle: %v", err)
	}

	pwd, ok, err := term.GetPwd()
	if err != nil || !ok || string(pwd) != "/tmp" {
		t.Errorf("GetPwd() = %q, %v, %v; want \"/tmp\", true, nil", pwd, ok, err)
	}
	title, ok, err := term.GetTitle()
	if err != nil || !ok || string(title) != "shell" {
		t.Errorf("GetTitle() = %q, %v, %v; want \"shell\", true, nil", title, ok, err)
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
	if err := term.PrintString([]byte("abc")); err != nil {
		t.Fatalf("PrintString: %v", err)
	}
	// An unnamed mode reaches the terminal, which ignores it rather than
	// crossing an invalid enum value.
	if err := term.EraseLine(EraseLine(7), false); err != nil {
		t.Fatalf("EraseLine(7): %v", err)
	}
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
	cols, _ := term.Cols()
	rows, _ := term.Rows()
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
		cps  []uint32
		want uint8
	}{
		{"ascii", []uint32{'a'}, 1},
		{"hangul", []uint32{0xAC00}, 2},
		{"emoji zwj", []uint32{0x1F468, 0x200D, 0x1F4BB}, 2},
	} {
		if got := GraphemeWidth(tc.cps); got != tc.want {
			t.Errorf("GraphemeWidth(%s) = %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestPrintSlice(t *testing.T) {
	term := newTerm(t, 10, 2)
	if err := term.PrintSlice([]uint32{'h', 'i', 0xAC00}); err != nil {
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
	if cols, err := term.Cols(); err != nil || cols != 80 {
		t.Errorf("Cols() = %d, %v; want 80, nil", cols, err)
	}
}

func TestScrollViewport(t *testing.T) {
	term := newTerm(t, 10, 2)
	for i := range 6 {
		if err := term.PrintString([]byte{byte('a' + i)}); err != nil {
			t.Fatalf("PrintString: %v", err)
		}
		if err := term.Index(); err != nil {
			t.Fatalf("Index: %v", err)
		}
		if err := term.CarriageReturn(); err != nil {
			t.Fatalf("CarriageReturn: %v", err)
		}
	}

	if err := term.ScrollViewport(ScrollViewportTop()); err != nil {
		t.Fatalf("ScrollViewport(top): %v", err)
	}
	top := screen(t, term)
	if !strings.Contains(top, "a") {
		t.Errorf("viewport at top = %q, want it to show the oldest row", top)
	}

	if err := term.ScrollViewport(ScrollViewportBottom()); err != nil {
		t.Fatalf("ScrollViewport(bottom): %v", err)
	}
	bottom := screen(t, term)
	if strings.Contains(bottom, "a") {
		t.Errorf("viewport at bottom = %q, want the oldest row scrolled off", bottom)
	}

	if err := term.ScrollViewport(ScrollViewportDelta(-2)); err != nil {
		t.Fatalf("ScrollViewport(delta): %v", err)
	}
	if screen(t, term) == bottom {
		t.Error("ScrollViewport(delta -2) did not move the viewport")
	}
	if err := term.ScrollViewport(ScrollViewportRow(0)); err != nil {
		t.Fatalf("ScrollViewport(row): %v", err)
	}
	if got := screen(t, term); got != top {
		t.Errorf("ScrollViewport(row 0) = %q, want the same as top %q", got, top)
	}
}

func TestSetDefaultCursor(t *testing.T) {
	term := newTerm(t, 10, 2)
	if err := term.SetDefaultCursorStyle(CursorStyleUnderline); err != nil {
		t.Fatalf("SetDefaultCursorStyle: %v", err)
	}
	got, err := term.CursorStyle()
	if err != nil {
		t.Fatalf("CursorStyle: %v", err)
	}
	if got != CursorStyleUnderline {
		t.Errorf("CursorStyle() = %v, want %v", got, CursorStyleUnderline)
	}

	// ?bool: nil selects the emulator default.
	if err := term.SetDefaultCursorBlink(nil); err != nil {
		t.Fatalf("SetDefaultCursorBlink(nil): %v", err)
	}
	blink := true
	if err := term.SetDefaultCursorBlink(&blink); err != nil {
		t.Fatalf("SetDefaultCursorBlink(&true): %v", err)
	}
}

// Charset, CharsetSlot and CursorStyle are all four-tag comptime enums whose
// truncated @typeName is one string; each parameter must still get its own type.
func TestCharsets(t *testing.T) {
	term := newTerm(t, 10, 2)
	if err := term.ConfigureCharset(CharsetSlotG1, CharsetDecSpecial); err != nil {
		t.Fatalf("ConfigureCharset: %v", err)
	}
	if err := term.InvokeCharset(CharsetActiveSlotGl, CharsetSlotG1, false); err != nil {
		t.Fatalf("InvokeCharset: %v", err)
	}
	// DEC special graphics maps `q` to a horizontal line.
	if err := term.PrintString([]byte("q")); err != nil {
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
}
