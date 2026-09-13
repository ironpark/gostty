package main

import (
	"io"
	"io/fs"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/ironpark/gostty"
	"github.com/ironpark/gostty/examples/hypercat/keys"
	"github.com/ironpark/gostty/examples/hypercat/ui"
	"github.com/ironpark/gostty/input"
)

// What the host reports ---------------------------------------------------------

// hostInput is one update's worth of platform state. The tests build these by
// hand, so nothing here is an Ebitengine type.
//
// Key events are read lazily through ReadKeys: tab routing first decides how
// long the terminal has had focus and whether a panel owns typing. The buffer
// it returns is borrowed until the next update.
type hostInput struct {
	Mods                keys.Mods
	Focused             bool
	X, Y                int // pointer, in device pixels from the window's origin
	Left, Right, Middle button
	Wheel               float64
	Dropped             fs.FS
	DeltaSeconds        float64
	PressedKeys         map[input.Key]bool // host shortcut keys pressed this frame
	Panel               ui.Input
	ReadKeys            func(focusedFrames int) []keys.Event
}

type button struct{ Down, Pressed, Released bool }

func (in hostInput) KeyPressed(key input.Key) bool { return in.PressedKeys[key] }

func (in hostInput) KeyEvents(focusedFrames int) []keys.Event {
	if in.ReadKeys == nil {
		return nil
	}
	return in.ReadKeys(focusedFrames)
}

// inputPoller reads Ebitengine once per update.
type inputPoller struct {
	keyboard keys.Reader
	chars    []rune
	pressed  map[input.Key]bool
	mods     keys.Mods
}

// hostKeys are the window's shortcuts and panel navigation, polled directly.
// Terminal typing goes through keys.Reader, which keeps whole Kitty keyboard
// events and IME text.
var hostKeys = map[input.Key]ebiten.Key{
	input.KeyKeyT: ebiten.KeyT, input.KeyKeyW: ebiten.KeyW, input.KeyTab: ebiten.KeyTab,
	input.KeyDigit1: ebiten.KeyDigit1, input.KeyDigit2: ebiten.KeyDigit2, input.KeyDigit3: ebiten.KeyDigit3,
	input.KeyDigit4: ebiten.KeyDigit4, input.KeyDigit5: ebiten.KeyDigit5, input.KeyDigit6: ebiten.KeyDigit6,
	input.KeyDigit7: ebiten.KeyDigit7, input.KeyDigit8: ebiten.KeyDigit8, input.KeyDigit9: ebiten.KeyDigit9,
}

func (p *inputPoller) read() hostInput {
	p.mods = keys.Current()
	if p.pressed == nil {
		p.pressed = make(map[input.Key]bool)
	}
	clear(p.pressed)
	for key, native := range hostKeys {
		if inpututil.IsKeyJustPressed(native) {
			p.pressed[key] = true
		}
	}
	p.chars = ebiten.AppendInputChars(p.chars[:0])
	x, y := ebiten.CursorPosition()
	_, wheel := ebiten.Wheel()
	just := inpututil.IsKeyJustPressed
	mouse := func(b ebiten.MouseButton) button {
		return button{ebiten.IsMouseButtonPressed(b), inpututil.IsMouseButtonJustPressed(b), inpututil.IsMouseButtonJustReleased(b)}
	}
	return hostInput{
		Mods: p.mods, Focused: ebiten.IsFocused(), X: x, Y: y,
		Left: mouse(ebiten.MouseButtonLeft), Right: mouse(ebiten.MouseButtonRight), Middle: mouse(ebiten.MouseButtonMiddle),
		Wheel: wheel, Dropped: ebiten.DroppedFiles(), DeltaSeconds: 1.0 / float64(ebiten.TPS()),
		PressedKeys: p.pressed,
		ReadKeys:    func(focusedFrames int) []keys.Event { return p.keyboard.Frame(p.mods, focusedFrames) },
		Panel: ui.Input{
			OpenSearch:   p.mods.Shortcut() && just(ebiten.KeyF),
			OpenSettings: p.mods.Shortcut() && just(ebiten.KeyComma),
			Close:        just(ebiten.KeyEscape),
			Enter:        just(ebiten.KeyEnter), Shift: p.mods.Shift,
			Chars:     p.chars,
			Backspace: keys.Repeating(inpututil.KeyPressDuration(ebiten.KeyBackspace)),
			Up:        just(ebiten.KeyArrowUp), Down: just(ebiten.KeyArrowDown),
			Left:  just(ebiten.KeyArrowLeft) || just(ebiten.KeyMinus),
			Right: just(ebiten.KeyArrowRight) || just(ebiten.KeyEqual),
		},
	}
}

// What the terminal does with it ---------------------------------------------------

// updateInput routes one update's input to the program. An open panel owns
// the keyboard; the pointer stays available for selection.
func (tab *terminal) updateInput(m keys.Mods, panelTook, pointerConsumed bool) error {
	if !tab.reports.focused {
		return nil
	}
	if err := tab.handlePointer(m, pointerConsumed); err != nil {
		return err
	}
	if panelTook {
		return nil
	}
	return tab.handleKeys(m)
}

// handleKeys takes the window's own shortcuts out of the key stream, encodes
// the rest, and writes one batch to the pty. Which wire format -- legacy or
// Kitty -- is the encoder's choice, read from the terminal's modes.
func (tab *terminal) handleKeys(m keys.Mods) error {
	tab.out.reset()
	for _, ev := range tab.input.KeyEvents(tab.reports.focusedFrames) {
		if m.Shortcut() && ev.Action == input.KeyActionPress {
			taken, err := tab.shortcut(ev.Key)
			if err != nil {
				return err
			}
			if taken {
				continue
			}
		}
		if err := tab.sendKey(ev, m); err != nil {
			return err
		}
	}
	if len(tab.out) == 0 {
		return nil
	}
	// Typing snaps the viewport back to the bottom, as every terminal does.
	if err := tab.vt.ScrollViewport(gostty.ScrollViewportBottom()); err != nil {
		return err
	}
	return tab.out.flush(tab.shell.Pty)
}

// shortcut runs the window's binding for a key, and reports whether there was
// one. What is bound here never reaches the program.
func (tab *terminal) shortcut(key input.Key) (bool, error) {
	switch key {
	case input.KeyKeyC:
		return true, tab.copySelection()
	case input.KeyKeyV:
		return true, tab.pasteText(string(tab.clipboard.paste()))
	case input.KeyKeyA:
		return true, tab.selectAll()
	case input.KeyKeyS:
		tab.exportScrollback()
		return true, nil
	}
	return tab.adjustSelection(key)
}

// sendKey describes the whole event, which the Kitty keyboard protocol wants:
// repeats and releases, the modifiers the text consumed, and the unshifted
// codepoint. The legacy encoder ignores what it does not need.
func (tab *terminal) sendKey(ev keys.Event, m keys.Mods) error {
	unshifted, _ := ev.Key.Codepoint()
	key := input.KeyEvent{
		Action:             ev.Action,
		Key:                ev.Key,
		Mods:               m.KeyMods(),
		ConsumedMods:       m.Consumed(len(ev.Text) > 0),
		UnshiftedCodepoint: unshifted,
		// Composing would be set while an IME has a preedit open. Ebitengine
		// reports none, which is also why this example draws no preedit.
	}
	return input.EncodeKey(&tab.out, tab.vt, key, string(ev.Text))
}

// pasteText rejects unsafe control bytes and lets gostty bracket the paste
// according to the program's modes.
func (tab *terminal) pasteText(text string) error {
	if text == "" || !input.IsSafePaste([]byte(text)) {
		return nil
	}
	return input.EncodePaste(tab.shell.Pty, tab.vt, []byte(text))
}

// frameBuffer batches encoder output without allocating per event. Reset it
// before every batch so bytes left by an earlier error are discarded.
type frameBuffer []byte

func (b *frameBuffer) Write(p []byte) (int, error) { *b = append(*b, p...); return len(p), nil }
func (b *frameBuffer) reset()                      { *b = (*b)[:0] }

func (b *frameBuffer) flush(w io.Writer) error {
	if len(*b) == 0 {
		return nil
	}
	_, err := w.Write(*b)
	return err
}

// Mouse and focus ----------------------------------------------------------------
//
// Whether a mouse event is reported at all, and in which encoding, is a
// property of the modes the program set. None of that is decided here:
// input.EncodeMouse reads the modes and writes nothing when the program has
// not asked, so "did it write anything" is also "does the program want the
// mouse" -- which decides whether a drag belongs to the shell or to the
// selection.

// reportState is what the program has been told about the pointer and focus
// so far, which decides whether the next frame has anything new to say.
type reportState struct {
	mouseCol, mouseRow int     // the cell last reported: motion is per cell
	wheel              float64 // wheel movement not yet adding up to a notch
	mouseGrabbed       bool    // a press went to the program, so its release must too
	focused            bool
	// Frames spent focused, which tells a key held through the click that
	// focused the window from one pressed afterwards.
	focusedFrames int
}

// handlePointer handles the pointer once the window has had its turn.
func (tab *terminal) handlePointer(m keys.Mods, consumed bool) error {
	if _, y := tab.cursorPosition(); y < 0 {
		if !tab.input.Left.Down {
			tab.sel.dragging = false
		}
		return nil
	}
	if !consumed {
		if err := tab.handleMouse(m); err != nil {
			return err
		}
	}
	if err := tab.handleWheel(m); err != nil {
		return err
	}
	return tab.handleDrop()
}

// reportMouse offers this frame's mouse activity to the program and reports
// whether it took it. Shift takes the mouse back for the window, the
// convention every emulator follows: it is the only way to select text in a
// program that has grabbed the mouse.
func (tab *terminal) reportMouse(m keys.Mods) (bool, error) {
	if m.Shift {
		tab.reports.mouseGrabbed = false
		return false, nil
	}
	px, py := tab.cursorPosition()
	buttons := [...]struct {
		state button
		vt    input.MouseButton
	}{
		{tab.input.Left, input.MouseButtonLeft},
		{tab.input.Right, input.MouseButtonRight},
		{tab.input.Middle, input.MouseButtonMiddle},
	}
	pressed := false
	for _, b := range buttons {
		pressed = pressed || b.state.Down
	}

	tab.report.reset()
	for _, b := range buttons {
		switch {
		case b.state.Pressed:
			if err := tab.encodeMouse(input.MouseActionPress, b.vt, true, px, py, m, true); err != nil {
				return false, err
			}
			// The press settles who owns the drag that follows: a program in
			// X10 mode reports the press only, and the release still has to go
			// to it rather than end a selection.
			tab.reports.mouseGrabbed = len(tab.report) > 0
		case b.state.Released:
			if err := tab.encodeMouse(input.MouseActionRelease, b.vt, true, px, py, m, pressed); err != nil {
				return false, err
			}
		}
	}
	// Motion is reported per cell: the wire format has nothing finer.
	if col, row := tab.grid().cellAt(px, py); col != tab.reports.mouseCol || row != tab.reports.mouseRow {
		tab.reports.mouseCol, tab.reports.mouseRow = col, row
		held, hasButton := input.MouseButtonUnknown, false
		for _, b := range buttons {
			if b.state.Down {
				held, hasButton = b.vt, true
				break
			}
		}
		if err := tab.encodeMouse(input.MouseActionMotion, held, hasButton, px, py, m, pressed); err != nil {
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

// encodeMouse appends whatever the terminal's modes make of one event, which
// may be nothing, to this frame's report.
func (tab *terminal) encodeMouse(action input.MouseAction, btn input.MouseButton, hasButton bool, px, py int, m keys.Mods, anyPressed bool) error {
	ev := input.MouseEvent{
		Action: action, Button: btn, HasButton: hasButton,
		Mods: m.KeyMods(), X: float32(px), Y: float32(py),
	}
	return input.EncodeMouse(&tab.report, tab.vt, ev, tab.grid().renderSize(), anyPressed)
}

// How many lines one notch of the wheel moves when the window does the moving.
const linesPerNotch = 3

// handleWheel decides who the wheel belongs to: the program, if it asked for
// the mouse; the alternate screen, which has no scrollback, so a pager gets
// arrow keys; and otherwise the scrollback. Ebitengine reports a continuous
// offset, so it is accumulated into notches.
func (tab *terminal) handleWheel(m keys.Mods) error {
	tab.reports.wheel += tab.input.Wheel
	notches := int(tab.reports.wheel) // truncates, so the remainder is kept
	if notches == 0 {
		return nil
	}
	tab.reports.wheel -= float64(notches)
	if !m.Shift { // Shift takes the wheel back, as it takes back a drag
		taken, err := tab.reportWheel(notches, m)
		if err != nil || taken {
			return err
		}
	}
	if tab.vt.ActiveScreenKey() == gostty.ScreenKeyAlternate {
		return tab.wheelAsArrows(notches)
	}
	// Positive is up, and up the scrollback is a negative delta.
	return tab.vt.ScrollViewport(gostty.ScrollViewportDelta(-notches * linesPerNotch))
}

// reportWheel offers the wheel to the program as the button presses the
// protocol spells it with, and reports whether it took them.
func (tab *terminal) reportWheel(notches int, m keys.Mods) (bool, error) {
	btn := input.MouseButtonFour // up
	if notches < 0 {
		btn, notches = input.MouseButtonFive, -notches
	}
	px, py := tab.cursorPosition()
	tab.report.reset()
	for range notches {
		if err := tab.encodeMouse(input.MouseActionPress, btn, true, px, py, m, false); err != nil {
			return false, err
		}
	}
	if len(tab.report) == 0 {
		return false, nil
	}
	return true, tab.report.flush(tab.shell.Pty)
}

// wheelAsArrows sends what pressing Up or Down would have: on the alternate
// screen there is no scrollback, and a full-screen program still understands
// the arrows.
func (tab *terminal) wheelAsArrows(notches int) error {
	key := input.KeyArrowUp
	if notches < 0 {
		key, notches = input.KeyArrowDown, -notches
	}
	tab.out.reset()
	for range notches * linesPerNotch {
		if err := tab.sendKey(keys.Event{Key: key, Action: input.KeyActionPress}, keys.Mods{}); err != nil {
			return err
		}
	}
	return tab.out.flush(tab.shell.Pty)
}

// reportFocus tells the program the window gained or lost focus, if it asked
// (DECSET 1004). EncodeFocus only formats the event; checking the mode is the
// tab's job.
func (tab *terminal) reportFocus(focused bool) error {
	if focused != tab.reports.focused {
		tab.reports.focused = focused
		if !focused {
			tab.reports.focusedFrames = 0
			tab.reports.mouseGrabbed = false
			tab.endGesture()
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
