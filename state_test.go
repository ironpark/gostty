package gostty

import "testing"

func TestScrollRegionReadsBack(t *testing.T) {
	term, stream := newStreamPair(t, 20, 10)

	got := term.ScrollRegion()
	want := ScrollRegion{Top: 0, Bottom: 9, Left: 0, Right: 19}
	if got != want {
		t.Errorf("ScrollRegion() = %+v; want %+v", got, want)
	}

	// DECSTBM, then DECSLRM -- which only applies once left/right margin mode
	// (DEC 69) is enabled.
	feed(t, stream, "\x1b[3;8r\x1b[?69h\x1b[2;10s")
	got = term.ScrollRegion()
	want = ScrollRegion{Top: 2, Bottom: 7, Left: 1, Right: 9}
	if got != want {
		t.Errorf("ScrollRegion() after margins = %+v; want %+v", got, want)
	}
}

func TestCharsetState(t *testing.T) {
	term, stream := newStreamPair(t, 20, 5)

	if got := term.Charset(CharsetSlotG0); got != CharsetUTF8 {
		t.Errorf("Charset(G0) = %v; want utf8", got)
	}
	if got := term.CharsetGL(); got != CharsetSlotG0 {
		t.Errorf("CharsetGL() = %v; want G0", got)
	}
	if got := term.CharsetGR(); got != CharsetSlotG2 {
		t.Errorf("CharsetGR() = %v; want G2", got)
	}

	// SCS: G0 to DEC special graphics, G1 to British.
	feed(t, stream, "\x1b(0\x1b)A")
	if got := term.Charset(CharsetSlotG0); got != CharsetDecSpecial {
		t.Errorf("Charset(G0) = %v; want dec_special", got)
	}
	if got := term.Charset(CharsetSlotG1); got != CharsetBritish {
		t.Errorf("Charset(G1) = %v; want british", got)
	}

	// LS2 moves GL to G2; LS1R moves GR to G1.
	feed(t, stream, "\x1bn\x1b~")
	if got := term.CharsetGL(); got != CharsetSlotG2 {
		t.Errorf("CharsetGL() after LS2 = %v; want G2", got)
	}
	if got := term.CharsetGR(); got != CharsetSlotG1 {
		t.Errorf("CharsetGR() after LS1R = %v; want G1", got)
	}
}

func TestCharsetSingleShift(t *testing.T) {
	term, stream := newStreamPair(t, 20, 5)

	if _, ok := term.CharsetSingleShift(); ok {
		t.Errorf("CharsetSingleShift() ok = %v; want false", ok)
	}

	// SS2 arms G2 for exactly one character.
	feed(t, stream, "\x1bN")
	slot, ok := term.CharsetSingleShift()
	if !ok || slot != CharsetSlotG2 {
		t.Errorf("CharsetSingleShift() = %v, %v; want G2, true", slot, ok)
	}

	// Printing consumes it.
	feed(t, stream, "x")
	if _, ok := term.CharsetSingleShift(); ok {
		t.Errorf("CharsetSingleShift() after print ok = %v; want false", ok)
	}
}

func TestProtectedModeAndCursorProtected(t *testing.T) {
	term, stream := newStreamPair(t, 20, 5)

	if got := term.ProtectedMode(); got != ProtectedModeOff {
		t.Errorf("ProtectedMode() = %v; want off", got)
	}
	if got := term.CursorProtected(); got {
		t.Errorf("CursorProtected() = %v; want false", got)
	}

	// DECSCA 1: protect what is printed from now on.
	feed(t, stream, "\x1b[1\"q")
	if got := term.ProtectedMode(); got != ProtectedModeDec {
		t.Errorf("ProtectedMode() = %v; want dec", got)
	}
	if got := term.CursorProtected(); !got {
		t.Errorf("CursorProtected() = %v; want true", got)
	}

	// DECSCA 0 clears the pen but leaves the most recent mode, which is what
	// ECH keys off.
	feed(t, stream, "\x1b[0\"q")
	if got := term.CursorProtected(); got {
		t.Errorf("CursorProtected() after DECSCA 0 = %v; want false", got)
	}
	if got := term.ProtectedMode(); got != ProtectedModeDec {
		t.Errorf("ProtectedMode() after DECSCA 0 = %v; want dec", got)
	}
}

func TestCursorPendingWrap(t *testing.T) {
	term, stream := newStreamPair(t, 5, 3)

	feed(t, stream, "abcd")
	if got := term.CursorPendingWrap(); got {
		t.Errorf("CursorPendingWrap() = %v; want false", got)
	}

	// The fifth character fills the last column: the cursor stays there with
	// the LCF set until the next print soft-wraps.
	feed(t, stream, "e")
	if got := term.CursorPendingWrap(); !got {
		t.Errorf("CursorPendingWrap() at last column = %v; want true", got)
	}
	if got := term.CursorX(); got != 4 {
		t.Errorf("CursorX() = %v; want 4", got)
	}

	feed(t, stream, "f")
	if got := term.CursorPendingWrap(); got {
		t.Errorf("CursorPendingWrap() after wrap = %v; want false", got)
	}
}

func TestMouseTrackingAndFormat(t *testing.T) {
	term, stream := newStreamPair(t, 20, 5)

	if got := term.MouseTracking(); got != MouseTrackingNone {
		t.Errorf("MouseTracking() = %v; want none", got)
	}
	if got := term.MouseReportFormat(); got != MouseReportFormatX10 {
		t.Errorf("MouseReportFormat() = %v; want x10", got)
	}

	// Mode 1000 tracks buttons only; 1002 adds motion while a button is held.
	feed(t, stream, "\x1b[?1000h")
	if got := term.MouseTracking(); got != MouseTrackingNormal {
		t.Errorf("MouseTracking() = %v; want normal", got)
	}
	if got := term.MouseTrackingSendsMotion(); got {
		t.Errorf("MouseTrackingSendsMotion() = %v; want false", got)
	}

	feed(t, stream, "\x1b[?1002h\x1b[?1006h")
	if got := term.MouseTracking(); got != MouseTrackingButton {
		t.Errorf("MouseTracking() = %v; want button", got)
	}
	if got := term.MouseTrackingSendsMotion(); !got {
		t.Errorf("MouseTrackingSendsMotion() = %v; want true", got)
	}
	if got := term.MouseReportFormat(); got != MouseReportFormatSgr {
		t.Errorf("MouseReportFormat() = %v; want sgr", got)
	}

	feed(t, stream, "\x1b[?1002l\x1b[?1000l")
	if got := term.MouseTracking(); got != MouseTrackingNone {
		t.Errorf("MouseTracking() after reset = %v; want none", got)
	}
}

func TestPixelSize(t *testing.T) {
	term := newTerm(t, 20, 5)

	if got := term.WidthPx(); got != 0 {
		t.Errorf("WidthPx() = %v; want 0", got)
	}
	if err := term.ResizeCells(20, 5, 7, 15); err != nil {
		t.Fatalf("ResizeCells: %v", err)
	}
	if got := term.WidthPx(); got != 20*7 {
		t.Errorf("WidthPx() = %v; want 140", got)
	}
	if got := term.HeightPx(); got != 5*15 {
		t.Errorf("HeightPx() = %v; want 75", got)
	}
}

func TestTerminalFlags(t *testing.T) {
	term := newTerm(t, 20, 5)

	// Unknown focus and visibility are reported optimistically, so an
	// embedder that never tells the terminal behaves as if it were on screen.
	if got := term.Focused(); !got {
		t.Errorf("Focused() = %v; want true", got)
	}
	if got := term.Visible(); !got {
		t.Errorf("Visible() = %v; want true", got)
	}
	if got := term.PasswordInput(); got {
		t.Errorf("PasswordInput() = %v; want false", got)
	}
}

func TestModeReport(t *testing.T) {
	term, stream := newStreamPair(t, 20, 5)

	// DEC 25 (cursor visible) defaults to set.
	if got := term.ModeReport(25, false); got != ModeReportSet {
		t.Errorf("ModeReport(25) = %v; want set", got)
	}
	feed(t, stream, "\x1b[?25l")
	if got := term.ModeReport(25, false); got != ModeReportReset {
		t.Errorf("ModeReport(25) after reset = %v; want reset", got)
	}

	// ANSI 4 (IRM) lives in the other namespace: the same number as DEC 4.
	if got := term.ModeReport(4, true); got != ModeReportReset {
		t.Errorf("ModeReport(4, ansi) = %v; want reset", got)
	}
	feed(t, stream, "\x1b[4h")
	if got := term.ModeReport(4, true); got != ModeReportSet {
		t.Errorf("ModeReport(4, ansi) after set = %v; want set", got)
	}

	// DECECM is fixed off in ghostty rather than unimplemented.
	if got := term.ModeReport(117, false); got != ModeReportPermanentlyReset {
		t.Errorf("ModeReport(117) = %v; want permanently_reset", got)
	}
	if got := term.ModeReport(9999, false); got != ModeReportNotRecognized {
		t.Errorf("ModeReport(9999) = %v; want not_recognized", got)
	}
	// Full-width queries must not alias insert, wraparound, or erase-color mode.
	for _, query := range []struct {
		mode uint16
		ansi bool
	}{{32772, true}, {32775, false}, {32885, false}, {65535, false}} {
		if got := term.ModeReport(query.mode, query.ansi); got != ModeReportNotRecognized {
			t.Errorf("ModeReport(%d, %v) = %v; want not_recognized", query.mode, query.ansi, got)
		}
	}
}
