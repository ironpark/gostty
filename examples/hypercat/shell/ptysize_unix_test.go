//go:build !windows

package shell_test

import (
	"testing"

	"github.com/aymanbagabas/go-pty"
	"github.com/ironpark/gostty/examples/hypercat/shell"
	"golang.org/x/sys/unix"
)

// The size the pty is carrying, which is what a program that has not asked the
// terminal reads. Only POSIX can be asked: a pseudoconsole is told its size and
// keeps no way to report it back.
func assertPtySize(t *testing.T, p shell.Device, cols, rows int) {
	t.Helper()
	unixPty, ok := p.(pty.UnixPty)
	if !ok {
		t.Fatalf("pty is %T, not a UnixPty", p)
	}
	var size *unix.Winsize
	if err := unixPty.Control(func(fd uintptr) {
		size, _ = unix.IoctlGetWinsize(int(fd), unix.TIOCGWINSZ)
	}); err != nil {
		t.Fatal(err)
	}
	if size == nil {
		t.Fatal("TIOCGWINSZ failed")
	}
	if int(size.Col) != cols || int(size.Row) != rows {
		t.Fatalf("pty is carrying %dx%d, want %dx%d", size.Col, size.Row, cols, rows)
	}
}
