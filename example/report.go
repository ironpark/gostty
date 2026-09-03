package main

import (
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/ironpark/gostty"
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
func (g *game) reportMouse(m mods) (bool, error) {
	if m.shift {
		g.mouseGrabbed = false
		return false, nil
	}

	px, py := ebiten.CursorPosition()
	pressed := false
	for _, b := range mouseButtons {
		if ebiten.IsMouseButtonPressed(b.ebiten) {
			pressed = true
		}
	}

	g.report = g.report[:0]
	for _, b := range mouseButtons {
		switch {
		case inpututil.IsMouseButtonJustPressed(b.ebiten):
			if err := g.encodeMouse(input.MouseActionPress, b.vt, true, px, py, m, true); err != nil {
				return false, err
			}
			// A press is what settles who owns the drag that follows: a
			// program in X10 mode reports the press and nothing else, and the
			// release still has to go to it rather than end a selection.
			g.mouseGrabbed = len(g.report) > 0
		case inpututil.IsMouseButtonJustReleased(b.ebiten):
			if err := g.encodeMouse(input.MouseActionRelease, b.vt, true, px, py, m, pressed); err != nil {
				return false, err
			}
		}
	}

	// Motion is reported per cell, not per pixel: the wire format has no room
	// for anything finer, and a program in any-event mode would otherwise get a
	// report for every frame the pointer drifts inside one cell.
	if col, row := g.cellAt(px, py); col != g.mouseCol || row != g.mouseRow {
		g.mouseCol, g.mouseRow = col, row
		button, held := input.MouseButtonUnknown, false
		for _, b := range mouseButtons {
			if ebiten.IsMouseButtonPressed(b.ebiten) {
				button, held = b.vt, true
				break
			}
		}
		if err := g.encodeMouse(input.MouseActionMotion, button, held, px, py, m, pressed); err != nil {
			return false, err
		}
	}

	if !pressed {
		g.mouseGrabbed = false
	}
	if len(g.report) == 0 {
		return g.mouseGrabbed && pressed, nil
	}
	if _, err := g.ptmx.Write(g.report); err != nil {
		return false, err
	}
	return true, nil
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
func (g *game) handleWheel(m mods) error {
	_, dy := ebiten.Wheel()
	g.wheel += dy
	notches := int(g.wheel) // truncates toward zero, so the remainder is kept
	if notches == 0 {
		return nil
	}
	g.wheel -= float64(notches)

	// Shift takes the wheel back for the window, the same way it takes back a
	// drag: it is the only way to reach the scrollback of a program that has
	// grabbed the mouse.
	if !m.shift {
		taken, err := g.reportWheel(notches, m)
		if err != nil || taken {
			return err
		}
	}

	screen, err := g.vt.ActiveScreenKey()
	if err != nil {
		return err
	}
	if screen == gostty.ScreenKeyAlternate {
		return g.wheelAsArrows(notches)
	}
	// Positive is up, and up the scrollback is a negative delta.
	return g.vt.ScrollViewport(gostty.ScrollViewportDelta(-notches * linesPerNotch))
}

// reportWheel offers the wheel to the program as the button presses the
// protocol represents it with, and reports whether it took them.
func (g *game) reportWheel(notches int, m mods) (bool, error) {
	button := input.MouseButtonFour // up
	if notches < 0 {
		button, notches = input.MouseButtonFive, -notches
	}
	px, py := ebiten.CursorPosition()

	g.report = g.report[:0]
	for range notches {
		if err := g.encodeMouse(input.MouseActionPress, button, true, px, py, m, false); err != nil {
			return false, err
		}
	}
	if len(g.report) == 0 {
		return false, nil
	}
	_, err := g.ptmx.Write(g.report)
	return true, err
}

// wheelAsArrows turns the wheel into arrow keys, which is what the alternate
// screen leaves: there is no scrollback to move through, and a full-screen
// program that has not asked for the mouse still understands Up and Down.
func (g *game) wheelAsArrows(notches int) error {
	key := input.KeyArrowUp
	if notches < 0 {
		key, notches = input.KeyArrowDown, -notches
	}
	g.out = g.out[:0]
	for range notches * linesPerNotch {
		if err := g.sendKey(key, nil, mods{}); err != nil {
			return err
		}
	}
	if len(g.out) == 0 {
		return nil
	}
	// Written here rather than left for handleInput, which clears the buffer
	// before it starts.
	_, err := g.ptmx.Write(g.out)
	g.out = g.out[:0]
	return err
}

// encodeMouse describes one event to the binding and appends whatever it
// encodes -- which may be nothing -- to this frame's report.
func (g *game) encodeMouse(action input.MouseAction, button input.MouseButton, hasButton bool, px, py int, m mods, anyPressed bool) error {
	if err := g.mev.Reset(); err != nil {
		return err
	}
	if err := g.mev.SetAction(action); err != nil {
		return err
	}
	if hasButton {
		if err := g.mev.SetButton(button); err != nil {
			return err
		}
	} else if err := g.mev.ClearButton(); err != nil {
		return err
	}
	for _, set := range [...]struct {
		mod input.KeyMod
		on  bool
	}{
		{input.KeyModShift, m.shift},
		{input.KeyModCtrl, m.ctrl},
		{input.KeyModAlt, m.alt},
		{input.KeyModSuper, m.super},
	} {
		if set.on {
			if err := g.mev.SetMod(set.mod, true); err != nil {
				return err
			}
		}
	}
	if err := g.mev.SetPosition(float32(px), float32(py)); err != nil {
		return err
	}
	return input.EncodeMouse(reportWriter{g: g}, g.vt, g.mev, g.renderSize(), anyPressed)
}

// renderSize describes the window to the encoder, which needs it to turn a
// pixel position into a cell. There is no padding around the grid here, so the
// only interesting fields are the cell size.
func (g *game) renderSize() input.RenderSize {
	return input.RenderSize{
		ScreenWidth:  uint32(float64(g.cols) * g.fonts.cellW),
		ScreenHeight: uint32(float64(g.rows) * g.fonts.cellH),
		CellWidth:    uint32(g.fonts.cellW),
		CellHeight:   uint32(g.fonts.cellH),
	}
}

// reportFocus tells the program the window gained or lost focus, for the
// programs that asked (DECSET 1004). Like the mouse, the encoder writes nothing
// when they did not.
func (g *game) reportFocus() error {
	focused := ebiten.IsFocused()
	if focused == g.focused {
		return nil
	}
	g.focused = focused
	event := input.FocusEventLost
	if focused {
		event = input.FocusEventGained
	}
	g.report = g.report[:0]
	if err := input.EncodeFocus(reportWriter{g: g}, event); err != nil {
		return err
	}
	if len(g.report) == 0 {
		return nil
	}
	_, err := g.ptmx.Write(g.report)
	return err
}

// reportWriter appends to the frame's report buffer, so an event that the
// program did not ask for costs nothing but a call.
type reportWriter struct{ g *game }

func (w reportWriter) Write(p []byte) (int, error) {
	w.g.report = append(w.g.report, p...)
	return len(p), nil
}
