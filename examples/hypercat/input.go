package main

import (
	"io"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/ironpark/gostty"
	"github.com/ironpark/gostty/examples/hypercat/keys"
	"github.com/ironpark/gostty/input"
)

// frameBuffer collects what one frame's encoders write before it goes to the
// pty. EncodeKey and EncodeMouse write at most a few bytes each, and a
// bytes.Buffer per event is more machinery than that deserves; one of these
// lives on the tab so nothing is allocated per key or per report.
//
// Every path that encodes into one truncates it first rather than trusting the
// last one to have emptied it. That is also what discards a partial encode left
// behind by an error.
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

// handleInput turns this frame's key presses into bytes for the pty.
//
// The encoding is not ours to invent: what Ctrl+C or an arrow key means on the
// wire depends on modes the running program has set (DECCKM, the Kitty keyboard
// protocol, bracketed paste). `input.EncodeKey` reads those modes off the
// terminal, so this only has to say which key was pressed. Which keys those are
// is the `keys` package's problem.
func (tab *terminalTab) handleInput(m keys.Mods) error {
	tab.out.reset()

	// Copy and paste are the two bindings this emulator keeps for itself.
	// Ctrl+Shift+C/V, or Cmd+C/V where that is the convention.
	if (m.Ctrl && m.Shift) || m.Super {
		switch {
		case inpututil.IsKeyJustPressed(ebiten.KeyC):
			return tab.copySelection()
		case inpututil.IsKeyJustPressed(ebiten.KeyV):
			// Bracketed paste and the safety check both come from the binding:
			// `IsSafePaste` is what refuses a paste containing a newline when
			// the program has not asked for bracketed paste.
			text := tab.pasteText()
			if len(text) > 0 && input.IsSafePaste(text) {
				return input.EncodePaste(tab.shell.pty, tab.vt, text)
			}
			return nil
		}
	}

	for _, ev := range tab.keys.Frame(m, tab.reports.focusedFrames) {
		if err := tab.sendKey(ev.Key, ev.Text, m); err != nil {
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
	return tab.out.flush(tab.shell.pty)
}

// sendKey describes one key press to the binding and appends whatever it
// encodes to this frame's output.
func (tab *terminalTab) sendKey(key input.Key, text []byte, m keys.Mods) error {
	ev := input.KeyEvent{Action: input.KeyActionPress, Key: key, Mods: m.KeyMods()}
	return input.EncodeKey(&tab.out, tab.vt, ev, string(text))
}
