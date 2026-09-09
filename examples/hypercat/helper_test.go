package main

import (
	"bufio"
	"fmt"
	"os"
	"testing"
	"time"
)

// The variable that turns this test binary into the shell the tab tests run.
//
// A pty test needs a child that writes something and then does as it is told,
// and the obvious child is a shell script -- until Windows, where there is no
// `/bin/sh` and cmd.exe spells none of this the same way. The test binary is a
// program both platforms already have, so `TestMain` hands it over to `helper`
// when this is set and the tests point the shell at `os.Executable()`.
const helperVar = "HYPERCAT_TEST_HELPER"

// What the helper writes to say it ran. Not the word "hypercat": a
// pseudoconsole announces the program it is running in a title sequence, and
// the program here is hypercat.test.exe.
const helperPrinted = "helper-ran"

// Where the helper records that it ran. Set only by the probe.
const helperTraceVar = "HYPERCAT_TEST_HELPER_TRACE"

// How long the printing helper stays alive after writing.
const helperLinger = 400 * time.Millisecond

func TestMain(m *testing.M) {
	if mode := os.Getenv(helperVar); mode != "" {
		os.Exit(helper(mode))
	}
	os.Exit(m.Run())
}

func helper(mode string) int {
	// A throwaway record of having run, outside the pty: it tells a failing
	// Windows run whether the child never started or started and had its
	// output go somewhere other than the console.
	if trace := os.Getenv(helperTraceVar); trace != "" {
		if f, err := os.OpenFile(trace, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
			fmt.Fprintf(f, "%s %s\n", mode, describeStdio())
			f.Close()
		}
	}
	switch mode {
	case "print":
		fmt.Print(helperPrinted)
		// A pseudoconsole paints on its own schedule and only while a client is
		// attached, so a program that writes and exits in the same breath may
		// never be drawn. Staying a moment tells that apart from output that
		// never reached the console at all.
		time.Sleep(helperLinger)
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
		fmt.Println(helperPrinted)
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
