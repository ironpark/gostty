package gostty

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/ironpark/gostty/sys"
)

// A 1x1 opaque red PNG, built at test time so the bytes are guaranteed to be
// what image/png produces.
func redPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.NRGBA{R: 0xff, A: 0xff})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return buf.Bytes()
}

// Transmit a PNG (`f=100`) with the given id. Unlike the raw formats the
// command carries no dimensions: they come out of the decoded image.
func transmitPNG(t *testing.T, s *Stream, id int, data []byte) {
	t.Helper()
	feed(t, s, fmt.Sprintf("\x1b_Ga=T,f=100,t=d,i=%d;%s\x1b\\", id, base64.StdEncoding.EncodeToString(data)))
}

// Installs image/png as the decoder for the duration of the test. The hooks
// are process-global, so every test that sets one clears it again.
func installPNGDecoder(t *testing.T) *int {
	t.Helper()
	calls := new(int)
	sys.OnPngDecodeRequest(func(n uint) {
		*calls++
		data := make([]byte, n)
		if got := sys.PngRequestData(data); got != n {
			t.Errorf("PngRequestData wrote %d, want %d", got, n)
			return
		}
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Errorf("png.Decode: %v", err)
			return
		}
		b := img.Bounds()
		rgba := image.NewNRGBA(b)
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				rgba.Set(x, y, img.At(x, y))
			}
		}
		if err := sys.ReplyPngImage(uint32(b.Dx()), uint32(b.Dy()), rgba.Pix); err != nil {
			t.Errorf("ReplyPngImage: %v", err)
		}
	})
	t.Cleanup(sys.Clear)
	return calls
}

// Without a decoder ghostty refuses the transmission up front: the image
// never exists, which is what a renderer without PNG support wants.
func TestPngRefusedWithoutDecoder(t *testing.T) {
	term, stream := newStreamPair(t, 20, 5)
	transmitPNG(t, stream, 5, redPNG(t))
	if _, ok, err := term.KittyImage(5); err != nil || ok {
		t.Fatalf("KittyImage(5) without a decoder = ok %v, err %v; want false, nil", ok, err)
	}
}

// With one installed the PNG is decoded as it arrives, and reaches Go as an
// RGBA image with the decoded dimensions.
func TestPngDecodedOnArrival(t *testing.T) {
	calls := installPNGDecoder(t)
	term, stream := newStreamPair(t, 20, 5)
	transmitPNG(t, stream, 6, redPNG(t))

	if *calls != 1 {
		t.Fatalf("decoder called %d times, want 1", *calls)
	}
	img, ok, err := term.KittyImage(6)
	if err != nil || !ok {
		t.Fatalf("KittyImage(6) = ok %v, err %v; want true, nil", ok, err)
	}
	if img.Format != KittyFormatRgba {
		t.Errorf("format = %v, want rgba", img.Format)
	}
	if img.Width != 1 || img.Height != 1 || img.DataLen != 4 {
		t.Errorf("image = %dx%d, %d bytes; want 1x1, 4 bytes", img.Width, img.Height, img.DataLen)
	}
	data := make([]byte, img.DataLen)
	if _, err := term.KittyImageData(6, data); err != nil {
		t.Fatalf("KittyImageData: %v", err)
	}
	if want := []byte{0xff, 0, 0, 0xff}; !bytes.Equal(data, want) {
		t.Errorf("pixels = %v, want %v", data, want)
	}
}

// A decoder that does not reply fails the transmission the same way as
// having none, and a reply of the wrong size is refused before it can.
func TestPngDecoderMustReply(t *testing.T) {
	var sizeErr error
	sys.OnPngDecodeRequest(func(n uint) {
		sizeErr = sys.ReplyPngImage(2, 2, []byte{1, 2, 3, 4})
	})
	t.Cleanup(sys.Clear)
	term, stream := newStreamPair(t, 20, 5)
	transmitPNG(t, stream, 7, redPNG(t))
	if !errors.Is(sizeErr, sys.ErrSizeMismatch) {
		t.Errorf("ReplyPngImage with 4 bytes for 2x2 = %v, want ErrSizeMismatch", sizeErr)
	}
	if _, ok, err := term.KittyImage(7); err != nil || ok {
		t.Errorf("KittyImage(7) after an unanswered decode = ok %v, err %v; want false, nil", ok, err)
	}
}

// Replies are only meaningful inside a request.
func TestSysRepliesOutsideRequest(t *testing.T) {
	if err := sys.ReplyPngImage(1, 1, []byte{0, 0, 0, 0}); !errors.Is(err, sys.ErrNoPendingRequest) {
		t.Errorf("ReplyPngImage outside a request = %v, want ErrNoPendingRequest", err)
	}
	if err := sys.ReplySecureRandom([]byte{1}); !errors.Is(err, sys.ErrNoPendingRequest) {
		t.Errorf("ReplySecureRandom outside a request = %v, want ErrNoPendingRequest", err)
	}
	// Installing and clearing is idempotent and leaves no request pending.
	sys.OnSecureRandomRequest(func(uint) {})
	sys.Clear()
	sys.Clear()
	if n := sys.PngRequestData(make([]byte, 8)); n != 0 {
		t.Errorf("PngRequestData outside a request wrote %d, want 0", n)
	}
}
