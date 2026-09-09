package main

import (
	"testing"

	"github.com/ironpark/gostty"
)

func TestClosePartiallyInitializedTerminalTab(t *testing.T) {
	tab := &terminalTab{}
	tab.close()
	var err error
	tab.vt, err = gostty.NewTerminal(80, 24)
	if err != nil {
		t.Fatal(err)
	}
	tab.stream, err = tab.vt.NewStream(0)
	if err != nil {
		tab.close()
		t.Fatal(err)
	}
	tab.close()
	// Close would leave the terminal alive if its stream were still a child.
	if _, err := tab.vt.NewStream(0); err == nil {
		t.Fatal("terminal remained open after cleanup")
	}
	tab.close()
}
