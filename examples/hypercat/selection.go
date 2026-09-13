package main

import (
	"bufio"
	"io"
	"log"
	"time"

	"github.com/ironpark/gostty"
	"github.com/ironpark/gostty/examples/hypercat/keys"
	"github.com/ironpark/gostty/input"
)

// Selection ---------------------------------------------------------------------
//
// Almost none of what a selection means is decided here. The native gesture
// counts clicks, applies the granularity, works out what a drag covers across
// soft wraps, wide characters and the scrollback, and says when the pointer
// has left the surface. This program supplies the pointer, the clock and the
// policy, and puts the selection it is handed on the screen.

// selection is the little this program keeps about a gesture.
type selection struct {
	gesture  *gostty.Gesture
	dragging bool                    // between a press and its release
	last     gostty.GestureDragEvent // what an autoscroll tick continues from
}

// Double-click policy: how close in time and space two presses have to be.
const (
	multiClickInterval = 400 * time.Millisecond
	multiClickDistance = 2 // cells
)

// wordBoundaries end a word for double-click selection. ghostty has no
// default on purpose: its own UI reads the set from configuration.
var wordBoundaries = []rune{
	0, ' ', '\t', '\'', '"', '`', '|', ':', ';', ',',
	'(', ')', '[', ']', '{', '}', '<', '>', '$', '│',
}

// startGesture opens the gesture and tells it what is this program's to
// decide: what a word is, and what a click at each count selects.
func (tab *terminal) startGesture() error {
	gesture, err := tab.vt.NewGesture()
	if err != nil {
		return err
	}
	tab.sel.gesture = gesture
	tab.core.AddGesture(gesture)
	if err := gesture.SetWordBoundaries(wordBoundaries); err != nil {
		return err
	}
	// One click places the anchor, two select the word, three the command
	// output around it -- which the semantic prompt marks are for.
	if err := gesture.SetBehaviors(gostty.GestureBehaviorCell, gostty.GestureBehaviorWord, gostty.GestureBehaviorOutput); err != nil {
		return err
	}
	return tab.syncGestureGeometry()
}

// syncGestureGeometry tells the gesture the shape of the window, which is how
// it turns pixels into cells and knows when the pointer is past the bottom.
func (tab *terminal) syncGestureGeometry() error {
	if tab.sel.gesture == nil {
		return nil
	}
	g := tab.grid()
	return tab.sel.gesture.SetGeometry(gostty.GestureGeometry{
		Columns: uint32(g.cols), CellWidth: uint32(g.cellW), ScreenHeight: uint32(g.height()),
	})
}

// handleMouse gives the mouse to the program if it asked for it, and
// otherwise turns a press and a drag into a selection.
func (tab *terminal) handleMouse(m keys.Mods) error {
	if reported, err := tab.reportMouse(m); err != nil || reported {
		return err
	}
	if tab.sel.gesture == nil {
		return nil
	}
	px, py := tab.cursorPosition()
	switch {
	case tab.input.Left.Pressed:
		if tab.openLink(m) { // a modified click on a link opens it instead
			return nil
		}
		return tab.pressSelection(px, py)
	case tab.sel.dragging && !tab.input.Left.Down:
		tab.sel.dragging = false
		col, row := tab.grid().cellAt(px, py)
		return tab.sel.gesture.Release(uint16(col), uint16(row))
	case tab.sel.dragging:
		return tab.dragSelection(px, py, m.Alt)
	}
	return nil
}

// pressSelection reports a press and applies what it selects: nothing for a
// single click, which only sets the anchor a drag grows from.
func (tab *terminal) pressSelection(px, py int) error {
	g := tab.grid()
	col, row := g.cellAt(px, py)
	sel, ok, err := tab.sel.gesture.Press(gostty.GesturePressEvent{
		X: uint16(col), Y: uint16(row), Xpos: float64(px), Ypos: float64(py),
		MaxDistance:      multiClickDistance * g.cellW,
		RepeatIntervalNs: uint64(multiClickInterval),
		TimeNs:           time.Now().UnixNano(),
	})
	if err != nil {
		return err
	}
	tab.sel.dragging = true
	return tab.applySelection(sel, ok)
}

// dragSelection follows the pointer. Alt selects the block between the
// corners rather than the flow of text; the gesture applies that as it grows.
func (tab *terminal) dragSelection(px, py int, rectangle bool) error {
	col, row := tab.grid().cellAt(px, py)
	// The pixel position matters at the edges: whether the pointer is past
	// the last row is a question the clamped cell cannot answer.
	tab.sel.last = gostty.GestureDragEvent{
		X: uint16(col), Y: uint16(row), Xpos: float64(px), Ypos: float64(py), Rectangle: rectangle,
	}
	sel, ok, err := tab.sel.gesture.Drag(tab.sel.last)
	if err != nil {
		return err
	}
	if err := tab.applySelection(sel, ok); err != nil {
		return err
	}
	return tab.autoscroll()
}

// autoscroll moves the viewport one row a frame while a drag is held past the
// top or bottom, and keeps the selection growing with it.
func (tab *terminal) autoscroll() error {
	direction, err := tab.sel.gesture.Autoscroll()
	if err != nil || direction == gostty.GestureAutoscrollDirectionNone {
		return err
	}
	delta := 1
	if direction == gostty.GestureAutoscrollDirectionUp {
		delta = -1
	}
	if err := tab.vt.ScrollViewport(gostty.ScrollViewportDelta(delta)); err != nil {
		return err
	}
	sel, ok, err := tab.sel.gesture.AutoscrollTick(tab.sel.last)
	if err != nil {
		return err
	}
	return tab.applySelection(sel, ok)
}

// applySelection puts what the gesture produced on the screen, or clears the
// screen's selection when it produced none.
func (tab *terminal) applySelection(sel gostty.Selection, ok bool) error {
	return tab.onScreen(func(screen *gostty.Screen) error {
		if !ok {
			return screen.ClearSelection()
		}
		_, err := screen.SetSelection(sel)
		return err
	})
}

// endGesture ends the click sequence, so the next press is a first click.
func (tab *terminal) endGesture() {
	tab.sel.dragging = false
	if tab.sel.gesture != nil {
		_ = tab.sel.gesture.Reset()
	}
}

func (tab *terminal) copySelection() error {
	return tab.onScreen(func(screen *gostty.Screen) error {
		text, ok, err := screen.SelectionString()
		if err != nil || !ok || len(text) == 0 {
			return err
		}
		return tab.clipboard.copy(text)
	})
}

// selectAll selects the whole scrollback, not only what is on screen.
func (tab *terminal) selectAll() error {
	return tab.onScreen(func(screen *gostty.Screen) error {
		_, err := screen.SelectAll()
		return err
	})
}

// The keys that move the loose end of a selection, and how far.
var selectionAdjustments = map[input.Key]gostty.SelectionAdjustment{
	input.KeyArrowLeft:  gostty.SelectionAdjustmentLeft,
	input.KeyArrowRight: gostty.SelectionAdjustmentRight,
	input.KeyArrowUp:    gostty.SelectionAdjustmentUp,
	input.KeyArrowDown:  gostty.SelectionAdjustmentDown,
	input.KeyHome:       gostty.SelectionAdjustmentBeginningOfLine,
	input.KeyEnd:        gostty.SelectionAdjustmentEndOfLine,
	input.KeyPageUp:     gostty.SelectionAdjustmentPageUp,
	input.KeyPageDown:   gostty.SelectionAdjustmentPageDown,
}

// adjustSelection moves the end of the selection with one key and reports
// whether the key was one of its own. Where the end lands is the terminal's
// answer: it knows about soft wraps, wide characters and the scrollback.
func (tab *terminal) adjustSelection(key input.Key) (bool, error) {
	adjustment, bound := selectionAdjustments[key]
	if !bound {
		return false, nil
	}
	return true, tab.onScreen(func(screen *gostty.Screen) error {
		current, ok, err := screen.Selection()
		if err != nil || !ok {
			return err
		}
		moved, ok, err := screen.SelectionAdjust(current, adjustment)
		if err != nil || !ok {
			return err
		}
		if _, err := screen.SetSelection(moved); err != nil {
			return err
		}
		return tab.revealRow(max(moved.StartY, moved.EndY))
	})
}

// Viewport ----------------------------------------------------------------------

// viewportTop is the scrollback row at the top of the screen, which turns a
// position in the scrollback into a row on the grid.
func (tab *terminal) viewportTop() (top uint32, err error) {
	err = tab.onScreen(func(screen *gostty.Screen) error {
		top, err = screen.ViewportTop()
		return err
	})
	return top, err
}

// revealRow scrolls only when the row is outside the viewport.
func (tab *terminal) revealRow(row uint32) error {
	top, err := tab.viewportTop()
	if err != nil || (row >= top && row < top+uint32(tab.rows)) {
		return err
	}
	return tab.vt.ScrollViewport(gostty.ScrollViewportRow(uint(row)))
}

// The scrollbar is Screen.Scrollbar: the terminal counts the scrollback and
// where the viewport sits in it, in rows, and hands over the three numbers a
// thumb is drawn from.

// How long the bar stays visible after the last scroll, in frames.
const scrollbarFrames = 90

type scrollbarState struct {
	bar     gostty.Scrollbar
	visible int
}

// refreshScrollbar reads the viewport's position and decides how long the
// bar stays up: while away from the bottom, and briefly after moving, the
// way an overlay scrollbar does.
func (tab *terminal) refreshScrollbar() error {
	return tab.onScreen(func(screen *gostty.Screen) error {
		bar, err := screen.Scrollbar()
		if err != nil {
			return err
		}
		tab.frame.updateScrollbar(bar)
		return nil
	})
}

func (f *frame) updateScrollbar(bar gostty.Scrollbar) {
	previous := f.scrollbar.bar
	f.scrollbar.bar = bar
	awayFromBottom := bar.Offset+bar.Len < bar.Total
	moved := bar.Offset != previous.Offset || bar.Total != previous.Total
	switch {
	case bar.Total <= bar.Len: // nothing to scroll through
		f.scrollbar.visible = 0
	case awayFromBottom || moved:
		f.scrollbar.visible = scrollbarFrames
	case f.scrollbar.visible > 0:
		f.scrollbar.visible--
	}
}

// exportScrollback saves the selection, or the whole scrollback, as HTML.
func (tab *terminal) exportScrollback() {
	name, err := saveTemp("", "hypercat-*.html", func(out io.Writer) error {
		// The formatter writes a chunk at a time; buffer so the whole
		// scrollback is not a syscall per chunk.
		buffered := bufio.NewWriter(out)
		if err := tab.formatScrollback(buffered); err != nil {
			return err
		}
		return buffered.Flush()
	})
	if err != nil {
		log.Printf("save scrollback: %v", err) // reported without ending the session
		return
	}
	log.Printf("scrollback saved to %s", name)
}

// formatScrollback writes styled HTML through the native formatter: soft
// wraps unwrapped, the palette resolved, and only the selection if there is
// one.
func (tab *terminal) formatScrollback(out io.Writer) error {
	options := gostty.FormatOptions{Format: gostty.FormatterFormatHtml, Unwrap: true, ResolvePalette: true}
	var sel gostty.Selection
	var ok bool
	if err := tab.onScreen(func(screen *gostty.Screen) (err error) {
		sel, ok, err = screen.Selection()
		return err
	}); err != nil {
		return err
	}
	if !ok {
		return tab.vt.Format(options, out)
	}
	_, err := tab.vt.FormatSelection(options, sel, out)
	return err
}
