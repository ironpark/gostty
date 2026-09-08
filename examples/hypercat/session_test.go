package main

import (
	"bytes"
	"os/exec"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/ironpark/gostty"
)

func TestShellOutputAndExit(t *testing.T) {
	s, err := startShellCommand(exec.Command("/bin/sh", "-c", "printf hypercat"), &pty.Winsize{Cols: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.close)
	var output []byte
	timeout := time.After(5 * time.Second)
	for {
		select {
		case chunk, ok := <-s.output:
			if !ok {
				if !bytes.Equal(output, []byte("hypercat")) {
					t.Fatalf("output = %q", output)
				}
				select {
				case <-s.processDone:
				case <-timeout:
					t.Fatal("child was not reaped")
				}
				return
			}
			output = append(output, chunk...)
		case <-timeout:
			t.Fatal("output did not close after shell exit")
		}
	}
}

func TestShellCloseWithUnreadOutput(t *testing.T) {
	s, err := startShellCommand(exec.Command("/bin/sh", "-c", "while :; do printf 'hypercat output\\n'; done"), &pty.Winsize{Cols: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.close)
	deadline := time.Now().Add(5 * time.Second)
	for len(s.output) != cap(s.output) {
		if time.Now().After(deadline) {
			t.Fatal("output queue did not fill")
		}
		time.Sleep(time.Millisecond)
	}
	done := make(chan struct{})
	go func() {
		s.close()
		s.close() // repeated shutdown is harmless
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("closing a full output queue blocked")
	}
}

func TestShellCloseWhileWaitingForInput(t *testing.T) {
	s, err := startShellCommand(exec.Command("/bin/sh", "-c", "trap '' HUP; printf ready; read line"), &pty.Winsize{Cols: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.close)
	select {
	case <-s.output:
	case <-time.After(5 * time.Second):
		t.Fatal("shell did not become ready")
	}
	done := make(chan struct{})
	go func() {
		s.close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("closing an idle shell blocked")
	}
}

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
