package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Not a test: a probe, because a pseudoconsole can only be watched from a
// Windows runner. The last run said the console painted an empty screen, set
// its title to the child's path, and then said nothing more -- so the question
// is whether the child ran at all and, if it did, where its output went.
// Always fails, because that is how a passing test's log gets printed.
func TestConPTYProbe(t *testing.T) {
	trace := filepath.Join(t.TempDir(), "helper-trace.txt")
	t.Setenv(helperTraceVar, trace)

	s, err := startShellCommand(80, 24, helperShell(t, "print"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.close)

	var chunks [][]byte
	deadline := time.After(8 * time.Second)
collect:
	for {
		select {
		case chunk, ok := <-s.output:
			if !ok {
				break collect
			}
			chunks = append(chunks, chunk)
		case <-deadline:
			t.Log("output channel never closed")
			break collect
		}
	}

	traced, readErr := os.ReadFile(trace)
	t.Logf("child ran: %v (%q, err=%v)", readErr == nil, string(traced), readErr)
	t.Logf("chunks: %d", len(chunks))
	for i, chunk := range chunks {
		t.Logf("  [%d] %q", i, chunk)
	}
	var all []byte
	for _, chunk := range chunks {
		all = append(all, chunk...)
	}
	t.Logf("marker in output: %v", strings.Contains(string(all), helperPrinted))

	select {
	case <-s.processDone:
		t.Log("process was reaped")
	case <-time.After(2 * time.Second):
		t.Log("process was never reaped")
	}
	t.Fail()
}
