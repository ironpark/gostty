package main

import (
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/ironpark/gostty"
	"github.com/ironpark/gostty/examples/hypercat/keys"
	"github.com/ironpark/gostty/input"
)

// Mouse and focus reporting: the two input streams the running program can ask
// for and this window has to hand over.
//
// Whether a mouse event is reported at all, and in which of the half dozen
// encodings, is a property of the modes the program has set (X10, normal,
// button-event, any-event; X10, SGR, UTF-8, urxvt). None of that is decided
// here. `input.EncodeMouse` reads the modes off the terminal and writes
// nothing when the program has not asked, so "did it write anything" is also
// the answer to "does the program want the mouse". That is what tells this
// program whether the drag belongs to the shell or to the selection.

// reportState is what the program has been told about the pointer and the
// focus so far, which is what decides whether the next frame has anything new
// to tell it.
type reportState struct {
	// The cell the pointer was last reported in: motion goes on the wire per
	// cell, not per pixel.
	mouseCol, mouseRow int
	// Wheel movement that has not yet added up to a notch.
	wheel float64
	// Set while a press has gone to the program, so the release does too.
	mouseGrabbed bool
	focused      bool
	// Frames spent focused, which is what tells a key held down through the
	// click that focused the window from one pressed afterwards.
	focusedFrames int
}

// The mouse buttons this window reports. Ebitengine knows about three.
var mouseButtons = [...]struct {
	ebiten ebiten.MouseButton
	vt     input.MouseButton
}{
	{ebiten.MouseButtonLeft, input.MouseButtonLeft},
	{ebiten.MouseButtonRight, input.MouseButtonRight},
	{ebiten.MouseButtonMiddle, input.MouseButtonMiddle},
}

// reportMouse hands this frame's mouse activity to the program, and reports
// whether the program took it.
//
// Holding Shift takes the mouse back for the window, which is the convention
// every emulator follows: it is the only way to select text in a full-screen
// program that has grabbed the mouse.
func (tab *terminalTab) reportMouse(m keys.Mods) (bool, error) {
	if m.Shift {
		tab.reports.mouseGrabbed = false
		return false, nil
	}

	px, py := tab.cursorPosition()
	pressed := false
	for _, b := range mouseButtons {
		if ebiten.IsMouseButtonPressed(b.ebiten) {
			pressed = true
		}
	}

	tab.report.reset()
	for _, b := range mouseButtons {
		switch {
		case inpututil.IsMouseButtonJustPressed(b.ebiten):
			if err := tab.encodeMouse(input.MouseActionPress, b.vt, true, px, py, m, true); err != nil {
				return false, err
			}
			// A press is what settles who owns the drag that follows: a
			// program in X10 mode reports the press and nothing else, and the
			// release still has to go to it rather than end a selection.
			tab.reports.mouseGrabbed = len(tab.report) > 0
		case inpututil.IsMouseButtonJustReleased(b.ebiten):
			if err := tab.encodeMouse(input.MouseActionRelease, b.vt, true, px, py, m, pressed); err != nil {
				return false, err
			}
		}
	}

	// Motion is reported per cell, not per pixel: the wire format has no room
	// for anything finer, and a program in any-event mode would otherwise get a
	// report for every frame the pointer drifts inside one cell.
	if col, row := tab.grid().cellAt(px, py); col != tab.reports.mouseCol || row != tab.reports.mouseRow {
		tab.reports.mouseCol, tab.reports.mouseRow = col, row
		button, held := input.MouseButtonUnknown, false
		for _, b := range mouseButtons {
			if ebiten.IsMouseButtonPressed(b.ebiten) {
				button, held = b.vt, true
				break
			}
		}
		if err := tab.encodeMouse(input.MouseActionMotion, button, held, px, py, m, pressed); err != nil {
			return false, err
		}
	}

	if !pressed {
		tab.reports.mouseGrabbed = false
	}
	if len(tab.report) == 0 {
		return tab.reports.mouseGrabbed && pressed, nil
	}
	return true, tab.report.flush(tab.shell.Pty)
}

// How many lines one notch of the wheel moves, when this window is the one
// doing the moving.
const linesPerNotch = 3

// handleWheel decides who the wheel belongs to.
//
// Three answers, in order: the program, if it has asked for the mouse; the
// alternate screen, which has no scrollback, so a pager gets the arrow keys it
// would have got from the keyboard; and otherwise the scrollback itself.
//
// Ebitengine reports a continuous offset rather than notches, so it is
// accumulated: a trackpad that reports a tenth of a line at a time still ends
// up scrolling.
func (tab *terminalTab) handleWheel(m keys.Mods) error {
	_, dy := ebiten.Wheel()
	tab.reports.wheel += dy
	notches := int(tab.reports.wheel) // truncates toward zero, so the remainder is kept
	if notches == 0 {
		return nil
	}
	tab.reports.wheel -= float64(notches)

	// Shift takes the wheel back for the window, the same way it takes back a
	// drag: it is the only way to reach the scrollback of a program that has
	// grabbed the mouse.
	if !m.Shift {
		taken, err := tab.reportWheel(notches, m)
		if err != nil || taken {
			return err
		}
	}

	screen, err := tab.vt.ActiveScreenKey()
	if err != nil {
		return err
	}
	if screen == gostty.ScreenKeyAlternate {
		return tab.wheelAsArrows(notches)
	}
	// Positive is up, and up the scrollback is a negative delta.
	return tab.vt.ScrollViewport(gostty.ScrollViewportDelta(-notches * linesPerNotch))
}

// reportWheel offers the wheel to the program as the button presses the
// protocol represents it with, and reports whether it took them.
func (tab *terminalTab) reportWheel(notches int, m keys.Mods) (bool, error) {
	button := input.MouseButtonFour // up
	if notches < 0 {
		button, notches = input.MouseButtonFive, -notches
	}
	px, py := tab.cursorPosition()

	tab.report.reset()
	for range notches {
		if err := tab.encodeMouse(input.MouseActionPress, button, true, px, py, m, false); err != nil {
			return false, err
		}
	}
	if len(tab.report) == 0 {
		return false, nil
	}
	return true, tab.report.flush(tab.shell.Pty)
}

// wheelAsArrows turns the wheel into arrow keys, which is what the alternate
// screen leaves: there is no scrollback to move through, and a full-screen
// program that has not asked for the mouse still understands Up and Down.
func (tab *terminalTab) wheelAsArrows(notches int) error {
	key := input.KeyArrowUp
	if notches < 0 {
		key, notches = input.KeyArrowDown, -notches
	}
	tab.out.reset()
	for range notches * linesPerNotch {
		// A press with no text: the wheel is standing in for the key, so what
		// goes out is what pressing it would have sent.
		if err := tab.sendKey(keys.Event{Key: key, Action: input.KeyActionPress}, keys.Mods{}); err != nil {
			return err
		}
	}
	return tab.out.flush(tab.shell.Pty)
}

// encodeMouse describes one event to the binding and appends whatever it
// encodes -- which may be nothing -- to this frame's report.
func (tab *terminalTab) encodeMouse(action input.MouseAction, button input.MouseButton, hasButton bool, px, py int, m keys.Mods, anyPressed bool) error {
	ev := input.MouseEvent{
		Action:    action,
		Button:    button,
		HasButton: hasButton,
		Mods:      m.KeyMods(),
		X:         float32(px),
		Y:         float32(py),
	}
	return input.EncodeMouse(&tab.report, tab.vt, ev, tab.grid().renderSize(), anyPressed)
}

// reportFocus tells the program the window gained or lost focus, for the
// programs that asked (DECSET 1004). EncodeFocus only formats the event;
// checking whether the program subscribed is the tab's responsibility.
func (tab *terminalTab) reportFocus(focused bool) error {
	if focused != tab.reports.focused {
		tab.reports.focused = focused
		if !focused {
			tab.reports.focusedFrames = 0
			tab.endGesture()
			tab.reports.mouseGrabbed = false
		}
		enabled, err := tab.vt.ModeEnabled(gostty.ModeFocusEvent)
		if err != nil {
			return err
		}
		if enabled {
			event := input.FocusEventLost
			if focused {
				event = input.FocusEventGained
			}
			tab.report.reset()
			if err := input.EncodeFocus(&tab.report, event); err != nil {
				return err
			}
			if err := tab.report.flush(tab.shell.Pty); err != nil {
				return err
			}
		}
	}
	if tab.reports.focused {
		tab.reports.focusedFrames++
	}
	return nil
}
