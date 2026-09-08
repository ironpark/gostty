package thecat

import "testing"

func TestCompanionTracksPointerAndExpiresAttention(t *testing.T) {
	world := grid("          ", "          ")
	companion, err := NewCompanion(world)
	if err != nil {
		t.Fatal(err)
	}
	companion.Update(world, 25, 10, 1.0/60)
	if x, y, ok := companion.Attention(); !ok || x != 25 || y != 10 {
		t.Fatalf("attention = (%v, %v, %v)", x, y, ok)
	}
	for range attentionTicks {
		companion.Update(world, 25, 10, 1.0/60)
	}
	if _, _, ok := companion.Attention(); ok {
		t.Fatal("stationary pointer retained attention")
	}

	world = GridWorld{Cols: 20, Rows: 8, CellWidth: 10, CellHeight: 20}
	companion.Update(world, 50, 30, 1.0/60)
	if w, h := companion.Bounds(); w != 200 || h != 160 {
		t.Fatalf("resized world bounds = %vx%v", w, h)
	}
	if _, _, ok := companion.Attention(); !ok {
		t.Fatal("moving pointer did not restore attention")
	}
}

func TestDisabledCompanionDoesNotMoveOrConsumeClicks(t *testing.T) {
	world := grid("          ", "          ")
	companion, err := NewCompanion(world)
	if err != nil {
		t.Fatal(err)
	}
	companion.SetEnabled(false)
	x, y := companion.Feet()
	companion.Update(world, 30, 10, 1.0/60)
	if gotX, gotY := companion.Feet(); gotX != x || gotY != y {
		t.Fatal("disabled cat moved")
	}
	if companion.PokeAt(int(x), int(y)) {
		t.Fatal("disabled cat consumed a click")
	}
	companion.SetEnabled(true)
	if !companion.Enabled() {
		t.Fatal("cat did not re-enable")
	}
}

func TestMissingCompanionIsHarmless(t *testing.T) {
	var companion *Companion
	companion.Update(GridWorld{}, 0, 0, 1.0/60)
	companion.ClearHover()
	if companion.Enabled() || companion.PokeAt(0, 0) {
		t.Fatal("missing cat was interactive")
	}
}
