//go:build !windows

package main

import (
	"os"

	"github.com/aymanbagabas/go-pty"
)

// The environment variable naming the shell on this platform.
const shellVar = "SHELL"

// The user's shell, or the one every POSIX system has.
func defaultShell() []string {
	if shell := os.Getenv(shellVar); shell != "" {
		return []string{shell}
	}
	return []string{"/bin/sh"}
}

// The child holds the far end of the pty now, and this process's copy of it is
// what would stop a read on the near end from ever ending: while a writer is
// open here, the read blocks after the shell exits rather than returning.
func detachChildEnd(p pty.Pty) {
	if unixPty, ok := p.(pty.UnixPty); ok {
		_ = unixPty.Slave().Close()
	}
}

// Nothing: letting go of the far end above is what ends the read.
func endReadsAfterExit(p pty.Pty) {}
