package main

import (
	"testing"

	"github.com/ironpark/gostty/examples/hypercat/fonts"
)

// Everything in the window is measured in device pixels, so that a HiDPI screen
// is drawn at its own resolution instead of at a quarter of it and stretched.
// The size in the settings panel stays device-independent, because that is what
// a user means by "14px"; the scale factor is applied when the faces are built.
func TestFontSizeIsScaledByTheDisplay(t *testing.T) {
	families := fonts.Discover()
	if len(families) == 0 {
		t.Skip("no system fonts on this machine")
	}
	win := &window{dsf: 2, settings: newSettings(families, fonts.DefaultSize, 1)}
	tab := &terminal{settings: win.settings}
	win.tabs = []*terminal{tab}
	win.applyFont()

	if win.settings.fonts.Size != fonts.DefaultSize*2 {
		t.Errorf("face size = %v, want %v (%v at 2x)", win.settings.fonts.Size, fonts.DefaultSize*2, fonts.DefaultSize)
	}
	if win.settings.size != fonts.DefaultSize {
		t.Errorf("the settings size became %v; it should stay in the units the user chose", win.settings.size)
	}
	if !tab.relayout {
		t.Error("the grid was not marked for relayout after the cell changed shape")
	}
}

// The same fan-out with no system font, which is the case that always runs:
// the test above skips on a machine without one. How the bitmap itself scales
// is the settings type's own, asserted below.
func TestBitmapFallbackFansOutToTabs(t *testing.T) {
	win := &window{dsf: 2, settings: newSettings(nil, fonts.DefaultSize, 1)}
	tab := &terminal{settings: win.settings}
	win.tabs = []*terminal{tab}
	win.applyFont()

	if win.settings.fonts.FamilyName != "bitmap" {
		t.Fatalf("family = %q, want the bitmap fallback with no families to choose from", win.settings.fonts.FamilyName)
	}
	if !tab.relayout {
		t.Error("the grid was not marked for relayout after the cell changed shape")
	}
}

func TestBitmapSizeChangeAndDisplayScale(t *testing.T) {
	s := newSettings(nil, fonts.DefaultSize, 1)
	before := s.fonts.Scale
	if !s.setFont(100, 36) {
		t.Fatal("bitmap size change was ignored")
	}
	if s.family != 0 || s.size != 36 || s.fonts.Scale <= before {
		t.Fatalf("bitmap setting: family=%d size=%v scale=%v", s.family, s.size, s.fonts.Scale)
	}
	scale := s.fonts.Scale
	s.load(2)
	if s.size != 36 || s.fonts.Scale != scale*2 {
		t.Fatal("display scale changed logical size or did not scale bitmap")
	}
	face := s.fonts
	if s.setFont(0, 36) || s.fonts != face {
		t.Fatal("unchanged setting rebuilt fonts")
	}
}

func TestFontSizeLimitsApplyWithoutSystemFonts(t *testing.T) {
	s := newSettings(nil, -100, 1)
	if s.size != fonts.MinSize {
		t.Fatalf("initial size = %v", s.size)
	}
	s.setFont(-1, 10000)
	if s.size != fonts.MaxSize {
		t.Fatalf("maximum size = %v", s.size)
	}
	s.setFont(-1, -100)
	if s.size != fonts.MinSize {
		t.Fatalf("minimum size = %v", s.size)
	}
}
