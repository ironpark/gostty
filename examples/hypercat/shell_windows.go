package main

import (
	"os"

	"github.com/aymanbagabas/go-pty"
)

// `SHELL` is a POSIX convention; Windows names its command processor in
// `COMSPEC`.
const shellVar = "COMSPEC"

// The command processor Windows names, or the one it has always shipped.
func defaultShell() []string {
	if shell := os.Getenv(shellVar); shell != "" {
		return []string{shell}
	}
	return []string{`C:\Windows\System32\cmd.exe`}
}

// Nothing: a pseudoconsole has no second handle to hand over.
func detachChildEnd(p pty.Pty) {}

// A pseudoconsole outlives the program running in it, so a read on its output
// pipe does not end when the shell does. Closing that pipe is what tells the
// reader the session is over; the console handle itself is left to `Close`.
func endReadsAfterExit(p pty.Pty) {
	if conPty, ok := p.(pty.ConPty); ok {
		_ = conPty.OutputPipe().Close()
	}
}
