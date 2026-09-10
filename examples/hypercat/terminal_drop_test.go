package main

import (
	"strings"
	"testing"
	"testing/fstest"
)

// A drop with no program listening is the old behaviour: the paths are typed
// at the prompt, quoted so a shell reads them as filenames.
func TestShellQuoteMakesADropTypeable(t *testing.T) {
	got := shellQuote([]string{"/tmp/plain.txt", "/tmp/two words.txt", "/tmp/it's.txt"})
	want := `'/tmp/plain.txt' '/tmp/two words.txt' '/tmp/it'\''s.txt' `
	if got != want {
		t.Errorf("shellQuote() = %q, want %q", got, want)
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
	if got := droppedPaths(nil); got != nil {
		t.Errorf("droppedPaths(nil) = %v, want nothing", got)
	}
	if got := droppedPaths(fstest.MapFS{}); len(got) != 0 {
		t.Errorf("droppedPaths(empty) = %v, want nothing", got)
	}
	dropped := fstest.MapFS{"one.txt": {}, "two.txt": {}}
	if got := droppedPaths(dropped); len(got) != 2 {
		t.Errorf("droppedPaths() = %v, want both entries", got)
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

func mimesOf(representations []dropRepresentation) []string {
	names := make([]string, 0, len(representations))
	for _, r := range representations {
		names = append(names, r.mime)
	}
	return names
}

// A program that registered with OSC 72 takes the drop itself: the terminal
// reports it and answers the program's request for the data, rather than the
// paths being typed at a prompt that is not there.
func TestDropGoesToTheProgramThatRegistered(t *testing.T) {
	win := newTabTestApp(t)
	tab := win.current()

	if active, err := tab.stream.DragActive(); err != nil || active {
		t.Fatalf("DragActive() before any program registered = %v, %v; want false", active, err)
	}
	feedTab(t, tab, "\x1b]72;t=a;text/uri-list text/plain\x1b\\")
	active, err := tab.stream.DragActive()
	if err != nil || !active {
		t.Fatalf("DragActive() after registration = %v, %v; want true", active, err)
	}
	mimes, err := tab.stream.DragRegisteredMimes()
	if err != nil {
		t.Fatalf("DragRegisteredMimes: %v", err)
	}
	if !strings.Contains(mimes, "text/uri-list") {
		t.Errorf("DragRegisteredMimes() = %q, want the registered types", mimes)
	}
}
