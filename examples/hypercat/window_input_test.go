package main

import (
	"errors"
	"testing"

	"github.com/ironpark/gostty/examples/hypercat/internal/frontend"
	"github.com/ironpark/gostty/examples/hypercat/keys"
	"github.com/ironpark/gostty/examples/hypercat/ui"
	"github.com/ironpark/gostty/input"
)

// Drive the new boundary with supplied input: no keyboard or mouse polling is
// needed to verify that tabs/panels take priority over terminal encoding.
func TestFrontendInputPreservesRouting(t *testing.T) {
	win := newTabTestApp(t)
	tab := win.current()
	feedTab(t, tab, "\x1b[>15u")
	reads := 0
	in := frontend.Input{
		Focused: true, Y: 100, DeltaSeconds: 1.0 / 60,
		ReadKeys: func(focusedFrames int) []keys.Event {
			reads++
			if focusedFrames != 1 {
				t.Errorf("first input after focus: held limit = %d, want 1", focusedFrames)
			}
			return []keys.Event{{Key: input.KeyKeyA, Action: input.KeyActionPress, Text: []byte("a")}}
		},
	}
	if err := win.Update(in); err != nil {
		t.Fatal(err)
	}
	if reads != 1 || string(tab.out) != "\x1b[97u" {
		t.Fatalf("terminal input: reads=%d, encoded=%q", reads, tab.out)
	}

	in.Panel = ui.Input{OpenSearch: true}
	if err := win.Update(in); err != nil {
		t.Fatal(err)
	}
	if reads != 1 || tab.panels.Mode != ui.Search {
		t.Fatal("opening search let typing reach the terminal")
	}
	in.Panel = ui.Input{Chars: []rune("needle")}
	if err := win.Update(in); err != nil {
		t.Fatal(err)
	}
	if reads != 1 || string(tab.panels.Search.Query) != "needle" {
		t.Fatal("search did not own the supplied text")
	}

	in.Mods = keys.Mods{Super: true}
	in.PressedKeys = map[input.Key]bool{input.KeyKeyT: true}
	if err := win.Update(in); err != nil {
		t.Fatal(err)
	}
	if reads != 1 || len(win.tabs) != 2 || win.current() == tab {
		t.Fatal("new-tab shortcut did not take priority over search and terminal input")
	}
	in.PressedKeys = map[input.Key]bool{input.KeyKeyW: true}
	if err := win.Update(in); err != nil {
		t.Fatal(err)
	}
	if err := win.Update(in); !errors.Is(err, frontend.ErrClosed) {
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
		in := frontend.Input{
			Focused: true, X: int(x + w/2), Y: int(y+h/2) + win.current().offsetY,
			Left: frontend.Button{Pressed: true, Down: true}, DeltaSeconds: 1.0 / 60,
		}
		if err := win.Update(in); err != nil {
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
