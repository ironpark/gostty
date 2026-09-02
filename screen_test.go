package gostty

import (
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
