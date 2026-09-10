package appearance

import (
	"testing"

	"github.com/ironpark/gostty/examples/hypercat/fonts"
)

func TestBitmapSizeChangeAndDisplayScale(t *testing.T) {
	s := New(nil, fonts.DefaultSize, 1)
	before := s.Fonts().Scale
	if !s.SetFont(100, 36) {
		t.Fatal("bitmap size change was ignored")
	}
	if s.FamilyIndex() != 0 || s.Size() != 36 || s.Fonts().Scale <= before {
		t.Fatalf("bitmap setting: family=%d size=%v scale=%v", s.FamilyIndex(), s.Size(), s.Fonts().Scale)
	}
	scale := s.Fonts().Scale
	s.Reload(2)
	if s.Size() != 36 || s.Fonts().Scale != scale*2 {
		t.Fatal("display scale changed logical size or did not scale bitmap")
	}
	face := s.Fonts()
	if s.SetFont(0, 36) || s.Fonts() != face {
		t.Fatal("unchanged setting rebuilt fonts")
	}
}

func TestFontSizeLimitsApplyWithoutSystemFonts(t *testing.T) {
	s := New(nil, -100, 1)
	if s.Size() != fonts.MinSize {
		t.Fatalf("initial size = %v", s.Size())
	}
	s.SetFont(-1, 10000)
	if s.Size() != fonts.MaxSize {
		t.Fatalf("maximum size = %v", s.Size())
	}
	s.SetFont(-1, -100)
	if s.Size() != fonts.MinSize {
		t.Fatalf("minimum size = %v", s.Size())
	}
}
