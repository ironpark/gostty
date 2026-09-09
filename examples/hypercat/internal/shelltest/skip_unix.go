//go:build !windows

package shelltest

import "testing"

// SkipWithoutIO is a no-op here: a pty passes the shell's own bytes through.
func SkipWithoutIO(t *testing.T) {}
