// Package shelltest runs a test binary as the shell a [shell.Session] starts.
//
// A pty test needs a child that writes something and then does as it is told,
// and the obvious child is a shell script -- until Windows, where there is no
// `/bin/sh` and cmd.exe spells none of this the same way. A test binary is a
// program both platforms already have, so [Main] hands it over to the helper
// when the mode variable is set, and [Shell] points the shell lookup at
// `os.Executable()`.
//
// It is a package rather than a test file because two packages need it: the one
// that owns the session, and the one that runs tabs on top of it.
package shelltest

import (
	"bufio"
	"fmt"
	"os"
	"testing"

	"github.com/ironpark/gostty/examples/hypercat/shell"
)

// The variable that turns a test binary into the shell the tests run.
const modeVar = "HYPERCAT_TEST_HELPER"

// Printed is what the helper writes to say it ran. Not the word "hypercat": a
// pseudoconsole announces the program it is running in a title sequence, and
// the program here is hypercat.test.exe.
const Printed = "helper-ran"

// Main runs the helper when this binary was started as a shell, and the tests
// otherwise. Every package whose tests start a session needs it, because the
// binary started as the shell is the one being tested.
func Main(m *testing.M) {
	if mode := os.Getenv(modeVar); mode != "" {
		os.Exit(helper(mode))
	}
	os.Exit(m.Run())
}

func helper(mode string) int {
	switch mode {
	case "print":
		fmt.Print(Printed)
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
		// Says the child is up before anything is written to it, so a session
		// that ends early is distinguishable from one that never started.
		fmt.Println(Printed)
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

// Shell points the shell lookup at this test binary in `mode`, and returns the
// same argv for the tests that start a session directly.
func Shell(t *testing.T, mode string) []string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(modeVar, mode)
	t.Setenv(shell.Var, exe)
	return []string{exe}
}
