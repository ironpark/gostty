package main

import (
	"bytes"
	"fmt"
	"image"
	"image/png"

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

// texture is one uploaded image, kept until its generation moves.
type texture struct {
	generation uint64
	image      *ebiten.Image
	// Whether a placement referred to this image in the last frame. What the
	// program deleted, we drop.
	live bool
}

// refreshImages rebuilds the placement snapshot for this frame.
func (g *game) refreshImages() error {
	if err := g.images.Update(g.vt); err != nil {
		return fmt.Errorf("kitty update: %w", err)
	}
	n, err := g.images.PlacementCount()
	if err != nil {
		return err
	}
	if uint(cap(g.placements)) < n {
		g.placements = make([]gostty.KittyPlacement, n)
	}
	g.placements = g.placements[:n]
	if n > 0 {
		if _, err := g.images.Placements(g.placements); err != nil {
			return fmt.Errorf("kitty placements: %w", err)
		}
	}
	return g.uploadImages()
}

// uploadImages makes sure every image referred to this frame has a texture, and
// drops the textures nothing refers to any more.
func (g *game) uploadImages() error {
	for id := range g.textures {
		g.textures[id].live = false
	}
	for _, p := range g.placements {
		info, ok, err := g.vt.KittyImage(p.ImageID)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		cached, hit := g.textures[p.ImageID]
		if hit && cached.generation == info.Generation {
			cached.live = true
			continue
		}
		img, err := g.decodeImage(p.ImageID, info)
		if err != nil {
			// A malformed or unsupported image is the program's problem, not a
			// reason to stop drawing. Cache the failure as a nil texture so it
			// is not decoded again every frame.
			img = nil
		}
		g.textures[p.ImageID] = &texture{generation: info.Generation, image: img, live: true}
	}
	for id, tex := range g.textures {
		if !tex.live {
			delete(g.textures, id)
		}
	}
	return nil
}

// decodeImage pulls one image's bytes out of the terminal and turns them into
// something Ebitengine can draw.
func (g *game) decodeImage(id uint32, info gostty.KittyImage) (*ebiten.Image, error) {
	if info.DataLen == 0 {
		return nil, fmt.Errorf("image %d has no data", id)
	}
	if uint64(cap(g.imageBuf)) < info.DataLen {
		g.imageBuf = make([]byte, info.DataLen)
	}
	g.imageBuf = g.imageBuf[:info.DataLen]
	n, err := g.vt.KittyImageData(id, g.imageBuf)
	if err != nil {
		return nil, err
	}
	data := g.imageBuf[:n]

	// The bytes are as the program sent them, so the format says how to read
	// them. Only PNG needs a decoder; the rest are raw samples.
	switch gostty.KittyFormat(info.Format) {
	case gostty.KittyFormatPng:
		decoded, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		return ebiten.NewImageFromImage(decoded), nil
	case gostty.KittyFormatRgb, gostty.KittyFormatRgba, gostty.KittyFormatGray, gostty.KittyFormatGrayAlpha:
		rgba, err := rawToRGBA(data, info)
		if err != nil {
			return nil, err
		}
		return ebiten.NewImageFromImage(rgba), nil
	default:
		return nil, fmt.Errorf("image %d: unsupported format %v", id, gostty.KittyFormat(info.Format))
	}
}

// rawToRGBA widens the raw sample formats to the RGBA Ebitengine wants.
func rawToRGBA(data []byte, info gostty.KittyImage) (*image.RGBA, error) {
	var bpp int
	switch gostty.KittyFormat(info.Format) {
	case gostty.KittyFormatGray:
		bpp = 1
	case gostty.KittyFormatGrayAlpha:
		bpp = 2
	case gostty.KittyFormatRgb:
		bpp = 3
	case gostty.KittyFormatRgba:
		bpp = 4
	}
	pixels := int(info.Width) * int(info.Height)
	if len(data) < pixels*bpp {
		return nil, fmt.Errorf("image is %d bytes, want %d for %dx%d at %d bpp",
			len(data), pixels*bpp, info.Width, info.Height, bpp)
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
			// Ebitengine wants alpha-premultiplied bytes.
			a := uint32(src[3])
			dst[0] = uint8(uint32(src[0]) * a / 0xff)
			dst[1] = uint8(uint32(src[1]) * a / 0xff)
			dst[2] = uint8(uint32(src[2]) * a / 0xff)
			dst[3] = src[3]
		}
	}
	return out, nil
}

// drawImages draws the placements in one z range. The snapshot is already
// sorted, so this is a slice of it: the negative z placements go under the
// text, the rest over it.
func (g *game) drawImages(screen *ebiten.Image, underText bool) {
	for _, p := range g.placements {
		if (p.Z < 0) != underText {
			continue
		}
		tex, ok := g.textures[p.ImageID]
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
			float64(p.ViewportCol)*g.fonts.cellW+float64(p.XOffset),
			float64(p.ViewportRow)*g.fonts.cellH+float64(p.YOffset),
		)
		screen.DrawImage(src, op)
	}
}
