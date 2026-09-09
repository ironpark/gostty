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
	app := &terminalApp{dsf: 2, settings: &appearance{families: families, size: fonts.DefaultSize}}
	tab := &terminalTab{owner: app, settings: app.settings}
	app.tabs = []*terminalTab{tab}
	app.applyFont()

	if app.settings.fonts.Size != fonts.DefaultSize*2 {
		t.Errorf("face size = %v, want %v (%v at 2x)", app.settings.fonts.Size, fonts.DefaultSize*2, fonts.DefaultSize)
	}
	if app.settings.size != fonts.DefaultSize {
		t.Errorf("the settings size became %v; it should stay in the units the user chose", app.settings.size)
	}
	if !tab.relayout {
		t.Error("the grid was not marked for relayout after the cell changed shape")
	}

	// The cell has to grow with it, since that is what the grid is laid out on.
	one := fonts.Load(families[0], fonts.DefaultSize)
	two := fonts.Load(families[0], fonts.DefaultSize*2)
	if two.CellWidth < 2*one.CellWidth-2 || two.CellHeight < 2*one.CellHeight-2 {
		t.Errorf("cell at 2x is %vx%v, want about twice %vx%v",
			two.CellWidth, two.CellHeight, one.CellWidth, one.CellHeight)
	}
}

// With no system font the same has to hold for the bundled bitmap, which can
// only grow in whole steps.
func TestBitmapFallbackFollowsTheDisplay(t *testing.T) {
	app := &terminalApp{dsf: 2, settings: &appearance{size: fonts.DefaultSize}}
	app.applyFont()
	if app.settings.fonts.FamilyName != "bitmap" {
		t.Fatalf("family = %q, want the bitmap fallback with no families to choose from", app.settings.fonts.FamilyName)
	}
	one := fonts.Load(nil, fonts.DefaultSize)
	if app.settings.fonts.Scale <= one.Scale {
		t.Errorf("scale at 2x is %v, want more than %v", app.settings.fonts.Scale, one.Scale)
	}
}
