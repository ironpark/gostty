package main

import (
	"path/filepath"
	"testing"

	"github.com/ironpark/gostty"
	"github.com/ironpark/gostty/examples/hypercat/internal/desktop"
	"github.com/ironpark/gostty/examples/hypercat/shell"
)

func TestClosePartiallyInitializedTerminalTab(t *testing.T) {
	tab := &terminal{}
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

// Fail after native resources have been acquired, before the shell can start.
func TestStartFailureClosesTerminalResources(t *testing.T) {
	t.Setenv(shell.Var, filepath.Join(t.TempDir(), "missing-shell"))
	tab := &terminal{
		cols: 80, rows: 24,
		settings:  testAppearance(),
		clipboard: &desktop.Clipboard{},
	}
	t.Cleanup(tab.close)
	if err := tab.start(); err == nil {
		t.Fatal("starting a missing shell succeeded")
	}
	if tab.vt == nil || tab.stream == nil || tab.state == nil || tab.images == nil {
		t.Fatal("startup failed before acquiring native resources")
	}
	if err := tab.stream.Feed([]byte("closed")); err == nil {
		t.Error("stream remained open after startup failure")
	}
	if _, err := tab.state.CellCount(); err == nil {
		t.Error("render state remained open after startup failure")
	}
	if tab.sel.gesture != nil {
		t.Error("selection gesture remained open after startup failure")
	}
	if stream, err := tab.vt.NewStream(0); err == nil {
		_ = stream.Close()
		t.Error("terminal remained open after startup failure")
	}
}
