package thecat

import "math"

// GridWorld turns occupied cells into ledges and stacked cells into walls.
// HasInk belongs to the caller, so this adapter needs no terminal or font types.
// Cell dimensions and target coordinates are in the caller's pixels.
type GridWorld struct {
	Cols, Rows            int
	CellWidth, CellHeight float64
	HasInk                func(col, row int) bool
	TargetX, TargetY      float64
	TargetActive          bool
}

var _ World = GridWorld{}

// GroundBelow finds the first line of text at or below a point, and reports the
// top of it -- which is where a cat standing on that line puts its feet.
//
// The bottom of the window is the floor, so there is always an answer.
func (g GridWorld) GroundBelow(x, from float64) (float64, bool) {
	floor := float64(g.Rows) * g.CellHeight
	// Floor, not a cast: a cast truncates towards zero, which would put every
	// point in the first column-width to the left of the window into column 0.
	if g.CellWidth <= 0 || g.CellHeight <= 0 {
		return floor, true
	}
	col := int(math.Floor(x / g.CellWidth))
	if col < 0 || col >= g.Cols || g.HasInk == nil {
		return floor, true
	}
	// The first row boundary at or below the point.
	row := max(int(math.Ceil(from/g.CellHeight)), 0)
	for ; row < g.Rows; row++ {
		if g.HasInk(col, row) {
			return float64(row) * g.CellHeight, true
		}
	}
	return floor, true
}

// Solid reports what the cat cannot walk through, as opposed to what it can
// stand on.
//
// Only stacked text counts: a cell is solid when the cell above it has a glyph
// too. A single line of output is therefore a ledge and nothing more -- the cat
// steps onto it from above and walks past it from the side -- while a paragraph
// is a wall to climb and a ceiling to duck under.
//
// The alternative, every glyph a wall, was tried and is unusable: a cat several
// text rows tall fits nowhere on a screen with output on it, so it spends its
// life pressed against the letter it happens to be next to. What counts as a
// wall is a property of this grid adapter; Cat itself only sees a World.
func (g GridWorld) Solid(x, y float64) bool {
	if g.CellWidth <= 0 || g.CellHeight <= 0 {
		return false
	}
	col := int(math.Floor(x / g.CellWidth))
	row := int(math.Floor(y / g.CellHeight))
	if col < 0 || col >= g.Cols || row <= 0 || row >= g.Rows {
		return false
	}
	if g.HasInk == nil {
		return false
	}
	return g.HasInk(col, row) && g.HasInk(col, row-1)
}

func (g GridWorld) Bounds() (w, h float64) {
	return float64(g.Cols) * g.CellWidth, float64(g.Rows) * g.CellHeight
}

func (g GridWorld) Attention() (x, y float64, ok bool) {
	return g.TargetX, g.TargetY, g.TargetActive
}
