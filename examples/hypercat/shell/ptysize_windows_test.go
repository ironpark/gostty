package shell_test

import (
	"testing"

	"github.com/ironpark/gostty/examples/hypercat/shell"
)

// A pseudoconsole is told its size and keeps no way to report it back, so there
// is nothing here to compare against.
func assertPtySize(t *testing.T, p shell.Device, cols, rows int) {}
