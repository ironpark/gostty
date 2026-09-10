package graphics

import (
	"bytes"
	"image"
	"image/draw"
	"image/png"

	"github.com/ironpark/gostty/sys"
)

// DecodePNG answers ghostty's request to decode a PNG transmission. The bytes
// arrive as a copy and the reply is copied by the terminal, so nothing here
// outlives the call.
func DecodePNG(data []byte) {
	decoded, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return // no reply: the transmission fails, as it would without a decoder
	}
	b := decoded.Bounds()
	rgba := image.NewNRGBA(b)
	draw.Draw(rgba, b, decoded, b.Min, draw.Src)
	_ = sys.ReplyPngImage(uint32(b.Dx()), uint32(b.Dy()), rgba.Pix)
}
