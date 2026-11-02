package raw

import "testing"

// The raw layer is the purego call plus argument marshalling, with none of the public
// package's locking, handle checks or error construction.
func BenchmarkRawCodepointWidth(b *testing.B) {
	loadForTest(b)
	for b.Loop() {
		_, _ = UnicodeCodepointWidth('A')
	}
}
