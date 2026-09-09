package main

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

var (
	kernel32             = windows.NewLazySystemDLL("kernel32.dll")
	procGetConsoleWindow = kernel32.NewProc("GetConsoleWindow")
)

// What the child was actually given. `GetConsoleMode` failing only says the
// handle is not a console -- a pipe answers the same way -- so this asks the
// two questions that tell the cases apart: whether the process has a console
// at all, and whether writing straight to that console reaches the screen.
func describeStdio() string {
	console, _, _ := procGetConsoleWindow.Call()

	conout := "CONOUT$ open failed"
	if f, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0); err == nil {
		if _, err := f.WriteString(conoutMarker); err == nil {
			conout = "CONOUT$ written"
		} else {
			conout = fmt.Sprintf("CONOUT$ write failed: %v", err)
		}
		f.Close()
	} else {
		conout = fmt.Sprintf("CONOUT$ open failed: %v", err)
	}

	describe := func(name string, fd uintptr) string {
		var mode uint32
		err := windows.GetConsoleMode(windows.Handle(fd), &mode)
		return fmt.Sprintf("%s=%d console=%v err=%v", name, fd, err == nil, err)
	}
	return fmt.Sprintf("hasConsole=%v %s %s %s",
		console != 0, conout, describe("stdout", os.Stdout.Fd()), describe("stdin", os.Stdin.Fd()))
}

// Written straight to the console rather than through stdout.
const conoutMarker = "via-conout"
