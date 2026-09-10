package frontend

import (
	"image/color"

	"github.com/ironpark/gostty"
	"github.com/ironpark/gostty/examples/hypercat/fonts"
	"github.com/ironpark/gostty/examples/hypercat/internal/graphics"
	"github.com/ironpark/gostty/examples/hypercat/thecat"
	"github.com/ironpark/gostty/examples/hypercat/ui"
)

// Damage lets drawing consume pending repaints without reading terminal state.
type Damage interface {
	Full() bool
	MarkAll()
	Take(row int) bool
	Clear()
}

// Frame is a borrowed snapshot, with no native terminal, stream, or render-state
// handles. Its slices remain valid until the next application update.
type Frame struct {
	Cols, Rows int
	Cells      []gostty.RenderCell
	Clusters   []string
	Matches    []bool
	Cursor     Cursor
	Colors     Colors
	Link       Link
	Scrollbar  Scrollbar
	Blink      bool
	Damage     Damage
	Fonts      *fonts.Set
	Emoji      *fonts.Emoji
	Theme      ui.Theme
	Scale      float64
	Images     *graphics.Cache
	Cat        *thecat.Companion
}

type Colors struct{ TerminalBg, TerminalFg, Bg, Fg color.RGBA }
type Cursor struct {
	X, Y                                  uint16
	Visible, Blinking, WideTail, Password bool
	Style                                 gostty.CursorStyle
	Color                                 color.RGBA
	HasColor                              bool
}
type Link struct {
	URI             string
	Row, Start, End int
}

func (l Link) valid() bool { return l.URI != "" && l.End > l.Start }

type Scrollbar struct {
	Bar     gostty.Scrollbar
	Visible int
}

// ClusterAt returns a cell's saved grapheme cluster, or its single codepoint.
func (f Frame) ClusterAt(col, row int, cell gostty.RenderCell) string {
	if i := row*f.Cols + col; i < len(f.Clusters) && f.Clusters[i] != "" {
		return f.Clusters[i]
	}
	return glyphString(cell.Codepoint)
}

// CursorCellIndex resolves the leading cell of a wide character under the cursor.
func (f Frame) CursorCellIndex() int {
	x, y := int(f.Cursor.X), int(f.Cursor.Y)
	if f.Cursor.WideTail {
		x--
	}
	i := y*f.Cols + x
	if x < 0 || x >= f.Cols || i < 0 || i >= len(f.Cells) {
		return -1
	}
	return i
}

// Renderer owns GPU layers for one terminal. Bell counts remaining draw frames.
type Renderer struct {
	frame  Frame
	layers gridCanvas
	Bell   int
}

func (r *Renderer) Close() { r.layers.close() }

func (r *Renderer) grid() grid {
	return grid{r.frame.Cols, r.frame.Rows, r.frame.Fonts.CellWidth, r.frame.Fonts.CellHeight}
}
func (r *Renderer) fonts() *fonts.Set      { return r.frame.Fonts }
func (r *Renderer) emoji() *fonts.Emoji    { return r.frame.Emoji }
func (r *Renderer) currentTheme() ui.Theme { return r.frame.Theme }
func (r *Renderer) themeColor(c color.RGBA) color.RGBA {
	return r.frame.Theme.ResolveColor(c, r.frame.Colors.TerminalBg, r.frame.Colors.TerminalFg)
}

type grid struct {
	cols, rows   int
	cellW, cellH float64
}

func (g grid) x(col int) float64      { return float64(col) * g.cellW }
func (g grid) y(row int) float64      { return float64(row) * g.cellH }
func (g grid) width() float64         { return g.x(g.cols) }
func (g grid) height() float64        { return g.y(g.rows) }
func (g grid) index(col, row int) int { return row*g.cols + col }
