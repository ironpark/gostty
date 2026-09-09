package main

import (
	"io"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/ironpark/gostty"
	"github.com/ironpark/gostty/examples/hypercat/keys"
	"github.com/ironpark/gostty/input"
)

// handleInput consumes host shortcuts, encodes the remaining keys, and writes
// one batch to the PTY. gostty reads terminal modes to choose the wire format.
func (tab *terminalTab) handleInput(m keys.Mods) error {
	tab.out.reset()

	for _, ev := range tab.keys.Frame(m, tab.reports.focusedFrames) {
		// The window's own bindings are taken out of the same stream of events
		// the program is fed from, rather than polled beside it: one reader
		// means one set of rules about what counts as a press, and a binding
		// takes its own key and leaves the rest of the frame alone.
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
	// Typing means you want to see what you are typing, so anything that goes
	// to the program snaps the viewport back to the bottom. Every terminal does
	// this; the program has no idea the view had moved.
	if err := tab.vt.ScrollViewport(gostty.ScrollViewportBottom()); err != nil {
		return err
	}
	return tab.out.flush(tab.shell.Pty)
}

// shortcut runs the window's own binding for a key, and reports whether there
// was one. What is bound here never reaches the program.
func (tab *terminalTab) shortcut(key input.Key) (bool, error) {
	switch key {
	case input.KeyKeyC:
		return true, tab.copySelection()
	case input.KeyKeyV:
		return true, tab.pasteText(string(tab.clipboard.paste()))
	case input.KeyKeyA:
		return true, tab.selectAll()
	case input.KeyKeyS:
		return true, tab.exportScrollback()
	}
	return tab.adjustSelection(key)
}

// pasteText rejects unsafe control bytes and lets gostty encode bracketed paste
// according to the running program's terminal modes.
func (tab *terminalTab) pasteText(text string) error {
	if text == "" || !input.IsSafePaste([]byte(text)) {
		return nil
	}
	return input.EncodePaste(tab.shell.Pty, tab.vt, []byte(text))
}

// sendKey supplies the full event for the Kitty keyboard protocol, including
// repeats, releases, consumed modifiers, and the unshifted codepoint.
// The legacy encoder ignores fields it does not need.
func (tab *terminalTab) sendKey(ev keys.Event, m keys.Mods) error {
	unshifted, _ := ev.Key.Codepoint()
	key := input.KeyEvent{
		Action:             ev.Action,
		Key:                ev.Key,
		Mods:               m.KeyMods(),
		ConsumedMods:       m.Consumed(len(ev.Text) > 0),
		UnshiftedCodepoint: unshifted,
		// Composing would be set while an IME has a preedit open, so that a
		// keystroke going into the composition is not also sent to the
		// program. Ebitengine does not report one, which is the same reason
		// this example draws no preedit.
	}
	return input.EncodeKey(&tab.out, tab.vt, key, string(ev.Text))
}

// updateInput routes input only; the app refreshes the viewport afterwards.
// An open panel consumes the keyboard while selection remains available.
func (tab *terminalTab) updateInput(m keys.Mods, panelTook bool) error {
	if !tab.reports.focused {
		return nil
	}
	if err := tab.handlePointer(m); err != nil {
		return err
	}
	if panelTook {
		return nil
	}
	return tab.handleInput(m)
}

// handlePointer gives the tab bar and cat first refusal on pointer input.
func (tab *terminalTab) handlePointer(m keys.Mods) error {
	_, y := tab.cursorPosition()
	if y < 0 {
		if !ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft) {
			tab.sel.dragging = false
		}
		return nil
	}
	if !tab.pokeCat() {
		if err := tab.handleMouse(m); err != nil {
			return err
		}
	}
	if err := tab.handleWheel(m); err != nil {
		return err
	}
	return tab.handleDrop()
}

// frameBuffer batches encoder output without allocating a buffer per event.
// Reset before every encode to discard any bytes left by a previous error.
type frameBuffer []byte

func (b *frameBuffer) Write(p []byte) (int, error) {
	*b = append(*b, p...)
	return len(p), nil
}

func (b *frameBuffer) reset() { *b = (*b)[:0] }

// flush writes the buffer to the pty, if anything was encoded into it.
func (b *frameBuffer) flush(w io.Writer) error {
	if len(*b) == 0 {
		return nil
	}
	_, err := w.Write(*b)
	return err
}
