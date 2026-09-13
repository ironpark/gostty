package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

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

// Clipboard, URL opener, temp files and dropped paths.

func TestLocalClipboardOwnsItsBytes(t *testing.T) {
	var c clipboard
	data := []byte("OSC text")
	c.hold(data)
	data[0] = 'x'
	if got := string(c.paste()); got != "OSC text" {
		t.Fatalf("held bytes changed: %q", got)
	}
	if err := c.copy("user copy"); err != nil {
		t.Fatal(err)
	}
	if got := string(c.paste()); got != "user copy" {
		t.Fatalf("paste = %q", got)
	}
	c.hold(nil)
	if len(c.paste()) != 0 {
		t.Fatal("empty clipboard retained content")
	}
}

func TestSaveTempDoesNotOverwritePreviousExports(t *testing.T) {
	dir := t.TempDir()
	names := make(map[string]bool)
	for _, body := range []string{"first", "second", "third"} {
		name, err := saveTemp(dir, "hypercat-*.html", func(out io.Writer) error { _, err := io.WriteString(out, body); return err })
		if err != nil {
			t.Fatal(err)
		}
		if names[name] {
			t.Fatalf("reused filename %q", name)
		}
		names[name] = true
		if filepath.Ext(name) != ".html" {
			t.Fatalf("export filename = %q", name)
		}
		got, err := os.ReadFile(name)
		if err != nil || string(got) != body {
			t.Fatalf("export = %q, %v, %v; want %q", got, err, body, err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 3 {
		t.Fatalf("exports = %v, %v", entries, err)
	}
}

func TestSaveTempRemovesIncompleteOutput(t *testing.T) {
	dir := t.TempDir()
	failed := errors.New("formatter failed")
	name, err := saveTemp(dir, "hypercat-*.html", func(out io.Writer) error {
		if _, err := io.WriteString(out, "partial"); err != nil {
			t.Fatal(err)
		}
		return failed
	})
	if name != "" || !errors.Is(err, failed) {
		t.Fatalf("SaveTemp = %q, %v", name, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("partial exports remain: %v, %v", entries, err)
	}
}

func TestSaveTempReportsCloseFailure(t *testing.T) {
	dir := t.TempDir()
	name, err := saveTemp(dir, "hypercat-*.html", func(out io.Writer) error {
		// Force finalization to fail after the formatter has returned successfully.
		return out.(io.Closer).Close()
	})
	if name != "" || err == nil {
		t.Fatalf("SaveTemp = %q, %v", name, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("failed export remains: %v, %v", entries, err)
	}
}

func TestSaveTempCreationFailureDoesNotCallFormatter(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing")
	name, err := saveTemp(dir, "hypercat-*.html", func(io.Writer) error { t.Fatal("formatter called without a file"); return nil })
	if name != "" || err == nil {
		t.Fatalf("SaveTemp = %q, %v", name, err)
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

// The staged representation a program asks for is text/uri-list, which is CRLF
// separated file URIs rather than the paths as they were dropped.
func TestURIListIsFileURIs(t *testing.T) {
	got := uriList([]string{"/tmp/a b.txt"})
	if !strings.HasSuffix(got, "\r\n") {
		t.Errorf("uriList() = %q, want it to end in CRLF", got)
	}
	if !strings.Contains(got, "file://") || !strings.Contains(got, "a%20b.txt") {
		t.Errorf("uriList() = %q, want an escaped file URI", got)
	}
}

func TestDroppedPathsReadsTheDropRoot(t *testing.T) {
	if got := dropPaths(nil); got != nil {
		t.Errorf("Paths(nil) = %v, want nothing", got)
	}
	if got := dropPaths(fstest.MapFS{}); len(got) != 0 {
		t.Errorf("Paths(empty) = %v, want nothing", got)
	}
	dropped := fstest.MapFS{"one.txt": {}, "two.txt": {}}
	if got := dropPaths(dropped); len(got) != 2 {
		t.Errorf("Paths() = %v, want both entries", got)
	}
}

// What is staged for a drop is the intersection of what this window can make
// with what the program registered for: a type it did not ask for is noise, and
// one it asked for that this window cannot make is a promise it cannot keep.
func TestOfferedRepresentationsIntersect(t *testing.T) {
	both := offeredRepresentations([]string{"text/plain", "text/uri-list"})
	if len(both) != 2 || both[0].mime != "text/uri-list" {
		t.Errorf("offered %v, want both in this window's order", mimesOf(both))
	}
	if one := offeredRepresentations([]string{"text/plain"}); len(one) != 1 || one[0].mime != "text/plain" {
		t.Errorf("offered %v, want only text/plain", mimesOf(one))
	}
	if none := offeredRepresentations([]string{"image/png"}); len(none) != 0 {
		t.Errorf("offered %v for a type this window cannot make", mimesOf(none))
	}
	// The bytes come from the same table that named the type, so a drop cannot
	// advertise one thing and stage another.
	for _, representation := range both {
		if len(representation.body([]string{"/tmp/a"})) == 0 {
			t.Errorf("%s staged nothing", representation.mime)
		}
	}
}

func mimesOf(representations []representation) []string {
	names := make([]string, 0, len(representations))
	for _, r := range representations {
		names = append(names, r.mime)
	}
	return names
}
