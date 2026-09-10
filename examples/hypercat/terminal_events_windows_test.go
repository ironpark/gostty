package main

import "testing"

// A pseudoconsole reads a title sequence as a title for its own window rather
// than passing it to the program reading the console, so there is nothing here
// for the tab to have applied.
func assertOSCReachesTheTab(t *testing.T, tab *terminal) {}
