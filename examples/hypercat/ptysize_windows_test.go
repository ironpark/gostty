package main

import "testing"

// A pseudoconsole is told its size and keeps no way to report it back, so there
// is nothing here to compare the grid against.
func assertPtySize(t *testing.T, p terminalDevice, cols, rows int) {}
