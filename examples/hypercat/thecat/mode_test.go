package thecat

import "testing"

func TestModeCycles(t *testing.T) {
	want := []Mode{ModeOn, ModeHyper, ModeOff, ModeOn}
	mode := ModeOn
	for i, next := range want[1:] {
		if mode = mode.Step(1); mode != next {
			t.Fatalf("step %d = %v, want %v", i, mode, next)
		}
	}
	if ModeOn.Step(-1) != ModeOff {
		t.Fatal("stepping back from the first mode did not wrap")
	}
	if Mode(0) != ModeOn {
		t.Fatal("the zero mode must be the default, with the cat on")
	}
}

func TestCompanionModeRoundTrips(t *testing.T) {
	c := &Companion{Cat: &Cat{}, enabled: true}
	for _, m := range []Mode{ModeOff, ModeHyper, ModeOn} {
		c.SetMode(m)
		if c.Mode() != m {
			t.Fatalf("SetMode(%v) reads back %v", m, c.Mode())
		}
	}
}
