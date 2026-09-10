package graphics

import (
	"bytes"
	"testing"

	"github.com/ironpark/gostty"
)

func TestRawToRGBAPremultipliesAlpha(t *testing.T) {
	got, err := rawToRGBA([]byte{200, 100, 50, 128}, gostty.KittyImage{
		Format: gostty.KittyFormatRgba,
		Width:  1,
		Height: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := []byte{100, 50, 25, 128}; !bytes.Equal(got.Pix, want) {
		t.Fatalf("pixels = %v, want %v", got.Pix, want)
	}
}

func TestRawToRGBARejectsShortData(t *testing.T) {
	_, err := rawToRGBA([]byte{1, 2}, gostty.KittyImage{
		Format: gostty.KittyFormatRgb,
		Width:  1,
		Height: 1,
	})
	if err == nil {
		t.Fatal("short image data was accepted")
	}
}
