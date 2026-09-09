package main

import (
	"image/color"
	"testing"
	"time"

	"github.com/ironpark/gostty"
)

// Whether the cursor blinks is the terminal's answer to DECSCUSR, not a fixed
// policy of the renderer: a program that asks for a steady cursor gets one.
func TestCursorBlinkFollowsTheTerminal(t *testing.T) {
	app := newTabTestApp(t)
	tab := app.current()

	feedTab(t, tab, "\x1b[1 q") // blinking block
	if !tab.frame.cursor.blinking {
		t.Error("cursor is steady after DECSCUSR 1, want blinking")
	}
	feedTab(t, tab, "\x1b[2 q") // steady block
	if tab.frame.cursor.blinking {
		t.Error("cursor blinks after DECSCUSR 2, want steady")
	}
	if tab.frame.cursor.style != gostty.CursorStyleBlock {
		t.Errorf("cursor style = %v, want a block", tab.frame.cursor.style)
	}
}

// A steady cursor is always drawn; a blinking one is drawn for half of each
// period, from the clock rather than from a frame counter.
func TestCursorLitHalfThePeriod(t *testing.T) {
	base := time.Unix(0, 0)
	if !cursorLit(false, base.Add(cursorBlinkPeriod*3/4)) {
		t.Error("a steady cursor went dark")
	}
	if !cursorLit(true, base) {
		t.Error("a blinking cursor is dark at the start of its period")
	}
	if cursorLit(true, base.Add(cursorBlinkPeriod*3/4)) {
		t.Error("a blinking cursor is lit in the second half of its period")
	}
	if !cursorLit(true, base.Add(cursorBlinkPeriod+1)) {
		t.Error("the next period did not start lit")
	}
}

// SGR 5 marks a cell as blinking and stops there: when it is dark is a
// question about a clock, so it is this side's to answer -- and to repaint for,
// since the terminal marks no row dirty when the phase turns over.
func TestBlinkingCellsRepaintOnThePhase(t *testing.T) {
	app := newTabTestApp(t)
	tab := app.current()

	feedTab(t, tab, "\x1b[5mblinking\x1b[0m")
	if !tab.frame.cells[0].Flags.Blink {
		t.Fatal("the cell is not marked as blinking")
	}
	tab.frame.redraw.clear()

	// Turning the phase over marks the row holding the blinking cell.
	tab.frame.blink = !blinkLit(time.Now())
	tab.frame.tickBlink(time.Now(), tab.grid())
	if !tab.frame.redraw.marked(0) {
		t.Error("the row with the blinking cell was not repainted")
	}

	// A screen with nothing blinking costs nothing.
	feedTab(t, tab, "\x1b[2J\x1b[Hsteady")
	tab.frame.redraw.clear()
	tab.frame.blink = !blinkLit(time.Now())
	tab.frame.tickBlink(time.Now(), tab.grid())
	if tab.frame.redraw.marked(0) {
		t.Error("a row with nothing blinking was repainted for the phase")
	}
}

// OSC 12 sets the cursor's colour. Without reading it back the cursor is drawn
// in the foreground colour whatever the program asked for.
func TestCursorTakesTheColorTheProgramSet(t *testing.T) {
	app := newTabTestApp(t)
	tab := app.current()

	if tab.frame.cursor.hasColor {
		t.Fatal("a cursor colour was reported before the program set one")
	}
	feedTab(t, tab, "\x1b]12;#ff0000\x07")
	if !tab.frame.cursor.hasColor {
		t.Fatal("no cursor colour after OSC 12")
	}
	if want := (color.RGBA{R: 0xff, A: 0xff}); tab.frame.cursor.color != want {
		t.Errorf("cursor colour = %v, want %v", tab.frame.cursor.color, want)
	}
}

// The cursor on the tail of a wide character covers both of its cells, so the
// glyph underneath is not left half-lit. The index it draws the character from
// is the head's, not the tail's.
func TestCursorOnAWideCharacter(t *testing.T) {
	app := newTabTestApp(t)
	tab := app.current()

	feedTab(t, tab, "한\x1b[1D") // print it, then step back onto its tail
	if !tab.frame.cursor.wideTail {
		t.Fatal("cursor is not on the wide character's tail")
	}
	i := tab.cursorCellIndex()
	if i < 0 || tab.frame.cells[i].Codepoint != '한' {
		t.Errorf("cursorCellIndex() = %d, want the cell holding the character", i)
	}
}
