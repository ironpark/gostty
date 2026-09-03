package main

import "testing"

func TestRepeating(t *testing.T) {
	// First press fires immediately.
	if !repeating(1) {
		t.Errorf("repeating(1) = false, want true")
	}

	// Between 2 and repeatDelayTicks, it must not repeat.
	for held := 2; held <= repeatDelayTicks; held++ {
		if repeating(held) {
			t.Errorf("repeating(%d) = true, want false during initial delay", held)
		}
	}

	// After delay, it fires every repeatIntervalTicks.
	if !repeating(repeatDelayTicks + repeatIntervalTicks) {
		t.Errorf("repeating(%d) = false, want true", repeatDelayTicks+repeatIntervalTicks)
	}
	if repeating(repeatDelayTicks + 1) {
		t.Errorf("repeating(%d) = true, want false", repeatDelayTicks+1)
	}
}

func TestCurrentMods(t *testing.T) {
	// Must not panic and should return valid flags.
	m := currentMods()
	t.Logf("currentMods: %+v", m)
}
