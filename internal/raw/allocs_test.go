package raw

import (
	"path/filepath"
	"runtime"
	"testing"
)

// loadForTest loads the library the public package embeds, from where the
// generator installs it, since the raw layer has no loader of its own.
func loadForTest(tb testing.TB) {
	tb.Helper()
	path := filepath.Join("..", "..", "libs", runtime.GOOS+"_"+runtime.GOARCH, DefaultLibraryName)
	if err := LoadLibrary(path); err != nil {
		tb.Fatalf("LoadLibrary: %v", err)
	}
}

// A call made through purego.SyscallN boxes nothing, and its out parameter
// stays on the caller's stack: the native side writes a pooled frame instead.
// Through RegisterFunc each of these took two to four allocations.
func TestRawCallAllocs(t *testing.T) {
	loadForTest(t)
	state, code := NewRenderState()
	if code != 0 {
		t.Fatalf("NewRenderState: code %d", code)
	}
	defer RenderStateDeinit(state)
	for _, c := range []struct {
		name string
		call func()
	}{
		{"UnicodeCodepointWidth", func() { _, _ = UnicodeCodepointWidth('A') }},
		{"RenderStateCols", func() { _, _ = RenderStateCols(state) }},
		{"RenderStateClean", func() { _ = RenderStateClean(state) }},
		{"RenderStateColors", func() { _, _ = RenderStateColors(state) }},
	} {
		c.call()
		if got := testing.AllocsPerRun(1000, c.call); got != 0 {
			t.Errorf("%s: %v allocations per call, want 0", c.name, got)
		}
	}
	if width, code := UnicodeCodepointWidth('가'); code != 0 || width != 2 {
		t.Errorf("UnicodeCodepointWidth('가') = %d, code %d; want 2, 0", width, code)
	}
}
