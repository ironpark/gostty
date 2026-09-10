// Package shell runs the user's shell on a pseudo-terminal and reads what it
// writes.
//
// It is the half of a terminal emulator that has nothing to do with terminal
// emulation: opening a pty, starting a process on it, sizing it, and getting
// the bytes off it without blocking the frame. The platform differences all
// live here -- a POSIX pty and a Windows pseudoconsole differ in who owns the
// far end, and in what ends a read once the program has gone -- so nothing
// above this package has to know which one it is talking to.
package shell

import (
	"io"
	"os"
	"sync"
)

// Device is what a session needs of a pty: the two ends of the conversation,
// and the size the program on the other side reads. Narrow because it is also
// what the tests stand in for, and because nothing outside `StartCommand`
// should care which platform's pseudo-terminal is underneath.
type Device interface {
	io.ReadWriteCloser
	Resize(cols, rows int) error
}

// Process is the program on the far end of one, as much of it as a session
// needs: it waits for the program to finish, and ends it when the window
// closes first.
type Process interface {
	Wait() error
	Kill() error
}

// Session owns the pty, the child process, and the output reader. Closing it
// releases the reader even when the caller has stopped consuming output.
type Session struct {
	// Pty is written to, to type at the program, and resized when the window
	// is. Reading it is the session's own job; the bytes come out of Output.
	Pty Device
	// Output carries the chunks read off the pty, in order. It is closed when
	// the program's output ends, which is how a caller learns the shell is
	// gone.
	Output chan []byte
	// Reaped is closed once the child has been waited for.
	Reaped chan struct{}

	command    string // executable used to choose dropped-path quoting
	cmd        Process
	stop       chan struct{}
	readerDone chan struct{}
	once       sync.Once
}

// Start runs the user's shell.
func Start(cols, rows int) (*Session, error) {
	return StartCommand(cols, rows, defaultShell())
}

// StartCommand opens a pty, sizes it, and starts `argv` on it. The pty makes
// the process rather than being handed one: Windows cannot attach an
// already-built `exec.Cmd` to a pseudoconsole.
func StartCommand(cols, rows int, argv []string) (*Session, error) {
	device, cmd, err := startOnPty(cols, rows, argv, append(os.Environ(), "TERM=xterm-256color"))
	if err != nil {
		return nil, err
	}
	s := &Session{
		Pty: device, cmd: cmd, command: argv[0],
		Output:     make(chan []byte, 64),
		Reaped:     make(chan struct{}),
		stop:       make(chan struct{}),
		readerDone: make(chan struct{}),
	}
	go s.read()
	go func() {
		// Reap the child even if the caller has not consumed its last output.
		_ = cmd.Wait()
		endReadsAfterExit(device)
		close(s.Reaped)
	}()
	return s, nil
}

func (s *Session) read() {
	defer close(s.readerDone)
	defer close(s.Output)
	buf := make([]byte, 1<<16)
	for {
		n, err := s.Pty.Read(buf)
		if n > 0 {
			select {
			case s.Output <- append([]byte(nil), buf[:n]...):
			case <-s.stop:
				return
			}
		}
		if err != nil {
			return
		}
	}
}

// Close ends the program and the reader. It is safe on a nil session and on
// one that has already been closed, which is what lets a tab that failed
// half-way through starting up be torn down the same way as any other.
func (s *Session) Close() {
	if s == nil {
		return
	}
	s.once.Do(func() {
		close(s.stop)
		_ = s.Pty.Close()
		// Closing the window also ends a shell that ignores the PTY hangup.
		_ = s.cmd.Kill()
		<-s.readerDone
		<-s.Reaped
	})
}
