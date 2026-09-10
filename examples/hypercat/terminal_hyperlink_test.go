package main

import (
	"testing"

	"github.com/ironpark/gostty/examples/hypercat/keys"
)

// A link is OSC 8: the URI is the terminal's, not a pattern matched against
// the text. The pointer is at the window's origin in a test, so the link is
// printed where it will be found.
func TestHoveredLinkCoversTheWholeRun(t *testing.T) {
	win := newTabTestApp(t)
	tab := win.current()
	tab.reports.focused = true

	feedTab(t, tab, "\x1b]8;;https://example.com\x07click\x1b]8;;\x07 plain")
	if err := tab.refreshLink(); err != nil {
		t.Fatalf("refreshLink: %v", err)
	}
	if !tab.frame.link.valid() {
		t.Fatal("no link under the pointer")
	}
	if got, want := tab.frame.link.uri, "https://example.com"; got != want {
		t.Errorf("link uri = %q, want %q", got, want)
	}
	// The underline covers the linked text and stops there, rather than the one
	// cell being pointed at or the rest of the row.
	if tab.frame.link.start != 0 || tab.frame.link.end != len("click") {
		t.Errorf("link run = [%d,%d), want the five cells of \"click\"", tab.frame.link.start, tab.frame.link.end)
	}
	if tab.frame.link.row != 0 {
		t.Errorf("link row = %d, want 0", tab.frame.link.row)
	}
}

// The run is cached while the pointer stays inside it, so a pointer resting on
// a link does not walk the row again every frame. What is cached has to be the
// same run: the cell under the pointer is still asked, every frame.
func TestHoveredLinkIsNotRescannedWhileTheRunHolds(t *testing.T) {
	win := newTabTestApp(t)
	tab := win.current()
	tab.reports.focused = true

	feedTab(t, tab, "\x1b]8;;https://example.com\x07click\x1b]8;;\x07")
	if err := tab.refreshLink(); err != nil {
		t.Fatalf("refreshLink: %v", err)
	}
	first := tab.frame.link

	// A second look with nothing rewritten keeps the same run.
	if err := tab.refreshLink(); err != nil {
		t.Fatalf("refreshLink: %v", err)
	}
	if tab.frame.link != first {
		t.Errorf("link = %+v, want the cached %+v", tab.frame.link, first)
	}

	// Printing over it is a rewritten row, so the run is found again -- and
	// there is no longer a link there to find.
	feedTab(t, tab, "\x1b[H\x1b[2Kplain")
	if err := tab.refreshLink(); err != nil {
		t.Fatalf("refreshLink: %v", err)
	}
	if tab.frame.link.valid() {
		t.Errorf("link = %+v, want none once the text was overwritten", tab.frame.link)
	}
}

// Text with no link under the pointer leaves nothing to underline or open.
func TestUnlinkedTextHasNoLink(t *testing.T) {
	win := newTabTestApp(t)
	tab := win.current()
	tab.reports.focused = true

	feedTab(t, tab, "https://example.com typed out, which is not a link")
	if err := tab.refreshLink(); err != nil {
		t.Fatalf("refreshLink: %v", err)
	}
	if tab.frame.link.valid() {
		t.Errorf("found a link %q in text that only looks like one", tab.frame.link.uri)
	}
	// And with no link there is nothing a modified click can open.
	if tab.openLink(keys.Mods{Super: true}) {
		t.Error("openLink reported it had opened something")
	}
}

// A plain click selects; opening a link takes the shortcut modifier, so a link
// under the pointer cannot turn an ordinary click into navigating away.
func TestOpeningALinkTakesTheShortcutModifier(t *testing.T) {
	win := newTabTestApp(t)
	tab := win.current()
	tab.frame.link = hoveredLink{uri: "https://example.com", row: 0, start: 0, end: 5}

	if tab.openLink(keys.Mods{}) {
		t.Error("a plain click opened a link")
	}
}
