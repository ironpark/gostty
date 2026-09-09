//go:build !windows

package main

import (
	"fmt"
	"os"

	"github.com/aymanbagabas/go-pty"
)

// startOnPty opens a pty, sizes it, and starts `argv` on it.
func startOnPty(cols, rows int, argv, env []string) (terminalDevice, shellProcess, error) {
	ptmx, err := pty.New()
	if err != nil {
		return nil, nil, fmt.Errorf("open pty: %w", err)
	}
	if err := ptmx.Resize(cols, rows); err != nil {
		_ = ptmx.Close()
		return nil, nil, fmt.Errorf("size pty: %w", err)
	}
	cmd := ptmx.Command(argv[0], argv[1:]...)
	cmd.Env = env
	if err := cmd.Start(); err != nil {
		_ = ptmx.Close()
		return nil, nil, fmt.Errorf("start %s: %w", argv[0], err)
	}
	// The child holds the far end now, and this process's copy of it is what
	// would stop a read on the near end from ever ending: while a writer is
	// open here, the read blocks after the shell exits rather than returning.
	// creack/pty let go of it; go-pty keeps both.
	if unixPty, ok := ptmx.(pty.UnixPty); ok {
		_ = unixPty.Slave().Close()
	}
	return ptmx, &unixProcess{cmd}, nil
}

// Letting go of the far end above is what ends the read; there is nothing to do
// once the program finishes.
func endReadsAfterExit(terminalDevice) {}

type unixProcess struct{ cmd *pty.Cmd }

func (p *unixProcess) Wait() error { return p.cmd.Wait() }

func (p *unixProcess) Kill() error {
	if p.cmd.Process == nil {
		return nil
	}
	return p.cmd.Process.Kill()
}

var _ = os.Environ
