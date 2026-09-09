package main

import (
	"testing"

	"github.com/ironpark/gostty/examples/hypercat/keys"
	"github.com/ironpark/gostty/input"
)

// sent is what one key event puts on the wire for this tab's terminal.
func sent(t *testing.T, tab *terminalTab, ev keys.Event, m keys.Mods) string {
	t.Helper()
	tab.out.reset()
	if err := tab.sendKey(ev, m); err != nil {
		t.Fatalf("sendKey: %v", err)
	}
	return string(tab.out)
}

// The Kitty keyboard protocol describes the key, not the text: the program is
// told which key was pressed and what it would have typed unshifted. Leaving
// the unshifted codepoint at zero is not a small omission -- the encoder has
// nothing to name and silently falls back to the legacy bytes, so a program
// that asked for the protocol never sees it.
func TestKittyKeyboardNeedsTheWholeEvent(t *testing.T) {
	app := newTabTestApp(t)
	tab := app.current()

	press := keys.Event{Key: input.KeyKeyA, Action: input.KeyActionPress, Text: []byte("a")}
	if got, want := sent(t, tab, press, keys.Mods{}), "a"; got != want {
		t.Errorf("legacy press sent %q, want %q", got, want)
	}

	// The program asks for the protocol: disambiguate, report event types,
	// report alternate keys, report all keys as escape codes.
	feedTab(t, tab, "\x1b[>15u")
	if got, want := sent(t, tab, press, keys.Mods{}), "\x1b[97u"; got != want {
		t.Errorf("kitty press sent %q, want %q", got, want)
	}
}

// Releases and repeats are what the protocol adds beyond a press. The legacy
// encoding drops a release and treats a repeat as a press, so they are sent
// unconditionally and the encoder decides.
func TestKittyReportsRepeatsAndReleases(t *testing.T) {
	app := newTabTestApp(t)
	tab := app.current()

	// The arrow keys are the ones that repeat: a held letter is left to the
	// program, so only a key with no text of its own is sent again.
	release := keys.Event{Key: input.KeyArrowUp, Action: input.KeyActionRelease}
	repeat := keys.Event{Key: input.KeyArrowUp, Action: input.KeyActionRepeat}
	if got := sent(t, tab, release, keys.Mods{}); got != "" {
		t.Errorf("legacy release sent %q, want nothing", got)
	}
	if got, want := sent(t, tab, repeat, keys.Mods{}), "\x1b[A"; got != want {
		t.Errorf("legacy repeat sent %q, want %q", got, want)
	}

	feedTab(t, tab, "\x1b[>15u")
	if got, want := sent(t, tab, release, keys.Mods{}), "\x1b[1;1:3A"; got != want {
		t.Errorf("kitty release sent %q, want %q", got, want)
	}
	if got, want := sent(t, tab, repeat, keys.Mods{}), "\x1b[1;1:2A"; got != want {
		t.Errorf("kitty repeat sent %q, want %q", got, want)
	}
}

// Shift that made a capital is reported as consumed, which is what the
// protocol needs to tell a typed "A" from Shift+A as a chord.
func TestKittyShiftIsConsumedByTheText(t *testing.T) {
	app := newTabTestApp(t)
	tab := app.current()
	feedTab(t, tab, "\x1b[>15u")

	press := keys.Event{Key: input.KeyKeyA, Action: input.KeyActionPress, Text: []byte("A")}
	if got, want := sent(t, tab, press, keys.Mods{Shift: true}), "\x1b[97:65;2u"; got != want {
		t.Errorf("shifted press sent %q, want %q", got, want)
	}
}

// The modifiers that went into the text are consumed: shift made the capital,
// so it is not also a modifier the program is told about.
func TestConsumedModifiers(t *testing.T) {
	withText := keys.Mods{Shift: true}.Consumed(true)
	if !withText.Shift {
		t.Error("shift was not consumed by the text it produced")
	}
	if noText := (keys.Mods{Shift: true}).Consumed(false); noText.Shift {
		t.Error("shift was consumed although the key produced no text")
	}
	// Ctrl and Super never make text, whatever else is held.
	if all := (keys.Mods{Ctrl: true, Super: true, Shift: true}).Consumed(true); all.Ctrl || all.Super {
		t.Errorf("Consumed() = %+v, want only the text-making modifiers", all)
	}
}
