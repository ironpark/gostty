package gostty

import (
	"bytes"
	"testing"
)

func newKeyEv(t *testing.T) *KeyEvent {
	t.Helper()
	ev, err := NewKeyEvent()
	if err != nil {
		t.Fatalf("NewKeyEvent: %v", err)
	}
	t.Cleanup(func() { ev.Close() })
	return ev
}

func encodeKey(t *testing.T, term *Terminal, ev *KeyEvent) string {
	t.Helper()
	var buf bytes.Buffer
	if err := EncodeKey(&buf, term, ev); err != nil {
		t.Fatalf("EncodeKey: %v", err)
	}
	return buf.String()
}

func TestEncodePlainText(t *testing.T) {
	term := newTerm(t, 20, 3)
	ev := newKeyEv(t)
	if err := ev.SetKey(KeyKeyA); err != nil {
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
	if err := ev.SetKey(KeyKeyC); err != nil {
		t.Fatalf("SetKey: %v", err)
	}
	if err := ev.SetMod(KeyModCtrl, true); err != nil {
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
	if err := ev.SetKey(KeyEnter); err != nil {
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
	if err := ev.SetKey(KeyArrowUp); err != nil {
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
	if err := ev.SetKey(KeyKeyC); err != nil {
		t.Fatalf("SetKey: %v", err)
	}
	if err := ev.SetMod(KeyModCtrl, true); err != nil {
		t.Fatalf("SetMod: %v", err)
	}
	if got := encodeKey(t, term, ev); got != "\x03" {
		t.Fatalf("setup encoded %q, want %q", got, "\x03")
	}

	if err := ev.Reset(); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if err := ev.SetKey(KeyEnter); err != nil {
		t.Fatalf("SetKey: %v", err)
	}
	if got, want := encodeKey(t, term, ev), "\r"; got != want {
		t.Errorf("after Reset encoded %q, want %q", got, want)
	}
}

func TestEncodeFocus(t *testing.T) {
	for _, tc := range []struct {
		event FocusEvent
		want  string
	}{
		{FocusEventGained, "\x1b[I"},
		{FocusEventLost, "\x1b[O"},
	} {
		var buf bytes.Buffer
		if err := EncodeFocus(&buf, tc.event); err != nil {
			t.Fatalf("EncodeFocus(%v): %v", tc.event, err)
		}
		if buf.String() != tc.want {
			t.Errorf("EncodeFocus(%v) = %q, want %q", tc.event, buf.String(), tc.want)
		}
	}
}

func TestIsSafePaste(t *testing.T) {
	if !IsSafePaste([]byte("plain text")) {
		t.Error("plain text reported unsafe")
	}
	if IsSafePaste([]byte("two\nlines")) {
		t.Error("text with a newline reported safe")
	}
	if IsSafePaste([]byte("escape \x1b[201~ hatch")) {
		t.Error("text with a bracketed-paste terminator reported safe")
	}
}

// Bracketed paste mode (2004) wraps the payload; without it the bytes go
// through untouched.
func TestEncodePasteFollowsBracketedMode(t *testing.T) {
	term, stream := newStreamPair(t, 20, 3)

	var plain bytes.Buffer
	if err := EncodePaste(&plain, term, []byte("hello")); err != nil {
		t.Fatalf("EncodePaste: %v", err)
	}
	if got, want := plain.String(), "hello"; got != want {
		t.Errorf("unbracketed paste = %q, want %q", got, want)
	}

	feed(t, stream, "\x1b[?2004h")
	var bracketed bytes.Buffer
	if err := EncodePaste(&bracketed, term, []byte("hello")); err != nil {
		t.Fatalf("EncodePaste: %v", err)
	}
	if got, want := bracketed.String(), "\x1b[200~hello\x1b[201~"; got != want {
		t.Errorf("bracketed paste = %q, want %q", got, want)
	}
}

// A key event round-trips through the terminal it targets.
func TestKeyEventThroughStream(t *testing.T) {
	term, stream := newStreamPair(t, 20, 3)
	ev := newKeyEv(t)
	if err := ev.SetKey(KeyKeyH); err != nil {
		t.Fatalf("SetKey: %v", err)
	}
	if err := ev.SetUTF8([]byte("h")); err != nil {
		t.Fatalf("SetUTF8: %v", err)
	}

	var buf bytes.Buffer
	if err := EncodeKey(&buf, term, ev); err != nil {
		t.Fatalf("EncodeKey: %v", err)
	}
	feed(t, stream, buf.String())

	if got, want := screen(t, term), "h"; got != want {
		t.Errorf("screen = %q, want %q", got, want)
	}
}
