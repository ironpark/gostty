package raw

import "testing"

// The raw layer is cgo plus argument marshalling, with none of the public
// package's locking, handle checks or error construction.
func BenchmarkRawCodepointWidth(b *testing.B) {
	for b.Loop() {
		_, _ = UnicodeCodepointWidth('A')
	}
}
