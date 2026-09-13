package main

import (
	"errors"
	"testing"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/ironpark/gostty/examples/hypercat/keys"
	"github.com/ironpark/gostty/examples/hypercat/ui"
	"github.com/ironpark/gostty/input"
)

// The Kitty keyboard protocol describes the key, not the text: the program is
// told which key was pressed and what it would have typed unshifted. Leaving
// the unshifted codepoint at zero is not a small omission -- the encoder has
// nothing to name and silently falls back to the legacy bytes, so a program
// that asked for the protocol never sees it.
func TestKittyKeyboardNeedsTheWholeEvent(t *testing.T) {
	win := newTabTestApp(t)
	tab := win.current()

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
	win := newTabTestApp(t)
	tab := win.current()

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
	win := newTabTestApp(t)
	tab := win.current()
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

// Drive the new boundary with supplied input: no keyboard or mouse polling is
// needed to verify that tabs/panels take priority over terminal encoding.
func TestHostInputPreservesRouting(t *testing.T) {
	win := newTabTestApp(t)
	tab := win.current()
	feedTab(t, tab, "\x1b[>15u")
	reads := 0
	in := hostInput{
		Focused: true, Y: 100, DeltaSeconds: 1.0 / 60,
		ReadKeys: func(focusedFrames int) []keys.Event {
			reads++
			if focusedFrames != 1 {
				t.Errorf("first input after focus: held limit = %d, want 1", focusedFrames)
			}
			return []keys.Event{{Key: input.KeyKeyA, Action: input.KeyActionPress, Text: []byte("a")}}
		},
	}
	if err := win.update(in); err != nil {
		t.Fatal(err)
	}
	if reads != 1 || string(tab.out) != "\x1b[97u" {
		t.Fatalf("terminal input: reads=%d, encoded=%q", reads, tab.out)
	}

	in.Panel = ui.Input{OpenSearch: true}
	if err := win.update(in); err != nil {
		t.Fatal(err)
	}
	if reads != 1 || tab.panels.Mode != ui.Search {
		t.Fatal("opening search let typing reach the terminal")
	}
	in.Panel = ui.Input{Chars: []rune("needle")}
	if err := win.update(in); err != nil {
		t.Fatal(err)
	}
	if reads != 1 || string(tab.panels.Search.Query) != "needle" {
		t.Fatal("search did not own the supplied text")
	}

	in.Mods = keys.Mods{Super: true}
	in.PressedKeys = map[input.Key]bool{input.KeyKeyT: true}
	if err := win.update(in); err != nil {
		t.Fatal(err)
	}
	if reads != 1 || len(win.tabs) != 2 || win.current() == tab {
		t.Fatal("new-tab shortcut did not take priority over search and terminal input")
	}
	in.PressedKeys = map[input.Key]bool{input.KeyKeyW: true}
	if err := win.update(in); err != nil {
		t.Fatal(err)
	}
	if err := win.update(in); !errors.Is(err, ebiten.Termination) {
		t.Fatalf("closing the last tab returned %v, want ErrClosed", err)
	}
	if reads != 1 {
		t.Fatal("closing a tab let typing reach a terminal")
	}
}

func TestSharedCatClickOpensOnlyActiveTabSettings(t *testing.T) {
	win := newTabTestApp(t)
	if win.cat == nil {
		t.Fatal("window companion did not load")
	}
	if err := win.addTab(); err != nil {
		t.Fatal(err)
	}
	for index := range win.tabs {
		win.selectTab(index)
		for _, tab := range win.tabs {
			tab.panels.Mode = ui.None
		}
		win.cat.Place(120, 180)
		x, y, w, h := win.cat.Box()
		in := hostInput{
			Focused: true, X: int(x + w/2), Y: int(y+h/2) + win.current().offsetY,
			Left: button{Pressed: true, Down: true}, DeltaSeconds: 1.0 / 60,
		}
		if err := win.update(in); err != nil {
			t.Fatal(err)
		}
		for i, tab := range win.tabs {
			if (tab.panels.Mode == ui.Settings) != (i == index) {
				t.Fatalf("click on tab %d changed panel in tab %d", index, i)
			}
			if tab.sel.dragging {
				t.Fatal("cat click also started selection")
			}
		}
	}
}
