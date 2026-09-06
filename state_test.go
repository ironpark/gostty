package gostty

import "testing"

func TestScrollRegionReadsBack(t *testing.T) {
	term, stream := newStreamPair(t, 20, 10)

	got, err := term.ScrollRegion()
	if err != nil {
		t.Fatalf("ScrollRegion: %v", err)
	}
	want := ScrollRegion{Top: 0, Bottom: 9, Left: 0, Right: 19}
	if got != want {
		t.Errorf("ScrollRegion() = %+v; want %+v", got, want)
	}

	// DECSTBM, then DECSLRM -- which only applies once left/right margin mode
	// (DEC 69) is enabled.
	feed(t, stream, "\x1b[3;8r\x1b[?69h\x1b[2;10s")
	got, err = term.ScrollRegion()
	if err != nil {
		t.Fatalf("ScrollRegion: %v", err)
	}
	want = ScrollRegion{Top: 2, Bottom: 7, Left: 1, Right: 9}
	if got != want {
		t.Errorf("ScrollRegion() after margins = %+v; want %+v", got, want)
	}
}

func TestCharsetState(t *testing.T) {
	term, stream := newStreamPair(t, 20, 5)

	if got, err := term.Charset(CharsetSlotG0); err != nil || got != CharsetUTF8 {
		t.Errorf("Charset(G0) = %v, %v; want utf8, nil", got, err)
	}
	if got, err := term.CharsetGl(); err != nil || got != CharsetSlotG0 {
		t.Errorf("CharsetGl() = %v, %v; want G0, nil", got, err)
	}
	if got, err := term.CharsetGr(); err != nil || got != CharsetSlotG2 {
		t.Errorf("CharsetGr() = %v, %v; want G2, nil", got, err)
	}

	// SCS: G0 to DEC special graphics, G1 to British.
	feed(t, stream, "\x1b(0\x1b)A")
	if got, err := term.Charset(CharsetSlotG0); err != nil || got != CharsetDecSpecial {
		t.Errorf("Charset(G0) = %v, %v; want dec_special, nil", got, err)
	}
	if got, err := term.Charset(CharsetSlotG1); err != nil || got != CharsetBritish {
		t.Errorf("Charset(G1) = %v, %v; want british, nil", got, err)
	}

	// LS2 moves GL to G2; LS1R moves GR to G1.
	feed(t, stream, "\x1bn\x1b~")
	if got, err := term.CharsetGl(); err != nil || got != CharsetSlotG2 {
		t.Errorf("CharsetGl() after LS2 = %v, %v; want G2, nil", got, err)
	}
	if got, err := term.CharsetGr(); err != nil || got != CharsetSlotG1 {
		t.Errorf("CharsetGr() after LS1R = %v, %v; want G1, nil", got, err)
	}
}

func TestCharsetSingleShift(t *testing.T) {
	term, stream := newStreamPair(t, 20, 5)

	if _, ok, err := term.CharsetSingleShift(); err != nil || ok {
		t.Errorf("CharsetSingleShift() ok = %v, %v; want false, nil", ok, err)
	}

	// SS2 arms G2 for exactly one character.
	feed(t, stream, "\x1bN")
	slot, ok, err := term.CharsetSingleShift()
	if err != nil {
		t.Fatalf("CharsetSingleShift: %v", err)
	}
	if !ok || slot != CharsetSlotG2 {
		t.Errorf("CharsetSingleShift() = %v, %v; want G2, true", slot, ok)
	}

	// Printing consumes it.
	feed(t, stream, "x")
	if _, ok, err := term.CharsetSingleShift(); err != nil || ok {
		t.Errorf("CharsetSingleShift() after print ok = %v, %v; want false, nil", ok, err)
	}
}

func TestProtectedModeAndCursorProtected(t *testing.T) {
	term, stream := newStreamPair(t, 20, 5)

	if got, err := term.ProtectedMode(); err != nil || got != ProtectedModeOff {
		t.Errorf("ProtectedMode() = %v, %v; want off, nil", got, err)
	}
	if got, err := term.CursorProtected(); err != nil || got {
		t.Errorf("CursorProtected() = %v, %v; want false, nil", got, err)
	}

	// DECSCA 1: protect what is printed from now on.
	feed(t, stream, "\x1b[1\"q")
	if got, err := term.ProtectedMode(); err != nil || got != ProtectedModeDec {
		t.Errorf("ProtectedMode() = %v, %v; want dec, nil", got, err)
	}
	if got, err := term.CursorProtected(); err != nil || !got {
		t.Errorf("CursorProtected() = %v, %v; want true, nil", got, err)
	}

	// DECSCA 0 clears the pen but leaves the most recent mode, which is what
	// ECH keys off.
	feed(t, stream, "\x1b[0\"q")
	if got, err := term.CursorProtected(); err != nil || got {
		t.Errorf("CursorProtected() after DECSCA 0 = %v, %v; want false, nil", got, err)
	}
	if got, err := term.ProtectedMode(); err != nil || got != ProtectedModeDec {
		t.Errorf("ProtectedMode() after DECSCA 0 = %v, %v; want dec, nil", got, err)
	}
}

func TestCursorPendingWrap(t *testing.T) {
	term, stream := newStreamPair(t, 5, 3)

	feed(t, stream, "abcd")
	if got, err := term.CursorPendingWrap(); err != nil || got {
		t.Errorf("CursorPendingWrap() = %v, %v; want false, nil", got, err)
	}

	// The fifth character fills the last column: the cursor stays there with
	// the LCF set until the next print soft-wraps.
	feed(t, stream, "e")
	if got, err := term.CursorPendingWrap(); err != nil || !got {
		t.Errorf("CursorPendingWrap() at last column = %v, %v; want true, nil", got, err)
	}
	if got, err := term.CursorX(); err != nil || got != 4 {
		t.Errorf("CursorX() = %v, %v; want 4, nil", got, err)
	}

	feed(t, stream, "f")
	if got, err := term.CursorPendingWrap(); err != nil || got {
		t.Errorf("CursorPendingWrap() after wrap = %v, %v; want false, nil", got, err)
	}
}

func TestMouseTrackingAndFormat(t *testing.T) {
	term, stream := newStreamPair(t, 20, 5)

	if got, err := term.MouseTracking(); err != nil || got != MouseTrackingNone {
		t.Errorf("MouseTracking() = %v, %v; want none, nil", got, err)
	}
	if got, err := term.MouseReportFormat(); err != nil || got != MouseReportFormatX10 {
		t.Errorf("MouseReportFormat() = %v, %v; want x10, nil", got, err)
	}

	// Mode 1000 tracks buttons only; 1002 adds motion while a button is held.
	feed(t, stream, "\x1b[?1000h")
	if got, err := term.MouseTracking(); err != nil || got != MouseTrackingNormal {
		t.Errorf("MouseTracking() = %v, %v; want normal, nil", got, err)
	}
	if got, err := term.MouseTrackingSendsMotion(); err != nil || got {
		t.Errorf("MouseTrackingSendsMotion() = %v, %v; want false, nil", got, err)
	}

	feed(t, stream, "\x1b[?1002h\x1b[?1006h")
	if got, err := term.MouseTracking(); err != nil || got != MouseTrackingButton {
		t.Errorf("MouseTracking() = %v, %v; want button, nil", got, err)
	}
	if got, err := term.MouseTrackingSendsMotion(); err != nil || !got {
		t.Errorf("MouseTrackingSendsMotion() = %v, %v; want true, nil", got, err)
	}
	if got, err := term.MouseReportFormat(); err != nil || got != MouseReportFormatSgr {
		t.Errorf("MouseReportFormat() = %v, %v; want sgr, nil", got, err)
	}

	feed(t, stream, "\x1b[?1002l\x1b[?1000l")
	if got, err := term.MouseTracking(); err != nil || got != MouseTrackingNone {
		t.Errorf("MouseTracking() after reset = %v, %v; want none, nil", got, err)
	}
}

func TestPixelSize(t *testing.T) {
	term := newTerm(t, 20, 5)

	if got, err := term.WidthPx(); err != nil || got != 0 {
		t.Errorf("WidthPx() = %v, %v; want 0, nil", got, err)
	}
	if err := term.ResizeCells(20, 5, 7, 15); err != nil {
		t.Fatalf("ResizeCells: %v", err)
	}
	if got, err := term.WidthPx(); err != nil || got != 20*7 {
		t.Errorf("WidthPx() = %v, %v; want 140, nil", got, err)
	}
	if got, err := term.HeightPx(); err != nil || got != 5*15 {
		t.Errorf("HeightPx() = %v, %v; want 75, nil", got, err)
	}
}

func TestTerminalFlags(t *testing.T) {
	term := newTerm(t, 20, 5)

	// Unknown focus and visibility are reported optimistically, so an
	// embedder that never tells the terminal behaves as if it were on screen.
	if got, err := term.Focused(); err != nil || !got {
		t.Errorf("Focused() = %v, %v; want true, nil", got, err)
	}
	if got, err := term.Visible(); err != nil || !got {
		t.Errorf("Visible() = %v, %v; want true, nil", got, err)
	}
	if got, err := term.PasswordInput(); err != nil || got {
		t.Errorf("PasswordInput() = %v, %v; want false, nil", got, err)
	}
}

func TestModeReport(t *testing.T) {
	term, stream := newStreamPair(t, 20, 5)

	// DEC 25 (cursor visible) defaults to set.
	if got, err := term.ModeReport(25, false); err != nil || got != ModeReportSet {
		t.Errorf("ModeReport(25) = %v, %v; want set, nil", got, err)
	}
	feed(t, stream, "\x1b[?25l")
	if got, err := term.ModeReport(25, false); err != nil || got != ModeReportReset {
		t.Errorf("ModeReport(25) after reset = %v, %v; want reset, nil", got, err)
	}

	// ANSI 4 (IRM) lives in the other namespace: the same number as DEC 4.
	if got, err := term.ModeReport(4, true); err != nil || got != ModeReportReset {
		t.Errorf("ModeReport(4, ansi) = %v, %v; want reset, nil", got, err)
	}
	feed(t, stream, "\x1b[4h")
	if got, err := term.ModeReport(4, true); err != nil || got != ModeReportSet {
		t.Errorf("ModeReport(4, ansi) after set = %v, %v; want set, nil", got, err)
	}

	// DECECM is fixed off in ghostty rather than unimplemented.
	if got, err := term.ModeReport(117, false); err != nil || got != ModeReportPermanentlyReset {
		t.Errorf("ModeReport(117) = %v, %v; want permanently_reset, nil", got, err)
	}
	if got, err := term.ModeReport(9999, false); err != nil || got != ModeReportNotRecognized {
		t.Errorf("ModeReport(9999) = %v, %v; want not_recognized, nil", got, err)
	}
}
