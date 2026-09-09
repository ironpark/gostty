package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/ironpark/gostty"
	"github.com/ironpark/gostty/input"
)

// The selection a window can make without the pointer: select everything,
// step the ends of one around the screen, and write what is on screen out.
//
// All three are the terminal's. "Everything" is the scrollback as well as the
// viewport; moving an end of a selection has to know about soft wraps and wide
// characters; and formatting a screen has to resolve the palette and decide
// what to do with a line that was wrapped. None of that is worth reinventing
// against a flat array of cells.

// selectAll selects the whole scrollback, not only what is on screen.
func (tab *terminalTab) selectAll() error {
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
func (tab *terminalTab) adjustSelection(key input.Key) (bool, error) {
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
func (tab *terminalTab) exportScrollback() error {
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
