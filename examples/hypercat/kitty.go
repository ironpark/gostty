package main

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"image/png"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/ironpark/gostty"
	"github.com/ironpark/gostty/sys"
)

// The Kitty graphics protocol: programs transmit images over the pty and say
// where to put them, and the terminal draws them over the grid.
//
// Everything about that except the drawing is the binding's. KittyImages is
// the same kind of per-frame snapshot as RenderState, for images: Update and
// Placements hand over a flat list of "this image, this part of it, at this
// cell, this many pixels wide", sorted back to front with everything
// off-screen dropped. The pixels come out separately, by id, because they
// almost never change: every image carries a generation, so a texture is
// uploaded once and reused until the program replaces the image.

// kittyCache is a tab's side of the protocol: this frame's placements and
// the textures they point at, kept across frames by image id.
type kittyCache struct {
	vt         *gostty.Terminal
	snapshot   *gostty.KittyImages
	placements []gostty.KittyPlacement
	textures   map[uint32]*texture
	buf        []byte
}

// texture is one uploaded image, kept until its generation moves.
type texture struct {
	generation uint64
	image      *ebiten.Image // nil when the image could not be decoded
	live       bool          // referred to by a placement this frame
}

// newKittyCache borrows vt; the cache must be closed before it.
func newKittyCache(vt *gostty.Terminal) (*kittyCache, error) {
	snapshot, err := gostty.NewKittyImages()
	if err != nil {
		return nil, err
	}
	return &kittyCache{vt: vt, snapshot: snapshot, textures: make(map[uint32]*texture)}, nil
}

func (c *kittyCache) close() {
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

// refresh rebuilds the placement snapshot for this frame and makes sure every
// image it refers to has a texture, dropping the ones nothing refers to.
func (c *kittyCache) refresh() error {
	if err := c.snapshot.Update(c.vt); err != nil {
		return fmt.Errorf("kitty update: %w", err)
	}
	n, err := c.snapshot.PlacementCount()
	if err != nil {
		return err
	}
	c.placements = grow(c.placements, int(n))
	if n > 0 {
		if _, err := c.snapshot.Placements(c.placements); err != nil {
			return fmt.Errorf("kitty placements: %w", err)
		}
	}
	for _, tex := range c.textures {
		tex.live = false
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
		if cached, hit := c.textures[p.ImageID]; hit && cached.generation == info.Generation {
			cached.live = true
			continue
		}
		// A malformed image is the program's problem, not a reason to stop
		// drawing; the failure is cached as a nil texture so it is not
		// decoded again every frame.
		img, _ := c.decode(p.ImageID, info)
		c.textures[p.ImageID] = &texture{generation: info.Generation, image: img, live: true}
	}
	for id, tex := range c.textures {
		if !tex.live {
			if tex.image != nil {
				tex.image.Deallocate()
			}
			delete(c.textures, id)
		}
	}
	return nil
}

// decode pulls one image's bytes out of the terminal. They are raw samples in
// the format ghostty stored: a PNG never gets here, because decodePNG turned
// it into RGBA as it arrived.
func (c *kittyCache) decode(id uint32, info gostty.KittyImage) (*ebiten.Image, error) {
	if info.DataLen == 0 {
		return nil, fmt.Errorf("image %d has no data", id)
	}
	c.buf = grow(c.buf, int(info.DataLen))
	n, err := c.vt.KittyImageData(id, c.buf)
	if err != nil {
		return nil, err
	}
	rgba, err := rawToRGBA(c.buf[:n], info)
	if err != nil {
		return nil, fmt.Errorf("image %d: %w", id, err)
	}
	return ebiten.NewImageFromImage(rgba), nil
}

// rawToRGBA widens the raw sample formats to the premultiplied RGBA
// Ebitengine wants.
func rawToRGBA(data []byte, info gostty.KittyImage) (*image.RGBA, error) {
	var bpp int
	switch info.Format {
	case gostty.KittyFormatGray:
		bpp = 1
	case gostty.KittyFormatGrayAlpha:
		bpp = 2
	case gostty.KittyFormatRgb:
		bpp = 3
	case gostty.KittyFormatRgba:
		bpp = 4
	default:
		return nil, fmt.Errorf("unsupported format %v", info.Format)
	}
	pixels := int(info.Width) * int(info.Height)
	if len(data) < pixels*bpp {
		return nil, fmt.Errorf("image is %d bytes, want %d for %dx%d at %d bpp", len(data), pixels*bpp, info.Width, info.Height, bpp)
	}
	out := image.NewRGBA(image.Rect(0, 0, int(info.Width), int(info.Height)))
	for i := range pixels {
		src, dst := data[i*bpp:], out.Pix[i*4:]
		switch bpp {
		case 1:
			dst[0], dst[1], dst[2], dst[3] = src[0], src[0], src[0], 0xff
		case 2:
			dst[0], dst[1], dst[2], dst[3] = src[0], src[0], src[0], src[1]
		case 3:
			dst[0], dst[1], dst[2], dst[3] = src[0], src[1], src[2], 0xff
		case 4:
			a := uint32(src[3])
			dst[0] = uint8(uint32(src[0]) * a / 0xff)
			dst[1] = uint8(uint32(src[1]) * a / 0xff)
			dst[2] = uint8(uint32(src[2]) * a / 0xff)
			dst[3] = src[3]
		}
	}
	return out, nil
}

// draw paints the placements of one layer. The snapshot is sorted by z, so a
// layer is a slice of it drawn in order; the grid turns cells into pixels.
// Virtual placements are positioned by unicode placeholders in the cells,
// which this example does not scan for, so they are skipped.
func (c *kittyCache) draw(screen *ebiten.Image, layer gostty.KittyLayer, g grid) {
	for _, p := range c.placements {
		if p.Layer != layer || p.Virtual {
			continue
		}
		tex, ok := c.textures[p.ImageID]
		if !ok || tex.image == nil {
			continue
		}
		// The source rectangle is the part of the image the placement asked
		// for; the pixel size is what it is stretched to.
		src := tex.image.SubImage(image.Rect(
			int(p.SourceX), int(p.SourceY), int(p.SourceX+p.SourceWidth), int(p.SourceY+p.SourceHeight),
		)).(*ebiten.Image)
		w, h := src.Bounds().Dx(), src.Bounds().Dy()
		if w == 0 || h == 0 || p.PixelWidth == 0 || p.PixelHeight == 0 {
			continue
		}
		op := &ebiten.DrawImageOptions{Filter: ebiten.FilterLinear}
		op.GeoM.Scale(float64(p.PixelWidth)/float64(w), float64(p.PixelHeight)/float64(h))
		op.GeoM.Translate(g.x(int(p.ViewportCol))+float64(p.XOffset), g.y(int(p.ViewportRow))+float64(p.YOffset))
		screen.DrawImage(src, op)
	}
}

// decodePNG answers ghostty's request to decode a PNG transmission. The
// bytes arrive as a copy and the reply is copied by the terminal, so nothing
// here outlives the call. No reply means the transmission fails, as it would
// without a decoder.
func decodePNG(data []byte) {
	decoded, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return
	}
	b := decoded.Bounds()
	rgba := image.NewNRGBA(b)
	draw.Draw(rgba, b, decoded, b.Min, draw.Src)
	_ = sys.ReplyPNGImage(uint32(b.Dx()), uint32(b.Dy()), rgba.Pix)
}
