package gostty

import (
	"encoding/base64"
	"fmt"
	"testing"
)

// imagePlacements updates state from term and reads out the image placements.
func imagePlacements(t *testing.T, state *RenderState, term *Terminal) []RenderImagePlacement {
	t.Helper()
	if err := state.Update(term); err != nil {
		t.Fatalf("RenderState.Update: %v", err)
	}
	dst := make([]RenderImagePlacement, state.ImagePlacementCount())
	n, err := state.ImagePlacements(dst)
	if err != nil {
		t.Fatalf("ImagePlacements: %v", err)
	}
	return dst[:n]
}

func newRenderStateFor(t *testing.T) *RenderState {
	t.Helper()
	state, err := NewRenderState()
	if err != nil {
		t.Fatalf("NewRenderState: %v", err)
	}
	t.Cleanup(func() { state.Close() })
	return state
}

// A placement at the cursor comes out of the render state at its cell, sized
// through the cell size, and the list generation only moves when it changes.
func TestRenderImageFixedPlacement(t *testing.T) {
	term, stream := newStreamPair(t, 20, 5)
	if err := term.ResizeCells(20, 5, 10, 20); err != nil {
		t.Fatalf("ResizeCells: %v", err)
	}
	state := newRenderStateFor(t)

	transmit(t, stream, 1, 3, 2)
	got := imagePlacements(t, state, term)
	if len(got) != 1 {
		t.Fatalf("placements = %d, want 1", len(got))
	}
	p := got[0]
	if p.Kind != RenderImageKindFixed || p.ImageID != 1 || p.X != 0 || p.Y != 0 {
		t.Errorf("placement = %+v, want fixed image 1 at (0,0)", p)
	}
	if p.Cols != 3 || p.Rows != 2 || p.Width != 30 || p.Height != 40 {
		t.Errorf("size = %dx%d cells, %dx%d px; want 3x2, 30x40", p.Cols, p.Rows, p.Width, p.Height)
	}
	if p.SourceWidth != 2 || p.SourceHeight != 2 || p.Layer != KittyLayerAboveText {
		t.Errorf("source %dx%d layer %v; want 2x2 above_text", p.SourceWidth, p.SourceHeight, p.Layer)
	}
	if p.ImageGeneration == 0 {
		t.Error("ImageGeneration = 0, want the image's generation")
	}

	generation := state.ImagePlacementsGeneration()
	feed(t, stream, "text")
	imagePlacements(t, state, term)
	if again := state.ImagePlacementsGeneration(); again != generation {
		t.Errorf("generation moved on a text-only change: %d -> %d", generation, again)
	}
	transmit(t, stream, 2, 1, 1)
	if got := imagePlacements(t, state, term); len(got) != 2 {
		t.Fatalf("placements after a second image = %d, want 2", len(got))
	}
	if again := state.ImagePlacementsGeneration(); again == generation {
		t.Error("generation did not move when an image was added")
	}
}

// A virtual placement is drawn where its unicode placeholder cells are, which
// only the render state can work out: one placement per run of cells.
func TestRenderImageVirtualPlacement(t *testing.T) {
	term, stream := newStreamPair(t, 20, 5)
	if err := term.ResizeCells(20, 5, 10, 20); err != nil {
		t.Fatalf("ResizeCells: %v", err)
	}
	state := newRenderStateFor(t)

	pixels := base64.StdEncoding.EncodeToString(make([]byte, 2*2*3))
	feed(t, stream, fmt.Sprintf("\x1b_Ga=T,U=1,f=24,s=2,v=2,t=d,i=9,c=4,r=2,q=2;%s\x1b\\", pixels))
	// The foreground color names the image; the diacritics name row 0 and
	// column 0, and the cells after the first continue the run.
	feed(t, stream, "\r\n\x1b[2C\x1b[38;5;9m\U0010EEEE̅̅\U0010EEEE\U0010EEEE\U0010EEEE\x1b[m")

	got := imagePlacements(t, state, term)
	if len(got) != 1 {
		t.Fatalf("placements = %+v, want one run", got)
	}
	p := got[0]
	if p.Kind != RenderImageKindVirtual || p.ImageID != 9 {
		t.Errorf("placement = %+v, want a virtual run of image 9", p)
	}
	if p.X != 2 || p.Y != 1 || p.Cols != 4 || p.Rows != 1 {
		t.Errorf("run at (%d,%d) %dx%d, want (2,1) 4x1", p.X, p.Y, p.Cols, p.Rows)
	}
	if p.Layer != KittyLayerBelowText || p.Z != -1 {
		t.Errorf("layer %v z %d, want below_text -1", p.Layer, p.Z)
	}
}

// The placements come out in drawing order: by layer, then z.
func TestRenderImagePlacementsSorted(t *testing.T) {
	term, stream := newStreamPair(t, 20, 5)
	if err := term.ResizeCells(20, 5, 10, 20); err != nil {
		t.Fatalf("ResizeCells: %v", err)
	}
	state := newRenderStateFor(t)

	pixels := base64.StdEncoding.EncodeToString([]byte{1, 2, 3})
	for _, spec := range []struct{ id, z int }{{1, 5}, {2, -(1 << 30) - 1}, {3, -5}, {4, 0}} {
		feed(t, stream, fmt.Sprintf("\x1b_Ga=T,f=24,s=1,v=1,t=d,i=%d,z=%d,q=2;%s\x1b\\", spec.id, spec.z, pixels))
	}
	got := imagePlacements(t, state, term)
	var ids []uint32
	for _, p := range got {
		ids = append(ids, p.ImageID)
	}
	if fmt.Sprint(ids) != "[2 3 4 1]" {
		t.Errorf("order = %v, want [2 3 4 1]", ids)
	}
}

// With nothing animating there is nothing to tick; a running animation asks
// to be woken when its current frame's gap runs out.
func TestKittyAnimationTick(t *testing.T) {
	term, stream := newStreamPair(t, 20, 5)
	if _, ok := term.KittyAnimationTick(1000); ok {
		t.Fatal("tick with no animations reported a deadline")
	}

	frame := base64.StdEncoding.EncodeToString(make([]byte, 2*2*3))
	transmit(t, stream, 1, 0, 0)
	// A second frame shown for 50ms, then start the animation looping.
	feed(t, stream, fmt.Sprintf("\x1b_Ga=f,i=1,f=24,s=2,v=2,z=50,q=2;%s\x1b\\", frame))
	feed(t, stream, "\x1b_Ga=a,i=1,s=3,q=2\x1b\\")

	before, _ := term.KittyImage(1)
	next, ok := term.KittyAnimationTick(1000)
	if !ok || next <= 1000 {
		t.Fatalf("tick = %d, %v; want a deadline after 1000", next, ok)
	}
	if _, ok := term.KittyAnimationTick(next); !ok {
		t.Fatal("running animation stopped asking for ticks")
	}
	if after, _ := term.KittyImage(1); after.Generation == before.Generation {
		t.Error("image generation did not move when the frame advanced")
	}
}
