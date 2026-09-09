package main

import (
	"testing"

	"github.com/ironpark/gostty/examples/hypercat/keys"
)

// A link is OSC 8: the URI is the terminal's, not a pattern matched against
// the text. The pointer is at the window's origin in a test, so the link is
// printed where it will be found.
func TestHoveredLinkCoversTheWholeRun(t *testing.T) {
	app := newTabTestApp(t)
	tab := app.current()
	tab.reports.focused = true

	feedTab(t, tab, "\x1b]8;;https://example.com\x07click\x1b]8;;\x07 plain")
	if err := tab.refreshLink(); err != nil {
		t.Fatalf("refreshLink: %v", err)
	}
	if !tab.link.valid() {
		t.Fatal("no link under the pointer")
	}
	if got, want := tab.link.uri, "https://example.com"; got != want {
		t.Errorf("link uri = %q, want %q", got, want)
	}
	// The underline covers the linked text and stops there, rather than the one
	// cell being pointed at or the rest of the row.
	if tab.link.start != 0 || tab.link.end != len("click") {
		t.Errorf("link run = [%d,%d), want the five cells of \"click\"", tab.link.start, tab.link.end)
	}
	if tab.link.row != 0 {
		t.Errorf("link row = %d, want 0", tab.link.row)
	}
}

// Text with no link under the pointer leaves nothing to underline or open.
func TestUnlinkedTextHasNoLink(t *testing.T) {
	app := newTabTestApp(t)
	tab := app.current()
	tab.reports.focused = true

	feedTab(t, tab, "https://example.com typed out, which is not a link")
	if err := tab.refreshLink(); err != nil {
		t.Fatalf("refreshLink: %v", err)
	}
	if tab.link.valid() {
		t.Errorf("found a link %q in text that only looks like one", tab.link.uri)
	}
	// And with no link there is nothing a modified click can open.
	if tab.openLink(keys.Mods{Super: true}) {
		t.Error("openLink reported it had opened something")
	}
}

// A plain click selects; opening a link takes the shortcut modifier, so a link
// under the pointer cannot turn an ordinary click into navigating away.
func TestOpeningALinkTakesTheShortcutModifier(t *testing.T) {
	app := newTabTestApp(t)
	tab := app.current()
	tab.link = hoveredLink{uri: "https://example.com", row: 0, start: 0, end: 5}

	if tab.openLink(keys.Mods{}) {
		t.Error("a plain click opened a link")
	}
}

// A URI out of a pty is not to be handed to the platform opener whatever it
// says: only the schemes a terminal is for are opened.
func TestOnlyKnownSchemesAreOpenable(t *testing.T) {
	for _, test := range []struct {
		uri  string
		want bool
	}{
		{"https://example.com", true},
		{"http://example.com", true},
		{"mailto:someone@example.com", true},
		{"file:///tmp/notes.txt", true},
		{"javascript:alert(1)", false},
		{"vnd.dangerous://run", false},
		{"not a uri at all", false},
	} {
		if got := openable(test.uri); got != test.want {
			t.Errorf("openable(%q) = %v, want %v", test.uri, got, test.want)
		}
	}
}
