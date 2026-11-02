package gostty

import (
	"fmt"
	"strings"
	"testing"
)

// A terminal's scrollback, compressed, takes less of the memory its pages
// reserve, and MemoryUsage tells how much.
func TestCompressedScrollbackTakesLessMemory(t *testing.T) {
	sess, err := New(120, 40, WithTerminalOptions(WithScrollbackMaxLines(20000)))
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	var b strings.Builder
	for i := range 10000 {
		fmt.Fprintf(&b, "\x1b[38;5;%dmline %d of the agent's output, with some words to fill it\x1b[0m\r\n", i%200, i)
	}
	if _, err := sess.WriteString(b.String()); err != nil {
		t.Fatal(err)
	}
	term := sess.Terminal()
	before := term.ActiveScreen().MemoryUsage()
	if before.Pages == 0 || before.ResidentBytes == 0 {
		t.Fatalf("no page memory: %+v", before)
	}
	result := CompressionResultPending
	for range 100000 {
		if result = term.Compress(CompressionModeIncremental); result != CompressionResultPending {
			break
		}
	}
	after := term.ActiveScreen().MemoryUsage()
	t.Logf("before %+v\nafter  %+v (%v)", before, after, result)
	if result == CompressionResultUnsupported {
		t.Skip("compression is not supported here")
	}
	if after.CompressedPages == 0 || after.ResidentBytes >= before.ResidentBytes {
		t.Fatalf("compressed %d pages, resident %d bytes from %d", after.CompressedPages, after.ResidentBytes, before.ResidentBytes)
	}
}
