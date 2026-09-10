package graphics

import (
	"fmt"
	"image"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/ironpark/gostty"
)

// The Kitty graphics protocol: programs transmit images over the pty and say
// where to put them, and the terminal draws them over the grid.
//
// Everything about that except the drawing is the binding's. `KittyImages` is
// the same kind of per-frame snapshot as `RenderState`, for images rather than
// cells: one `Update` and one `Placements` call hands over a flat list of "this
// image, this part of it, at this cell, this many pixels wide", already sorted
// back to front and with everything off-screen dropped. The pixels come out
// separately, by id, because they are the expensive part and they almost never
// change: every image carries a generation stamp, so a texture is uploaded once
// and reused until the program replaces the image or advances an animation.
//
// The one thing this program has to tell the terminal is how big a cell is. A
// placement sized in cells (`c=`/`r=`) is measured in pixels through it, so
// resizes go through `ResizeCells` rather than `Resize`.

// Cache is a tab's side of the protocol: the placements for this frame
// and the textures they point at, kept across frames and keyed by image id.
type Cache struct {
	vt         *gostty.Terminal
	snapshot   *gostty.KittyImages
	placements []gostty.KittyPlacement
	textures   map[uint32]*texture
	buf        []byte
}

// texture is one uploaded image, kept until its generation moves.
type texture struct {
	generation uint64
	image      *ebiten.Image
	// Whether a placement referred to this image in the last frame. What the
	// program deleted, we drop.
	live bool
}

// NewCache creates an image snapshot and texture cache for vt.
// The caller retains ownership of vt and must close the cache before vt.
func NewCache(vt *gostty.Terminal) (*Cache, error) {
	snapshot, err := gostty.NewKittyImages()
	if err != nil {
		return nil, err
	}
	return &Cache{vt: vt, snapshot: snapshot, textures: make(map[uint32]*texture)}, nil
}

// Close releases the snapshot and cached GPU images. A nil cache is safe.
func (c *Cache) Close() {
	if c == nil {
		return
	}
	for _, tex := range c.textures {
		if tex.image != nil {
			tex.image.Deallocate()
		}
	}
	clear(c.textures)
	_ = c.snapshot.Close()
}

// Refresh rebuilds the placement snapshot for this frame.
func (c *Cache) Refresh() error {
	if err := c.snapshot.Update(c.vt); err != nil {
		return fmt.Errorf("kitty update: %w", err)
	}
	n, err := c.snapshot.PlacementCount()
	if err != nil {
		return err
	}
	if uint(cap(c.placements)) < n {
		c.placements = make([]gostty.KittyPlacement, n)
	}
	c.placements = c.placements[:n]
	if n > 0 {
		if _, err := c.snapshot.Placements(c.placements); err != nil {
			return fmt.Errorf("kitty placements: %w", err)
		}
	}
	return c.upload()
}

// upload makes sure every image referred to this frame has a texture, and
// drops the textures nothing refers to any more.
func (c *Cache) upload() error {
	for id := range c.textures {
		c.textures[id].live = false
	}
	for _, p := range c.placements {
		if p.Virtual {
			continue
		}
		info, ok, err := c.vt.KittyImage(p.ImageID)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		cached, hit := c.textures[p.ImageID]
		if hit && cached.generation == info.Generation {
			cached.live = true
			continue
		}
		img, err := c.decode(p.ImageID, info)
		if err != nil {
			// A malformed or unsupported image is the program's problem, not a
			// reason to stop drawing. Cache the failure as a nil texture so it
			// is not decoded again every frame.
			img = nil
		}
		c.textures[p.ImageID] = &texture{generation: info.Generation, image: img, live: true}
	}
	for id, tex := range c.textures {
		if !tex.live {
			delete(c.textures, id)
		}
	}
	return nil
}

// decode pulls one image's bytes out of the terminal and turns them into
// something Ebitengine can draw.
func (c *Cache) decode(id uint32, info gostty.KittyImage) (*ebiten.Image, error) {
	if info.DataLen == 0 {
		return nil, fmt.Errorf("image %d has no data", id)
	}
	if uint64(cap(c.buf)) < info.DataLen {
		c.buf = make([]byte, info.DataLen)
	}
	c.buf = c.buf[:info.DataLen]
	n, err := c.vt.KittyImageData(id, c.buf)
	if err != nil {
		return nil, err
	}
	data := c.buf[:n]

	// The bytes are raw samples in the format ghostty stored them in. A PNG
	// never gets here: the decoder installed at startup (`DecodePNG`) turns
	// it into RGBA as it arrives.
	switch info.Format {
	case gostty.KittyFormatRgb, gostty.KittyFormatRgba, gostty.KittyFormatGray, gostty.KittyFormatGrayAlpha:
		rgba, err := rawToRGBA(data, info)
		if err != nil {
			return nil, err
		}
		return ebiten.NewImageFromImage(rgba), nil
	default:
		return nil, fmt.Errorf("image %d: unsupported format %v", id, info.Format)
	}
}

// Draw paints the placements of one layer. The snapshot is already sorted by
// z, so each layer is a slice of it, drawn in order. Cell size turns the
// placement's grid position into pixels.
//
// Virtual placements are skipped: they are positioned by the cells that
// reference them through unicode placeholders, and this example does not scan
// for those, so it has nowhere to put them.
func (c *Cache) Draw(screen *ebiten.Image, layer gostty.KittyLayer, cellW, cellH float64) {
	for _, p := range c.placements {
		if p.Layer != layer || p.Virtual {
			continue
		}
		tex, ok := c.textures[p.ImageID]
		if !ok || tex.image == nil {
			continue
		}

		// The source rectangle is the part of the image the placement asked
		// for; the pixel size is what it should be stretched to.
		src := tex.image.SubImage(image.Rect(
			int(p.SourceX), int(p.SourceY),
			int(p.SourceX+p.SourceWidth), int(p.SourceY+p.SourceHeight),
		)).(*ebiten.Image)
		w, h := src.Bounds().Dx(), src.Bounds().Dy()
		if w == 0 || h == 0 || p.PixelWidth == 0 || p.PixelHeight == 0 {
			continue
		}

		op := &ebiten.DrawImageOptions{Filter: ebiten.FilterLinear}
		op.GeoM.Scale(float64(p.PixelWidth)/float64(w), float64(p.PixelHeight)/float64(h))
		op.GeoM.Translate(
			float64(p.ViewportCol)*cellW+float64(p.XOffset),
			float64(p.ViewportRow)*cellH+float64(p.YOffset),
		)
		screen.DrawImage(src, op)
	}
}
