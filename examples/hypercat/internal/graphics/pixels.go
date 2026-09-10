package graphics

import (
	"fmt"
	"image"

	"github.com/ironpark/gostty"
)

// rawToRGBA widens the raw sample formats to the RGBA Ebitengine wants.
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
