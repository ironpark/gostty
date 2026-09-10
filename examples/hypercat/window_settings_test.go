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
	win := &window{dsf: 2, settings: &appearance{families: families, size: fonts.DefaultSize}}
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

// With no system font the same has to hold for the bundled bitmap, which can
// only grow in whole steps.
func TestBitmapFallbackFollowsTheDisplay(t *testing.T) {
	win := &window{dsf: 2, settings: &appearance{size: fonts.DefaultSize}}
	win.applyFont()
	if win.settings.fonts.FamilyName != "bitmap" {
		t.Fatalf("family = %q, want the bitmap fallback with no families to choose from", win.settings.fonts.FamilyName)
	}
	one := fonts.Load(nil, fonts.DefaultSize)
	if win.settings.fonts.Scale <= one.Scale {
		t.Errorf("scale at 2x is %v, want more than %v", win.settings.fonts.Scale, one.Scale)
	}
}
