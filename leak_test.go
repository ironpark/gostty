package gostty

import (
	"bytes"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

func runLeakSubprocess(t *testing.T, mode string, envLeaks string) (stdout, stderr string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestLeakSubprocessHelper$", "-test.v")
	cmd.Env = append(os.Environ(),
		"TEST_ZIGO_LEAKS_HELPER=1",
		"TEST_ZIGO_LEAKS_MODE="+mode,
		"ZIGO_DEBUG_LEAKS="+envLeaks,
	)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	if err != nil {
		t.Fatalf("subprocess failed (%v): stderr=%s, stdout=%s", err, errBuf.String(), outBuf.String())
	}
	return outBuf.String(), errBuf.String()
}

func leakTerminalHelper() {
	term, err := NewTerminal(80, 24)
	if err != nil {
		panic(err)
	}
	_ = term
}

func noLeakTerminalHelper() {
	term, err := NewTerminal(80, 24)
	if err != nil {
		panic(err)
	}
	if err := term.Close(); err != nil {
		panic(err)
	}
}

func TestLeakSubprocessHelper(t *testing.T) {
	if os.Getenv("TEST_ZIGO_LEAKS_HELPER") != "1" {
		return
	}
	mode := os.Getenv("TEST_ZIGO_LEAKS_MODE")
	switch mode {
	case "leak":
		leakTerminalHelper()
	case "noleak":
		noLeakTerminalHelper()
	default:
		t.Fatalf("unknown leak test mode: %s", mode)
	}

	for i := 0; i < 5; i++ {
		runtime.GC()
		time.Sleep(10 * time.Millisecond)
	}
}

func TestZigoDebugLeaksEnabled(t *testing.T) {
	_, stderr := runLeakSubprocess(t, "leak", "1")
	expected := "zigo: Terminal was garbage-collected without Close; created at:"
	if !strings.Contains(stderr, expected) {
		t.Fatalf("expected stderr to contain %q, got:\n%s", expected, stderr)
	}
	if !strings.Contains(stderr, "leakTerminalHelper") {
		t.Errorf("expected stack trace in stderr to reference leakTerminalHelper, got:\n%s", stderr)
	}
}

func TestZigoDebugLeaksDisabled(t *testing.T) {
	_, stderr := runLeakSubprocess(t, "leak", "")
	unexpected := "was garbage-collected without Close"
	if strings.Contains(stderr, unexpected) {
		t.Fatalf("expected stderr NOT to contain %q when ZIGO_DEBUG_LEAKS is empty, got:\n%s", unexpected, stderr)
	}
}

func TestZigoDebugLeaksNoLeakWhenClosed(t *testing.T) {
	_, stderr := runLeakSubprocess(t, "noleak", "1")
	unexpected := "was garbage-collected without Close"
	if strings.Contains(stderr, unexpected) {
		t.Fatalf("expected stderr NOT to contain %q when handle was Closed, got:\n%s", unexpected, stderr)
	}
}
