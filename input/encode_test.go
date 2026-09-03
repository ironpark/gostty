package input_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ironpark/gostty"
	"github.com/ironpark/gostty/input"
)

// Local copies of the terminal helpers: these tests live in their own package.

func newTerm(t *testing.T, cols, rows uint16) *gostty.Terminal {
	t.Helper()
	term, err := gostty.NewTerminal(cols, rows)
	if err != nil {
		t.Fatalf("NewTerminal: %v", err)
	}
	t.Cleanup(func() { term.Close() })
	return term
}

func newStreamPair(t *testing.T, cols, rows uint16) (*gostty.Terminal, *gostty.Stream) {
	t.Helper()
	term, err := gostty.NewTerminal(cols, rows)
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

func feed(t *testing.T, s *gostty.Stream, data string) {
	t.Helper()
	if err := s.Feed([]byte(data)); err != nil {
		t.Fatalf("Feed(%q): %v", data, err)
	}
}

func screen(t *testing.T, term *gostty.Terminal) string {
	t.Helper()
	got, err := term.PlainString()
	if err != nil {
		t.Fatalf("PlainString: %v", err)
	}
	return strings.TrimRight(string(got), "\n")
}

func newKeyEv(t *testing.T) *input.KeyEvent {
	t.Helper()
	ev, err := input.NewKeyEvent()
	if err != nil {
		t.Fatalf("input.NewKeyEvent: %v", err)
	}
	t.Cleanup(func() { ev.Close() })
	return ev
}

func encodeKey(t *testing.T, term *gostty.Terminal, ev *input.KeyEvent) string {
	t.Helper()
	var buf bytes.Buffer
	if err := input.EncodeKey(&buf, term, ev); err != nil {
		t.Fatalf("input.EncodeKey: %v", err)
	}
	return buf.String()
}

func TestEncodePlainText(t *testing.T) {
	term := newTerm(t, 20, 3)
	ev := newKeyEv(t)
	if err := ev.SetKey(input.KeyKeyA); err != nil {
		t.Fatalf("SetKey: %v", err)
	}
	if err := ev.SetUTF8([]byte("a")); err != nil {
		t.Fatalf("SetUTF8: %v", err)
	}
	if got, want := encodeKey(t, term, ev), "a"; got != want {
		t.Errorf("encoded %q, want %q", got, want)
	}
}

func TestEncodeCtrlKey(t *testing.T) {
	term := newTerm(t, 20, 3)
	ev := newKeyEv(t)
	if err := ev.SetKey(input.KeyKeyC); err != nil {
		t.Fatalf("SetKey: %v", err)
	}
	if err := ev.SetMod(input.KeyModCtrl, true); err != nil {
		t.Fatalf("SetMod: %v", err)
	}
	// ctrl+c is 0x03.
	if got, want := encodeKey(t, term, ev), "\x03"; got != want {
		t.Errorf("encoded %q, want %q", got, want)
	}
}

func TestEncodeEnter(t *testing.T) {
	term := newTerm(t, 20, 3)
	ev := newKeyEv(t)
	if err := ev.SetKey(input.KeyEnter); err != nil {
		t.Fatalf("SetKey: %v", err)
	}
	if got, want := encodeKey(t, term, ev), "\r"; got != want {
		t.Errorf("encoded %q, want %q", got, want)
	}
}

// DECCKM (mode 1) switches the arrow keys between CSI and SS3 forms, so the
// encoding has to follow the terminal's current state.
func TestEncodeArrowFollowsCursorKeyMode(t *testing.T) {
	term, stream := newStreamPair(t, 20, 3)
	ev := newKeyEv(t)
	if err := ev.SetKey(input.KeyArrowUp); err != nil {
		t.Fatalf("SetKey: %v", err)
	}

	if got, want := encodeKey(t, term, ev), "\x1b[A"; got != want {
		t.Errorf("normal mode encoded %q, want %q", got, want)
	}

	feed(t, stream, "\x1b[?1h") // DECCKM on
	if got, want := encodeKey(t, term, ev), "\x1bOA"; got != want {
		t.Errorf("application mode encoded %q, want %q", got, want)
	}
}

func TestKeyEventReset(t *testing.T) {
	term := newTerm(t, 20, 3)
	ev := newKeyEv(t)
	if err := ev.SetKey(input.KeyKeyC); err != nil {
		t.Fatalf("SetKey: %v", err)
	}
	if err := ev.SetMod(input.KeyModCtrl, true); err != nil {
		t.Fatalf("SetMod: %v", err)
	}
	if got := encodeKey(t, term, ev); got != "\x03" {
		t.Fatalf("setup encoded %q, want %q", got, "\x03")
	}

	if err := ev.Reset(); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if err := ev.SetKey(input.KeyEnter); err != nil {
		t.Fatalf("SetKey: %v", err)
	}
	if got, want := encodeKey(t, term, ev), "\r"; got != want {
		t.Errorf("after Reset encoded %q, want %q", got, want)
	}
}

func TestEncodeFocus(t *testing.T) {
	for _, tc := range []struct {
		event input.FocusEvent
		want  string
	}{
		{input.FocusEventGained, "\x1b[I"},
		{input.FocusEventLost, "\x1b[O"},
	} {
		var buf bytes.Buffer
		if err := input.EncodeFocus(&buf, tc.event); err != nil {
			t.Fatalf("input.EncodeFocus(%v): %v", tc.event, err)
		}
		if buf.String() != tc.want {
			t.Errorf("input.EncodeFocus(%v) = %q, want %q", tc.event, buf.String(), tc.want)
		}
	}
}

func TestIsSafePaste(t *testing.T) {
	if !input.IsSafePaste([]byte("plain text")) {
		t.Error("plain text reported unsafe")
	}
	if input.IsSafePaste([]byte("two\nlines")) {
		t.Error("text with a newline reported safe")
	}
	if input.IsSafePaste([]byte("escape \x1b[201~ hatch")) {
		t.Error("text with a bracketed-paste terminator reported safe")
	}
}

// Bracketed paste mode (2004) wraps the payload; without it the bytes go
// through untouched.
func TestEncodePasteFollowsBracketedMode(t *testing.T) {
	term, stream := newStreamPair(t, 20, 3)

	var plain bytes.Buffer
	if err := input.EncodePaste(&plain, term, []byte("hello")); err != nil {
		t.Fatalf("input.EncodePaste: %v", err)
	}
	if got, want := plain.String(), "hello"; got != want {
		t.Errorf("unbracketed paste = %q, want %q", got, want)
	}

	feed(t, stream, "\x1b[?2004h")
	var bracketed bytes.Buffer
	if err := input.EncodePaste(&bracketed, term, []byte("hello")); err != nil {
		t.Fatalf("input.EncodePaste: %v", err)
	}
	if got, want := bracketed.String(), "\x1b[200~hello\x1b[201~"; got != want {
		t.Errorf("bracketed paste = %q, want %q", got, want)
	}
}

// A key event round-trips through the terminal it targets.
func TestKeyEventThroughStream(t *testing.T) {
	term, stream := newStreamPair(t, 20, 3)
	ev := newKeyEv(t)
	if err := ev.SetKey(input.KeyKeyH); err != nil {
		t.Fatalf("SetKey: %v", err)
	}
	if err := ev.SetUTF8([]byte("h")); err != nil {
		t.Fatalf("SetUTF8: %v", err)
	}

	var buf bytes.Buffer
	if err := input.EncodeKey(&buf, term, ev); err != nil {
		t.Fatalf("input.EncodeKey: %v", err)
	}
	feed(t, stream, buf.String())

	if got, want := screen(t, term), "h"; got != want {
		t.Errorf("screen = %q, want %q", got, want)
	}
}

func newMouseEv(t *testing.T) *input.MouseEvent {
	t.Helper()
	ev, err := input.NewMouseEvent()
	if err != nil {
		t.Fatalf("input.NewMouseEvent: %v", err)
	}
	t.Cleanup(func() { ev.Close() })
	return ev
}

// A 10x20 pixel cell with no padding, so pixel (x, y) is cell (x/10, y/20).
var testSize = input.RenderSize{
	ScreenWidth:  800,
	ScreenHeight: 480,
	CellWidth:    10,
	CellHeight:   20,
}

func encodeMouse(t *testing.T, term *gostty.Terminal, ev *input.MouseEvent, pressed bool) string {
	t.Helper()
	var buf bytes.Buffer
	if err := input.EncodeMouse(&buf, term, ev, testSize, pressed); err != nil {
		t.Fatalf("input.EncodeMouse: %v", err)
	}
	return buf.String()
}

// Nothing is reported until the program turns mouse tracking on.
func TestEncodeMouseSilentWithoutTracking(t *testing.T) {
	term, _ := newStreamPair(t, 80, 24)
	ev := newMouseEv(t)
	if err := ev.SetAction(input.MouseActionPress); err != nil {
		t.Fatalf("SetAction: %v", err)
	}
	if err := ev.SetButton(input.MouseButtonLeft); err != nil {
		t.Fatalf("SetButton: %v", err)
	}
	if err := ev.SetPosition(0, 0); err != nil {
		t.Fatalf("SetPosition: %v", err)
	}
	if got := encodeMouse(t, term, ev, true); got != "" {
		t.Errorf("encoded %q with tracking off, want nothing", got)
	}
}

// Mode 1000 turns on button tracking; mode 1006 selects the SGR format.
func TestEncodeMouseSGR(t *testing.T) {
	term, stream := newStreamPair(t, 80, 24)
	feed(t, stream, "\x1b[?1000h\x1b[?1006h")

	ev := newMouseEv(t)
	if err := ev.SetAction(input.MouseActionPress); err != nil {
		t.Fatalf("SetAction: %v", err)
	}
	if err := ev.SetButton(input.MouseButtonLeft); err != nil {
		t.Fatalf("SetButton: %v", err)
	}
	// Pixel (25, 45) with a 10x20 cell is column 3, row 3 one-based.
	if err := ev.SetPosition(25, 45); err != nil {
		t.Fatalf("SetPosition: %v", err)
	}
	if got, want := encodeMouse(t, term, ev, true), "\x1b[<0;3;3M"; got != want {
		t.Errorf("press encoded %q, want %q", got, want)
	}

	if err := ev.SetAction(input.MouseActionRelease); err != nil {
		t.Fatalf("SetAction: %v", err)
	}
	if got, want := encodeMouse(t, term, ev, false), "\x1b[<0;3;3m"; got != want {
		t.Errorf("release encoded %q, want %q", got, want)
	}
}

// The button and modifier bits land in the same field.
func TestEncodeMouseModifiers(t *testing.T) {
	term, stream := newStreamPair(t, 80, 24)
	feed(t, stream, "\x1b[?1000h\x1b[?1006h")

	ev := newMouseEv(t)
	if err := ev.SetAction(input.MouseActionPress); err != nil {
		t.Fatalf("SetAction: %v", err)
	}
	if err := ev.SetButton(input.MouseButtonRight); err != nil {
		t.Fatalf("SetButton: %v", err)
	}
	if err := ev.SetMod(input.KeyModShift, true); err != nil {
		t.Fatalf("SetMod: %v", err)
	}
	if err := ev.SetPosition(0, 0); err != nil {
		t.Fatalf("SetPosition: %v", err)
	}
	// Right button is 2, shift adds 4.
	if got, want := encodeMouse(t, term, ev, true), "\x1b[<6;1;1M"; got != want {
		t.Errorf("encoded %q, want %q", got, want)
	}
}

func TestMouseEventReset(t *testing.T) {
	term, stream := newStreamPair(t, 80, 24)
	feed(t, stream, "\x1b[?1000h\x1b[?1006h")

	ev := newMouseEv(t)
	if err := ev.SetButton(input.MouseButtonRight); err != nil {
		t.Fatalf("SetButton: %v", err)
	}
	if err := ev.SetMod(input.KeyModShift, true); err != nil {
		t.Fatalf("SetMod: %v", err)
	}
	if err := ev.Reset(); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if err := ev.SetButton(input.MouseButtonLeft); err != nil {
		t.Fatalf("SetButton: %v", err)
	}
	if err := ev.SetPosition(0, 0); err != nil {
		t.Fatalf("SetPosition: %v", err)
	}
	if got, want := encodeMouse(t, term, ev, true), "\x1b[<0;1;1M"; got != want {
		t.Errorf("after Reset encoded %q, want %q", got, want)
	}
}

// Text that no physical key describes -- anything from an IME, and most
// non-Latin layouts -- is encoded from the UTF-8 alone. An embedder that has a
// composed string but no key code needs this to work.
func TestEncodeTextWithoutKey(t *testing.T) {
	term := newTerm(t, 20, 3)
	ev := newKeyEv(t)
	if err := ev.SetKey(input.KeyUnidentified); err != nil {
		t.Fatalf("SetKey: %v", err)
	}
	if err := ev.SetUTF8([]byte("안")); err != nil {
		t.Fatalf("SetUTF8: %v", err)
	}
	if got, want := encodeKey(t, term, ev), "안"; got != want {
		t.Errorf("encoded %q, want %q", got, want)
	}
}
