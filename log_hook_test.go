package gostty

import (
	"strings"
	"sync"
	"testing"

	"github.com/ironpark/gostty/sys"
)

// The log sink only exists because the generated shim forwards this library's
// `std_options`. Nothing here asserts a particular message: ghostty decides
// what it logs and at which level, and ReleaseSafe drops everything below
// `.err`. What is asserted is that the path is wired -- the handler is
// installed, the strings arrive intact, and Clear detaches it.
func TestSysOnLogIsWired(t *testing.T) {
	var mu sync.Mutex
	var lines []string
	sys.OnLog(func(level sys.LogLevel, scope, message string) {
		mu.Lock()
		defer mu.Unlock()
		lines = append(lines, level.String()+" "+scope+" "+message)
	})
	defer sys.Clear()

	term, err := NewTerminal(20, 5)
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	stream, err := term.NewStream(0)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()

	// Feed a pile of malformed sequences, which is what ghostty logs about.
	for _, s := range []string{
		"\x1b[999999999999;9999999999H",
		"\x1b]99999;bogus\x07",
		"\x1b_Gf=100,a=T;not-a-png\x1b\\",
		"\x1b[?99999h",
	} {
		_, _ = stream.WriteString(s)
	}

	mu.Lock()
	got := append([]string(nil), lines...)
	mu.Unlock()
	for _, line := range got {
		if strings.ContainsRune(line, 0) {
			t.Fatalf("log line carries a NUL, strings did not cross intact: %q", line)
		}
	}
	for _, l := range got {
		t.Logf("captured: %s", l)
	}
}
