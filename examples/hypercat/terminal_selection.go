package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/ironpark/gostty"
	"github.com/ironpark/gostty/examples/hypercat/keys"
	"github.com/ironpark/gostty/input"
)

// selection is what this program has to keep about a selection gesture, which
// is very little: the gesture itself lives in the terminal.
type selection struct {
	// The native gesture. It counts the clicks, decides what a press means at
	// that count, snaps a drag out to whole words or lines, and says when the
	// pointer has left the surface and the viewport should follow.
	gesture *gostty.Gesture
	// Set between a press and its release, so a drag is only followed while
	// the button that started it is still down.
	dragging bool
	// The last drag reported, which is what an autoscroll tick continues from.
	last gostty.GestureDragEvent
}

// How close together two presses have to be to count as a double click, and
// how far apart they may land. Both are policy, so the gesture is told rather
// than asked.
const (
	multiClickInterval = 400 * time.Millisecond
	multiClickDistance = 2 // cells
)

// The codepoints that end a word for double-click selection.
//
// ghostty has no default for these on purpose: its own UI reads the set from
// configuration, so the choice belongs to whoever embeds it. This is that
// configuration for this program.
var wordBoundaries = []rune{
	0, ' ', '\t', '\'', '"', '`', '|', ':', ';', ',',
	'(', ')', '[', ']', '{', '}', '<', '>', '$', '│',
}

// startGesture opens the tab's selection gesture and tells it the two things
// that are this program's to decide: what a word is, and what a click at each
// count should select.
func (tab *terminal) startGesture() error {
	gesture, err := tab.vt.NewGesture()
	if err != nil {
		return err
	}
	tab.sel.gesture = gesture
	if err := gesture.SetWordBoundaries(wordBoundaries); err != nil {
		return err
	}
	// One click places the anchor and selects nothing, two the word, three the
	// command output around it -- which is what the semantic prompt marks are
	// for, and a thing this program could not work out for itself.
	if err := gesture.SetBehaviors(
		gostty.GestureBehaviorCell,
		gostty.GestureBehaviorWord,
		gostty.GestureBehaviorOutput,
	); err != nil {
		return err
	}
	return tab.syncGestureGeometry()
}

// syncGestureGeometry tells the gesture the shape of the window, which is how
// it turns a pixel position into a cell and knows when the pointer has gone
// past the bottom edge. It has to be told again whenever the grid or the font
// changes.
func (tab *terminal) syncGestureGeometry() error {
	if tab.sel.gesture == nil {
		return nil
	}
	g := tab.grid()
	return tab.sel.gesture.SetGeometry(gostty.GestureGeometry{
		Columns:      uint32(g.cols),
		CellWidth:    uint32(g.cellW),
		PaddingLeft:  0,
		ScreenHeight: uint32(g.height()),
	})
}

// handleMouse gives the mouse to the running program if it has asked for it,
// and otherwise turns a press and a drag into a selection on the screen.
//
// Almost none of what a selection means is decided here. The gesture counts
// the clicks and applies the granularity, works out what a drag covers across
// soft wraps, wide characters and the scrollback, and says when the pointer
// has left the surface. This program supplies the pointer, the clock and the
// policy, and puts the selection it is handed on the screen.
func (tab *terminal) handleMouse(m keys.Mods) error {
	// The program gets first refusal. When it has asked for the mouse, the
	// pointer is its input device and not this window's selection tool.
	if reported, err := tab.reportMouse(m); err != nil || reported {
		return err
	}
	if tab.sel.gesture == nil {
		return nil
	}

	px, py := tab.cursorPosition()
	switch {
	case tab.input.Left.Pressed:
		// A modified click on a link opens it instead of selecting.
		if tab.openLink(m) {
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

// pressSelection reports a press and applies whatever it selects: the word for
// a double click, the command output for a triple, and nothing for the first
// one, which only sets the anchor a drag grows from.
func (tab *terminal) pressSelection(px, py int) error {
	g := tab.grid()
	col, row := g.cellAt(px, py)
	sel, ok, err := tab.sel.gesture.Press(gostty.GesturePressEvent{
		X: uint16(col), Y: uint16(row),
		Xpos: float64(px), Ypos: float64(py),
		// The distance and the interval are what separate a double click from
		// two clicks that happen to be near each other in space or time.
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

// dragSelection follows the pointer, and scrolls the viewport when the gesture
// says the pointer has left the surface.
//
// Alt selects the block between the corners instead of the flow of text, which
// the gesture applies as it grows the selection rather than only at the end.
func (tab *terminal) dragSelection(px, py int, rectangle bool) error {
	col, row := tab.grid().cellAt(px, py)
	tab.sel.last = gostty.GestureDragEvent{
		X: uint16(col), Y: uint16(row),
		// The pixel position matters at the edges: whether the pointer is past
		// the bottom of the last row is a question about pixels, and the cell
		// above is clamped to the grid so it cannot answer it.
		Xpos: float64(px), Ypos: float64(py),
		Rectangle: rectangle,
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

// autoscroll moves the viewport while a drag is held past the top or bottom of
// the window, and keeps the selection growing with it. Which way, and whether
// at all, is the gesture's answer; how fast is this program's, and one row a
// frame is what a hand-held drag looks like.
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
// screen's selection when it produced none -- which is what a single click is:
// an anchor, and no selection until the pointer moves.
func (tab *terminal) applySelection(sel gostty.Selection, ok bool) error {
	return tab.onScreen(func(screen *gostty.Screen) error {
		if !ok {
			return screen.ClearSelection()
		}
		_, err := screen.SetSelection(sel)
		return err
	})
}

// endGesture ends the sequence, so the next press starts a fresh one rather
// than continuing a click count from before the tab lost the pointer.
func (tab *terminal) endGesture() {
	tab.sel.dragging = false
	if tab.sel.gesture != nil {
		_ = tab.sel.gesture.Reset()
	}
}

// copySelection hands the selected text to the window's clipboard.
func (tab *terminal) copySelection() error {
	return tab.onScreen(func(screen *gostty.Screen) error {
		text, ok, err := screen.SelectionString()
		if err != nil || !ok || len(text) == 0 {
			return err
		}
		return tab.clipboard.copy(text)
	})
}

// The selection a window can make without the pointer: select everything,
// step the ends of one around the screen, and write what is on screen out.
//
// All three are the terminal's. "Everything" is the scrollback as well as the
// viewport; moving an end of a selection has to know about soft wraps and wide
// characters; and formatting a screen has to resolve the palette and decide
// what to do with a line that was wrapped. None of that is worth reinventing
// against a flat array of cells.

// selectAll selects the whole scrollback, not only what is on screen.
func (tab *terminal) selectAll() error {
	return tab.onScreen(func(screen *gostty.Screen) error {
		_, err := screen.SelectAll()
		return err
	})
}

// The keys that move the end of a selection, and how far.
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

// adjustSelection moves the loose end of the selection with one key, and
// reports whether that key was one of its own.
//
// Where the end lands is the terminal's answer: one cell to the right at the
// end of a soft-wrapped line is the start of the next row, and one row up in a
// viewport already at the top is a row of scrollback. Only the modified arrows
// do this, so the unmodified ones still reach the program.
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
		// A selection the user is steering off the top of the viewport brings
		// the viewport with it.
		return tab.revealRow(max(moved.StartY, moved.EndY))
	})
}

// exportScrollback writes the scrollback to a file, styles and all.
//
// `Format` is what turns a screen back into text: it unwraps the soft wraps,
// resolves the palette so a colour survives without the terminal it came from,
// and can write HTML as readily as it writes plain text. Walking the cells and
// printing their codepoints would lose all three.
func (tab *terminal) exportScrollback() error {
	name := filepath.Join(os.TempDir(), fmt.Sprintf("hypercat-%s.html", time.Now().Format("20060102-150405")))
	file, err := os.Create(name)
	if err != nil {
		// A temporary directory that cannot be written to is worth saying so
		// about, and not worth ending the terminal over.
		log.Printf("save scrollback: %v", err)
		return nil
	}
	defer file.Close()

	options := gostty.FormatOptions{
		Format:         gostty.FormatterFormatHtml,
		Unwrap:         true,
		ResolvePalette: true,
	}
	// The selection when there is one, and the whole scrollback otherwise,
	// which is what "save this" means in either case.
	if err := tab.onScreen(func(screen *gostty.Screen) error {
		sel, ok, err := screen.Selection()
		if err != nil {
			return err
		}
		if !ok {
			return screen.Format(options, file)
		}
		_, err = screen.FormatSelection(options, sel, file)
		return err
	}); err != nil {
		return err
	}
	log.Printf("scrollback saved to %s", name)
	return nil
}
