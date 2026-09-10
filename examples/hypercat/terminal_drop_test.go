package main

import (
	"strings"
	"testing"
)

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
