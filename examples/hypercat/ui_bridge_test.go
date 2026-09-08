package main

import (
	"testing"

	"github.com/ironpark/gostty/examples/hypercat/ui"
)

func TestSearchPanelActionsManageNativeSearch(t *testing.T) {
	app := newTabTestApp(t)
	tab := app.current()
	if err := tab.stream.Feed([]byte("needle in the terminal")); err != nil {
		t.Fatal(err)
	}
	handle := func(in ui.Input) {
		t.Helper()
		consumed, err := tab.applyUIActions(tab.panels.Handle(in))
		if err != nil {
			t.Fatal(err)
		}
		if !consumed {
			t.Fatal("panel input was not consumed")
		}
	}
	handle(ui.Input{OpenSearch: true})
	handle(ui.Input{Chars: []rune("needle")})
	if tab.search == nil {
		t.Fatal("query did not create native search")
	}
	if err := tab.tickSearch(); err != nil {
		t.Fatal(err)
	}
	if tab.panels.Search.Matches == 0 {
		t.Fatal("native results did not reach the search panel")
	}
	handle(ui.Input{Enter: true})
	search := tab.search
	handle(ui.Input{Close: true})
	if tab.search != nil || tab.panels.Mode != ui.None {
		t.Fatal("closing panel retained native search")
	}
	if _, err := search.MatchCount(); err == nil {
		t.Fatal("native search handle was not closed")
	}
}
