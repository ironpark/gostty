package main

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// Whether the handles the child was given are the pseudoconsole's. A console
// handle answers `GetConsoleMode`; a pipe, or a handle that is not there at
// all, does not.
func describeStdio() string {
	describe := func(name string, fd uintptr) string {
		var mode uint32
		err := windows.GetConsoleMode(windows.Handle(fd), &mode)
		return fmt.Sprintf("%s=%d console=%v mode=%#x err=%v", name, fd, err == nil, mode, err)
	}
	return describe("stdout", os.Stdout.Fd()) + " " + describe("stdin", os.Stdin.Fd())
}
