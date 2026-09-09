package main

import (
	"io"
	"os"
	"sync"
)

// What a session needs of a pty: the two ends of the conversation, and the size
// the program on the other side reads. Narrow because it is also what the tests
// stand in for, and because nothing outside `startShellCommand` should care
// which platform's pseudo-terminal is underneath.
type terminalDevice interface {
	io.ReadWriteCloser
	Resize(cols, rows int) error
}

// The program on the far end of one, as much of it as a session needs: it waits
// for the program to finish, and ends it when the window closes first.
type shellProcess interface {
	Wait() error
	Kill() error
}

// shellSession owns the PTY, child process, and output reader. Closing it
// releases the reader even when Update has stopped consuming output.
type shellSession struct {
	pty         terminalDevice
	cmd         shellProcess
	output      chan []byte
	stop        chan struct{}
	readerDone  chan struct{}
	processDone chan struct{}
	once        sync.Once
}

func startShell(cols, rows int) (*shellSession, error) {
	return startShellCommand(cols, rows, defaultShell())
}

// startShellCommand opens a pty, sizes it, and starts `argv` on it. The pty
// makes the process rather than being handed one: Windows cannot attach an
// already-built `exec.Cmd` to a pseudoconsole.
func startShellCommand(cols, rows int, argv []string) (*shellSession, error) {
	device, cmd, err := startOnPty(cols, rows, argv, append(os.Environ(), "TERM=xterm-256color"))
	if err != nil {
		return nil, err
	}
	s := &shellSession{
		pty: device, cmd: cmd,
		output:      make(chan []byte, 64),
		stop:        make(chan struct{}),
		readerDone:  make(chan struct{}),
		processDone: make(chan struct{}),
	}
	go s.readOutput()
	go func() {
		// Reap the child even if the window has not consumed its last output.
		_ = cmd.Wait()
		endReadsAfterExit(device)
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
		_ = s.cmd.Kill()
		<-s.readerDone
		<-s.processDone
	})
}
