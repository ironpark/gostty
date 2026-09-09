//go:build !windows

package main

import (
	"testing"
	"time"
)

// An OSC the shell writes reaches the tab. Separated from the output test it
// used to be part of because it is only true on a pty that passes bytes
// through: a Windows pseudoconsole is a console host, and reads a title
// sequence as a title for itself rather than passing it on.
func assertOSCReachesTheTab(t *testing.T, tab *terminalTab) {
	t.Helper()
	if _, err := tab.shell.pty.Write([]byte("\033]2;background-titled\007\n")); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for tab.title != "background-titled" {
		if _, err := tab.readOutput(); err != nil {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("OSC title was not applied; title is %q", tab.title)
		}
		time.Sleep(time.Millisecond)
	}
}
