package shell_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/ironpark/gostty/examples/hypercat/internal/shelltest"
	"github.com/ironpark/gostty/examples/hypercat/shell"
)

func TestMain(m *testing.M) { shelltest.Main(m) }

func TestShellOutputAndExit(t *testing.T) {
	shelltest.SkipWithoutIO(t)
	s, err := shell.StartCommand(80, 24, shelltest.Shell(t, "print"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	var output []byte
	timeout := time.After(5 * time.Second)
	for {
		select {
		case chunk, ok := <-s.Output:
			if !ok {
				// Contains rather than equals: a pseudoconsole is a renderer,
				// so what the shell printed arrives inside a screen it drew.
				if !bytes.Contains(output, []byte(shelltest.Printed)) {
					t.Fatalf("output = %q", output)
				}
				select {
				case <-s.Reaped:
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
	shelltest.SkipWithoutIO(t)
	s, err := shell.StartCommand(80, 24, shelltest.Shell(t, "spew"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	deadline := time.Now().Add(5 * time.Second)
	for len(s.Output) < unreadChunksWanted {
		if time.Now().After(deadline) {
			t.Fatalf("output queue holds %d of the %d wanted after 5s", len(s.Output), unreadChunksWanted)
		}
		time.Sleep(time.Millisecond)
	}
	done := make(chan struct{})
	go func() {
		s.Close()
		s.Close() // repeated shutdown is harmless
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("closing a full output queue blocked")
	}
}

func TestShellCloseWhileWaitingForInput(t *testing.T) {
	shelltest.SkipWithoutIO(t)
	s, err := shell.StartCommand(80, 24, shelltest.Shell(t, "hold"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	select {
	case <-s.Output:
	case <-time.After(5 * time.Second):
		t.Fatal("shell did not become ready")
	}
	done := make(chan struct{})
	go func() {
		s.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("closing an idle shell blocked")
	}
}

// The size a session is started at, and every size it is resized to, reaches
// the pty. The program on the far end reads its size from there, so a terminal
// that resized only its own grid would leave the program drawing to the old
// one.
func TestSessionCarriesTheSizeToThePty(t *testing.T) {
	s, err := shell.StartCommand(80, 24, shelltest.Shell(t, "hold"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	assertPtySize(t, s.Pty, 80, 24)
	if err := s.Pty.Resize(100, 30); err != nil {
		t.Fatal(err)
	}
	assertPtySize(t, s.Pty, 100, 30)
}
