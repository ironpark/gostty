package main

import (
	"testing"

	"github.com/ironpark/gostty/examples/hypercat/ui"
)

func TestSearchPanelActionsManageNativeSearch(t *testing.T) {
	win := newTabTestApp(t)
	tab := win.current()
	if err := tab.stream.Feed([]byte("needle in the terminal")); err != nil {
		t.Fatal(err)
	}
	handle := func(in ui.Input) {
		t.Helper()
		consumed, err := win.applyPanel(tab, tab.panels.Handle(in))
		if err != nil {
			t.Fatal(err)
		}
		if !consumed {
			t.Fatal("panel input was not consumed")
		}
	}
	handle(ui.Input{OpenSearch: true})
	handle(ui.Input{Chars: []rune("needle")})
	if tab.search.handle == nil {
		t.Fatal("query did not create native search")
	}
	if err := tab.tickSearch(); err != nil {
		t.Fatal(err)
	}
	if tab.panels.Search.Matches == 0 {
		t.Fatal("native results did not reach the search panel")
	}
	handle(ui.Input{Enter: true})
	search := tab.search.handle
	handle(ui.Input{Close: true})
	if tab.search.handle != nil || tab.panels.Mode != ui.None {
		t.Fatal("closing panel retained native search")
	}
	if _, err := search.MatchCount(); err == nil {
		t.Fatal("native search handle was not closed")
	}
}

func TestSearchHighlightBuffersRefreshAndClear(t *testing.T) {
	win := newTabTestApp(t)
	tab := win.current()
	feedTab(t, tab, "needle plain")
	tab.panels.Search.Query = []rune("needle")
	if err := tab.runSearch(); err != nil {
		t.Fatal(err)
	}
	if err := tab.refreshSnapshot(); err != nil {
		t.Fatal(err)
	}
	if len(tab.search.cells) == 0 || !tab.search.cells[0] || tab.search.cells[7] {
		t.Fatal("search did not highlight only the matching cells")
	}
	tab.closeSearch()
	tab.frame.redraw.Clear()
	if err := tab.refreshMatches(); err != nil {
		t.Fatal(err)
	}
	if len(tab.search.cells) != 0 || !tab.frame.redraw.Take(0) {
		t.Fatal("closing search did not clear and repaint old highlights")
	}
	tab.panels.Search.Query = []rune("plain")
	if err := tab.runSearch(); err != nil {
		t.Fatal(err)
	}
	if err := tab.refreshSnapshot(); err != nil {
		t.Fatal(err)
	}
	if tab.search.cells[0] || !tab.search.cells[7] {
		t.Fatal("reopened search retained previous highlights")
	}
}

func TestSearchMaskReusePreservesPreviousHighlights(t *testing.T) {
	var search tabSearch
	search.resetMask(80 * 24)
	search.cells[7] = true
	search.resetMask(80 * 24)
	if !search.previous[7] || search.cells[7] {
		t.Fatal("reset lost previous highlights or retained new ones")
	}
	allocs := testing.AllocsPerRun(20, func() { search.resetMask(80 * 24) })
	if allocs != 0 {
		t.Fatalf("highlight buffers allocate %v times per frame", allocs)
	}
	search.cells[100] = true
	search.resetMask(40 * 12)
	if !search.previous[100] || len(search.cells) != 40*12 || search.cells[100] {
		t.Fatal("resizing lost previous highlights or retained new ones")
	}
	search.resetMask(100 * 40)
	if len(search.cells) != 100*40 {
		t.Fatal("mask did not grow with viewport")
	}
}
