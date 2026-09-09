package main

import (
	"bufio"
	"fmt"
	"os"
	"testing"
)

// The variable that turns this test binary into the shell the tab tests run.
//
// A pty test needs a child that writes something and then does as it is told,
// and the obvious child is a shell script -- until Windows, where there is no
// `/bin/sh` and cmd.exe spells none of this the same way. The test binary is a
// program both platforms already have, so `TestMain` hands it over to `helper`
// when this is set and the tests point the shell at `os.Executable()`.
const helperVar = "HYPERCAT_TEST_HELPER"

func TestMain(m *testing.M) {
	if mode := os.Getenv(helperVar); mode != "" {
		os.Exit(helper(mode))
	}
	os.Exit(m.Run())
}

func helper(mode string) int {
	switch mode {
	case "print":
		fmt.Print("hypercat")
	case "spew":
		for {
			if _, err := fmt.Println("hypercat output"); err != nil {
				return 1
			}
		}
	case "hold":
		// The point of this one is a child that outlives the pty hangup, so the
		// test can close a session that is still waiting on input.
		ignoreHangup()
		fmt.Print("ready")
		bufio.NewReader(os.Stdin).ReadString('\n')
	case "echo":
		// A stand-in for an interactive shell: whatever the test writes comes
		// back out of the terminal, so a test that wants the emulator to see an
		// escape sequence can just write one.
		in := bufio.NewReader(os.Stdin)
		for {
			line, err := in.ReadString('\n')
			if line == "exit\n" || line == "exit\r\n" {
				return 0
			}
			if _, werr := os.Stdout.WriteString(line); werr != nil {
				return 1
			}
			if err != nil {
				return 0
			}
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown helper mode %q\n", mode)
		return 2
	}
	return 0
}

// helperShell points the tab's shell lookup at this test binary in `mode`, and
// returns the same argv for the tests that start a session directly.
func helperShell(t *testing.T, mode string) []string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(helperVar, mode)
	t.Setenv(shellVar, exe)
	return []string{exe}
}
