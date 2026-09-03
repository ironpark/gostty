package gostty

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"testing"
)

// A 2x2 RGB image, transmitted directly and placed at the cursor. `a=T` is
// transmit-and-display, `f=24` is RGB, `s`/`v` are the dimensions and `i` is
// the id the placement and every later query use.
func transmit(t *testing.T, s *Stream, id int, cols, rows int) {
	t.Helper()
	pixels := []byte{
		0xff, 0x00, 0x00, 0x00, 0xff, 0x00,
		0x00, 0x00, 0xff, 0xff, 0xff, 0xff,
	}
	data := base64.StdEncoding.EncodeToString(pixels)
	geometry := ""
	if cols > 0 || rows > 0 {
		geometry = fmt.Sprintf(",c=%d,r=%d", cols, rows)
	}
	feed(t, s, fmt.Sprintf("\x1b_Ga=T,f=24,s=2,v=2,t=d,i=%d%s;%s\x1b\\", id, geometry, data))
}

func kittyImages(t *testing.T) *KittyImages {
	t.Helper()
	images, err := NewKittyImages()
	if err != nil {
		t.Fatalf("NewKittyImages: %v", err)
	}
	t.Cleanup(func() { images.Close() })
	return images
}

// placements refreshes the snapshot and reads it out.
func placements(t *testing.T, images *KittyImages, term *Terminal) []KittyPlacement {
	t.Helper()
	if err := images.Update(term); err != nil {
		t.Fatalf("KittyImages.Update: %v", err)
	}
	n, err := images.PlacementCount()
	if err != nil {
		t.Fatalf("PlacementCount: %v", err)
	}
	dst := make([]KittyPlacement, n)
	if _, err := images.Placements(dst); err != nil {
		t.Fatalf("Placements: %v", err)
	}
	return dst
}

func TestKittyNoImages(t *testing.T) {
	term, _ := newStreamPair(t, 20, 5)
	images := kittyImages(t)
	if got := placements(t, images, term); len(got) != 0 {
		t.Errorf("placements on a fresh terminal = %d, want 0", len(got))
	}
	if gen, err := images.Generation(); err != nil {
		t.Fatalf("Generation: %v", err)
	} else if gen != 0 {
		t.Errorf("generation on a fresh terminal = %d, want 0", gen)
	}
}

func TestKittyTransmitAndPlace(t *testing.T) {
	term, stream := newStreamPair(t, 20, 5)
	images := kittyImages(t)

	transmit(t, stream, 1, 0, 0)

	got := placements(t, images, term)
	if len(got) != 1 {
		t.Fatalf("placements = %d, want 1", len(got))
	}
	p := got[0]
	if p.ImageID != 1 {
		t.Errorf("image id = %d, want 1", p.ImageID)
	}
	// Placed at the cursor, which has not moved.
	if p.ViewportCol != 0 || p.ViewportRow != 0 {
		t.Errorf("viewport pos = (%d,%d), want (0,0)", p.ViewportCol, p.ViewportRow)
	}
	// No c=/r= and no cell size, so the placement is the image's own size and
	// the whole of it is the source.
	if p.PixelWidth != 2 || p.PixelHeight != 2 {
		t.Errorf("pixel size = %dx%d, want 2x2", p.PixelWidth, p.PixelHeight)
	}
	if p.SourceWidth != 2 || p.SourceHeight != 2 {
		t.Errorf("source size = %dx%d, want 2x2", p.SourceWidth, p.SourceHeight)
	}

	gen, err := images.Generation()
	if err != nil {
		t.Fatalf("Generation: %v", err)
	}
	if gen == 0 {
		t.Error("generation is still 0 after a transmit")
	}

	image, ok, err := term.KittyImage(p.ImageID)
	if err != nil {
		t.Fatalf("KittyImage: %v", err)
	}
	if !ok {
		t.Fatal("KittyImage: no image for a placed id")
	}
	if image.Width != 2 || image.Height != 2 {
		t.Errorf("image size = %dx%d, want 2x2", image.Width, image.Height)
	}
	if KittyFormat(image.Format) != KittyFormatRgb {
		t.Errorf("format = %v, want rgb", KittyFormat(image.Format))
	}
	if KittyCompression(image.Compression) != KittyCompressionNone {
		t.Errorf("compression = %v, want none", KittyCompression(image.Compression))
	}
	if image.DataLen != 12 {
		t.Errorf("data len = %d, want 12 (2*2*3)", image.DataLen)
	}

	data := make([]byte, image.DataLen)
	n, err := term.KittyImageData(p.ImageID, data)
	if err != nil {
		t.Fatalf("KittyImageData: %v", err)
	}
	if n != uint(image.DataLen) {
		t.Fatalf("KittyImageData wrote %d, want %d", n, image.DataLen)
	}
	if data[0] != 0xff || data[1] != 0x00 || data[2] != 0x00 {
		t.Errorf("first pixel = %v, want red", data[:3])
	}
}

func TestKittyMissingImage(t *testing.T) {
	term, _ := newStreamPair(t, 20, 5)
	if _, ok, err := term.KittyImage(99); err != nil {
		t.Fatalf("KittyImage: %v", err)
	} else if ok {
		t.Error("KittyImage reported an image that was never transmitted")
	}
	n, err := term.KittyImageData(99, make([]byte, 16))
	if err != nil {
		t.Fatalf("KittyImageData: %v", err)
	}
	if n != 0 {
		t.Errorf("KittyImageData for a missing image wrote %d bytes, want 0", n)
	}
}

// A placement sized in cells is measured in pixels through the cell size, which
// the terminal only knows once it has been resized with one.
func TestKittyCellSizedPlacement(t *testing.T) {
	term, stream := newStreamPair(t, 20, 5)
	images := kittyImages(t)
	if err := term.ResizeCells(20, 5, 10, 20); err != nil {
		t.Fatalf("ResizeCells: %v", err)
	}

	transmit(t, stream, 1, 3, 2)

	got := placements(t, images, term)
	if len(got) != 1 {
		t.Fatalf("placements = %d, want 1", len(got))
	}
	p := got[0]
	if p.GridCols != 3 || p.GridRows != 2 {
		t.Errorf("grid size = %dx%d, want 3x2", p.GridCols, p.GridRows)
	}
	if p.PixelWidth != 30 || p.PixelHeight != 40 {
		t.Errorf("pixel size = %dx%d, want 30x40 (3*10 x 2*20)", p.PixelWidth, p.PixelHeight)
	}
}

// Placements are handed over back to front, so drawing the array in order is
// correct without the caller sorting anything.
func TestKittyPlacementsSortedByZ(t *testing.T) {
	term, stream := newStreamPair(t, 20, 5)
	images := kittyImages(t)

	pixels := base64.StdEncoding.EncodeToString([]byte{1, 2, 3})
	for _, spec := range []struct{ id, z int }{{1, 5}, {2, -5}, {3, 0}} {
		feed(t, stream, fmt.Sprintf(
			"\x1b_Ga=T,f=24,s=1,v=1,t=d,i=%d,z=%d;%s\x1b\\",
			spec.id, spec.z, pixels))
	}

	got := placements(t, images, term)
	if len(got) != 3 {
		t.Fatalf("placements = %d, want 3", len(got))
	}
	for i := 1; i < len(got); i++ {
		if got[i-1].Z > got[i].Z {
			t.Fatalf("placements are not sorted by z: %v", []int32{got[0].Z, got[1].Z, got[2].Z})
		}
	}
}

// A placement scrolled off the top of the viewport is left out: it has nothing
// a renderer could do with it.
func TestKittyScrolledOffScreen(t *testing.T) {
	term, stream := newStreamPair(t, 20, 3)
	images := kittyImages(t)

	transmit(t, stream, 1, 0, 0)
	if len(placements(t, images, term)) != 1 {
		t.Fatal("the placement is missing before scrolling")
	}

	feed(t, stream, "\r\n\r\n\r\n\r\n\r\n\r\n")
	if got := placements(t, images, term); len(got) != 0 {
		t.Errorf("placements after scrolling away = %d, want 0", len(got))
	}
}

func TestKittyDelete(t *testing.T) {
	term, stream := newStreamPair(t, 20, 5)
	images := kittyImages(t)

	transmit(t, stream, 1, 0, 0)
	before, err := images.Generation()
	if err != nil {
		t.Fatalf("Generation: %v", err)
	}
	_ = placements(t, images, term)

	// a=d,d=I deletes the image with id i and every placement of it.
	feed(t, stream, "\x1b_Ga=d,d=I,i=1\x1b\\")

	if got := placements(t, images, term); len(got) != 0 {
		t.Errorf("placements after delete = %d, want 0", len(got))
	}
	after, err := images.Generation()
	if err != nil {
		t.Fatalf("Generation: %v", err)
	}
	if after == before {
		t.Error("the generation did not move for a delete")
	}
	if _, ok, err := term.KittyImage(1); err != nil {
		t.Fatalf("KittyImage: %v", err)
	} else if ok {
		t.Error("the image is still there after a delete")
	}
}

// A program that wants to use the protocol asks whether it is supported first,
// and waits for the answer. The terminal writes those answers itself; the
// embedder's job is to hand them back to the program.
func TestKittyQueryIsAnswered(t *testing.T) {
	_, stream := newStreamPair(t, 20, 5)

	// a=q is "would this work?", asked with a one-pixel image.
	pixels := base64.StdEncoding.EncodeToString([]byte{1, 2, 3})
	feed(t, stream, fmt.Sprintf("\x1b_Ga=q,f=24,s=1,v=1,t=d,i=31;%s\x1b\\", pixels))

	has, err := stream.HasReplies()
	if err != nil {
		t.Fatalf("HasReplies: %v", err)
	}
	if !has {
		t.Fatal("the query was not answered")
	}
	var buf bytes.Buffer
	if err := stream.WriteReplies(&buf); err != nil {
		t.Fatalf("WriteReplies: %v", err)
	}
	if want := "\x1b_Gi=31;OK\x1b\\"; buf.String() != want {
		t.Errorf("reply = %q, want %q", buf.String(), want)
	}

	// Drained.
	if has, err := stream.HasReplies(); err != nil {
		t.Fatalf("HasReplies: %v", err)
	} else if has {
		t.Error("the replies were not cleared")
	}
}

// CSI 14 t asks for the window size in pixels, which the terminal can only
// answer once it has been told how big a cell is.
func TestSizeReport(t *testing.T) {
	term, stream := newStreamPair(t, 20, 5)
	if err := term.ResizeCells(20, 5, 10, 20); err != nil {
		t.Fatalf("ResizeCells: %v", err)
	}

	feed(t, stream, "\x1b[14t")

	var buf bytes.Buffer
	if err := stream.WriteReplies(&buf); err != nil {
		t.Fatalf("WriteReplies: %v", err)
	}
	if want := "\x1b[4;100;200t"; buf.String() != want {
		t.Errorf("size report = %q, want %q", buf.String(), want)
	}
}
