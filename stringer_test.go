package gostty

import (
	"fmt"
	"testing"
)

// The `flags` style names only what is set, so a cell's attributes read as
// attributes rather than as a row of bools whose order the reader has to know.
func TestCellFlagsString(t *testing.T) {
	for _, c := range []struct {
		name  string
		flags CellFlags
		want  string
	}{
		{"zero", CellFlags{}, "none"},
		{"one bool", CellFlags{Bold: true}, "Bold"},
		{"declaration order", CellFlags{Selected: true, Bold: true, Faint: true}, "Bold|Faint|Selected"},
		{"enum member", CellFlags{Underline: UnderlineCurly}, "Underline:curly"},
		{"mixed", CellFlags{Italic: true, Wide: CellWidthWide}, "Italic|Wide:wide"},
	} {
		if got := c.flags.String(); got != c.want {
			t.Errorf("%s: String() = %q, want %q", c.name, got, c.want)
		}
	}

	// The padding the packed struct declares so its bits add up is not one of
	// the names, whatever it happens to hold.
	if got := (CellFlags{Pad: 0x3ffff}).String(); got != "none" {
		t.Errorf("padding leaked into String(): %q", got)
	}
}

// The whole point is that `%v` on a struct picks the method up, so a cell's
// flags are readable wherever they are printed rather than only where someone
// remembered to call String.
func TestCellFlagsFormatting(t *testing.T) {
	got := fmt.Sprintf("%v", CellFlags{Bold: true, Underline: UnderlineSingle})
	if want := "Bold|Underline:single"; got != want {
		t.Errorf("%%v = %q, want %q", got, want)
	}
}

// The `fields` style names every field, which is what a coordinate wants:
// Go's own rendering of GridPoint is `{3 4}`, and which number is which is
// exactly the question being asked.
func TestGridPointString(t *testing.T) {
	got := GridPoint{X: 3, Y: 4}.String()
	if want := "GridPoint{X:3, Y:4}"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
	sel := Selection{StartX: 1, StartY: 2, EndX: 3, EndY: 4, Rectangle: true}
	if want := "Selection{StartX:1, StartY:2, EndX:3, EndY:4, Rectangle:true}"; sel.String() != want {
		t.Errorf("Selection String() = %q, want %q", sel.String(), want)
	}
	region := ScrollRegion{Top: 0, Bottom: 23, Left: 0, Right: 79}
	if want := "ScrollRegion{Top:0, Bottom:23, Left:0, Right:79}"; region.String() != want {
		t.Errorf("ScrollRegion String() = %q, want %q", region.String(), want)
	}
}

// The flag structs that cross as packed integers keep their String through the
// backing conversion, which is the form they actually arrive in.
func TestFlagsFromBackingString(t *testing.T) {
	flags := CellFlags{Bold: true, Selected: true}
	if got := CellFlagsFromBacking(flags.Backing()).String(); got != "Bold|Selected" {
		t.Errorf("round-tripped String() = %q, want %q", got, "Bold|Selected")
	}
	if got := DragOperationsFromBacking(DragOperations{Move: true}.Backing()).String(); got != "Move" {
		t.Errorf("DragOperations String() = %q, want %q", got, "Move")
	}
}
