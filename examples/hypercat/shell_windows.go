package main

import (
	"os"
	"time"

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
//
// The pause is because a pseudoconsole is a renderer, not a pipe: the program's
// last writes have gone into a screen that conhost has not necessarily painted
// yet, and closing the pipe on the exit itself takes that screen with it. There
// is no handle to wait on for the paint -- `ClosePseudoConsole` flushes, but
// go-pty closes the pipe in the same call -- so this waits by the clock, long
// enough for a paint and short enough not to be noticed.
func endReadsAfterExit(p pty.Pty) {
	if conPty, ok := p.(pty.ConPty); ok {
		time.Sleep(250 * time.Millisecond)
		_ = conPty.OutputPipe().Close()
	}
}
