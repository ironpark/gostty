package main

import (
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/aymanbagabas/go-pty"
)

// What a session needs of a pty: the two ends of the conversation, and the size
// the program on the other side reads. Narrow because it is also what the tests
// stand in for, and because nothing outside `startShellCommand` should care
// which platform's pseudo-terminal is underneath.
type terminalDevice interface {
	io.ReadWriteCloser
	Resize(cols, rows int) error
}

// shellSession owns the PTY, child process, and output reader. Closing it
// releases the reader even when Update has stopped consuming output.
type shellSession struct {
	pty         terminalDevice
	cmd         *pty.Cmd
	output      chan []byte
	stop        chan struct{}
	readerDone  chan struct{}
	processDone chan struct{}
	once        sync.Once
}

func startShell(cols, rows int) (*shellSession, error) {
	return startShellCommand(cols, rows, defaultShell())
}

// startShellCommand opens a pty, sizes it, and starts `argv` on it. The command
// is made by the pty rather than handed to it: Windows cannot attach an
// already-built `exec.Cmd` to a pseudoconsole, so the pty owns the spawn.
func startShellCommand(cols, rows int, argv []string) (*shellSession, error) {
	ptmx, err := pty.New()
	if err != nil {
		return nil, fmt.Errorf("open pty: %w", err)
	}
	if err := ptmx.Resize(cols, rows); err != nil {
		_ = ptmx.Close()
		return nil, fmt.Errorf("size pty: %w", err)
	}
	cmd := ptmx.Command(argv[0], argv[1:]...)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	if err := cmd.Start(); err != nil {
		_ = ptmx.Close()
		return nil, fmt.Errorf("start %s: %w", argv[0], err)
	}
	s := &shellSession{
		pty: ptmx, cmd: cmd,
		output:      make(chan []byte, 64),
		stop:        make(chan struct{}),
		readerDone:  make(chan struct{}),
		processDone: make(chan struct{}),
	}
	go s.readOutput()
	go func() {
		// Reap the child even if the window has not consumed its last output.
		_ = cmd.Wait()
		close(s.processDone)
	}()
	return s, nil
}

func (s *shellSession) readOutput() {
	defer close(s.readerDone)
	defer close(s.output)
	buf := make([]byte, 1<<16)
	for {
		n, err := s.pty.Read(buf)
		if n > 0 {
			select {
			case s.output <- append([]byte(nil), buf[:n]...):
			case <-s.stop:
				return
			}
		}
		if err != nil {
			return
		}
	}
}

func (s *shellSession) close() {
	if s == nil {
		return
	}
	s.once.Do(func() {
		close(s.stop)
		_ = s.pty.Close()
		// Closing the window also ends a shell that ignores the PTY hangup.
		_ = s.cmd.Process.Kill()
		<-s.readerDone
		<-s.processDone
	})
}
