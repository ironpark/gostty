package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	reference "github.com/UserExistsError/conpty"
)

// The same question asked through a pseudoconsole implementation that is not
// this one. Every value on this side of the spawn is right and the child still
// comes up without a console, so the thing left to establish is whether a
// client can attach to a pseudoconsole on this runner at all. Always fails, to
// print the log.
func TestReferenceConPTY(t *testing.T) {
	if !reference.IsConPtyAvailable() {
		t.Log("pseudoconsoles are not available here")
		t.Fail()
		return
	}
	trace := filepath.Join(t.TempDir(), "reference-trace.txt")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	console, err := reference.Start(
		exe,
		reference.ConPtyDimensions(80, 24),
		reference.ConPtyEnv(append(os.Environ(),
			helperVar+"=print",
			helperTraceVar+"="+trace)),
	)
	if err != nil {
		t.Fatalf("reference start: %v", err)
	}
	defer console.Close()

	read := make(chan []byte, 1)
	go func() {
		buf := make([]byte, 1<<16)
		var all []byte
		for {
			n, err := console.Read(buf)
			all = append(all, buf[:n]...)
			if err != nil {
				break
			}
			if strings.Contains(string(all), helperPrinted) {
				break
			}
		}
		read <- all
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	code, waitErr := console.Wait(ctx)
	t.Logf("exit=%d err=%v", code, waitErr)

	var output []byte
	select {
	case output = <-read:
	case <-time.After(3 * time.Second):
		t.Log("reader did not finish")
	}

	traced, readErr := os.ReadFile(trace)
	t.Logf("child ran: %v (%q, err=%v)", readErr == nil, string(traced), readErr)
	t.Logf("output: %q", output)
	t.Logf("marker in output: %v", strings.Contains(string(output), helperPrinted))
	t.Fail()
}
