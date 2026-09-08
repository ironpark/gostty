package main

import (
	"fmt"
	"os"
	"os/exec"
	"sync"

	"github.com/creack/pty"
)

// shellSession owns the PTY, child process, and output reader. Closing it
// releases the reader even when Update has stopped consuming output.
type shellSession struct {
	pty         *os.File
	cmd         *exec.Cmd
	output      chan []byte
	stop        chan struct{}
	readerDone  chan struct{}
	processDone chan struct{}
	once        sync.Once
}

func startShell(size *pty.Winsize) (*shellSession, error) {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	cmd := exec.Command(shell)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	return startShellCommand(cmd, size)
}

func startShellCommand(cmd *exec.Cmd, size *pty.Winsize) (*shellSession, error) {
	ptmx, err := pty.StartWithSize(cmd, size)
	if err != nil {
		return nil, fmt.Errorf("start %s: %w", cmd.Path, err)
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
