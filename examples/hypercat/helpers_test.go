package main

import (
	"testing"

	"github.com/ironpark/gostty"
	"github.com/ironpark/gostty/examples/hypercat/fonts"
	"github.com/ironpark/gostty/examples/hypercat/internal/appearance"
	"github.com/ironpark/gostty/examples/hypercat/internal/shelltest"
	"github.com/ironpark/gostty/examples/hypercat/keys"
)

// The helpers every test in this package builds on: a window with one tab
// running a shell, and the few things a test does with it. They live here
// rather than in whichever test file first needed them, so a new test can find
// them without knowing which one that was.

// newTabTestApp is a window with one tab, on a 10x20 cell so that pixel and
// cell positions convert by eye. The shell is this test binary; see
// `internal/shelltest`.
func newTabTestApp(t *testing.T) *window {
	t.Helper()
	shelltest.Shell(t, "echo")
	win := &window{dsf: 1, settings: testAppearance()}
	tab := &terminal{
		clipboard: &win.clipboard, settings: win.settings,
		cols: 80, rows: 24,
	}
	if err := tab.start(); err != nil {
		tab.close()
		t.Fatal(err)
	}
	win.tabs = []*terminal{tab}
	win.startCat()
	t.Cleanup(win.close)
	return win
}

// feedTab writes to a tab's terminal and brings the drawn state up to date,
// which is what a frame of shell output does.
func feedTab(t *testing.T, tab *terminal, s string) {
	t.Helper()
	if err := tab.stream.Feed([]byte(s)); err != nil {
		t.Fatalf("feed: %v", err)
	}
	if err := tab.refreshSnapshot(); err != nil {
		t.Fatalf("refresh: %v", err)
	}
}

// selected is what the tab's screen currently has selected.
func selected(t *testing.T, tab *terminal) string {
	t.Helper()
	text, ok, err := screenOf(t, tab).SelectionString()
	if err != nil {
		t.Fatalf("SelectionString: %v", err)
	}
	if !ok {
		return ""
	}
	return text
}

// sent is what one key event puts on the wire for this tab's terminal.
func sent(t *testing.T, tab *terminal, ev keys.Event, m keys.Mods) string {
	t.Helper()
	tab.out.reset()
	if err := tab.sendKey(ev, m); err != nil {
		t.Fatalf("sendKey: %v", err)
	}
	return string(tab.out)
}

// pressAtCell and dragToCell drive the selection gesture the way the frame
// loop does, in pixels, with the tab's own cell size.
func (tab *terminal) pressAtCell(t *testing.T, col, row int) {
	t.Helper()
	g := tab.grid()
	if err := tab.pressSelection(int(g.x(col))+1, int(g.y(row))); err != nil {
		t.Fatalf("pressSelection: %v", err)
	}
}

func (tab *terminal) dragToCell(t *testing.T, col, row int, rectangle bool) {
	t.Helper()
	g := tab.grid()
	// Most of the way into the cell, which is what includes it in the run.
	if err := tab.dragSelection(int(g.x(col+1))-1, int(g.y(row)), rectangle); err != nil {
		t.Fatalf("dragSelection: %v", err)
	}
}

// screenOf is the tab's active screen, for the tests that drive it directly.
func screenOf(t *testing.T, tab *terminal) *gostty.Screen {
	t.Helper()
	screen, err := tab.vt.ActiveScreen()
	if err != nil {
		t.Fatalf("ActiveScreen: %v", err)
	}
	return screen
}

// The tab tests run this binary as their shell, so it has to be able to be one.
func TestMain(m *testing.M) { shelltest.Main(m) }

// testAppearance supplies deterministic grid metrics without system discovery.
func testAppearance() *appearance.State {
	s := appearance.New(nil, fonts.DefaultSize, 1)
	s.SetMetrics(&fonts.Set{CellWidth: 10, CellHeight: 20})
	return s
}
