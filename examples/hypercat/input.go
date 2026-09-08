package main

import (
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/ironpark/gostty"
	"github.com/ironpark/gostty/examples/hypercat/keys"
	"github.com/ironpark/gostty/input"
)

// handleInput turns this frame's key presses into bytes for the pty.
//
// The encoding is not ours to invent: what Ctrl+C or an arrow key means on the
// wire depends on modes the running program has set (DECCKM, the Kitty keyboard
// protocol, bracketed paste). `input.EncodeKey` reads those modes off the
// terminal, so this only has to say which key was pressed. Which keys those are
// is the `keys` package's problem.
func (g *game) handleInput(m keys.Mods) error {
	g.out = g.out[:0]

	// Copy and paste are the two bindings this emulator keeps for itself.
	// Ctrl+Shift+C/V, or Cmd+C/V where that is the convention.
	if (m.Ctrl && m.Shift) || m.Super {
		switch {
		case inpututil.IsKeyJustPressed(ebiten.KeyC):
			return g.copySelection()
		case inpututil.IsKeyJustPressed(ebiten.KeyV):
			// Bracketed paste and the safety check both come from the binding:
			// `IsSafePaste` is what refuses a paste containing a newline when
			// the program has not asked for bracketed paste.
			text := g.pasteText()
			if len(text) > 0 && input.IsSafePaste(text) {
				return input.EncodePaste(g.ptmx, g.vt, text)
			}
			return nil
		}
	}

	for _, ev := range g.keys.Frame(m, g.focusedFrames) {
		if err := g.sendKey(ev.Key, ev.Text, m); err != nil {
			return err
		}
	}
	if len(g.out) == 0 {
		return nil
	}
	// Typing means you want to see what you are typing, so anything that goes
	// to the program snaps the viewport back to the bottom. Every terminal does
	// this; the program has no idea the view had moved.
	if err := g.vt.ScrollViewport(gostty.ScrollViewportBottom()); err != nil {
		return err
	}
	return g.flushKeys()
}

// flushKeys writes whatever the key encoders appended to the frame's buffer.
//
// The buffer is shared by every path that encodes keys -- typing, and the wheel
// turning into arrow keys on the alternate screen -- so each one truncates it
// before it starts rather than trusting the last one to have emptied it. That
// is also what discards a partial encode left behind by an error.
func (g *game) flushKeys() error {
	if len(g.out) == 0 {
		return nil
	}
	_, err := g.ptmx.Write(g.out)
	return err
}

// sendKey describes one key press to the binding and appends whatever it
// encodes to this frame's output.
func (g *game) sendKey(key input.Key, text []byte, m keys.Mods) error {
	ev := input.KeyEvent{Action: input.KeyActionPress, Key: key, Mods: m.KeyMods()}
	return input.EncodeKey(g.enc, g.vt, ev, string(text))
}

// outputWriter appends to the frame's output buffer. EncodeKey writes at most a
// few bytes and a bytes.Buffer per keystroke is more machinery than that
// deserves; one of these lives on the game so nothing is allocated per key.
type outputWriter struct{ g *game }

func (w outputWriter) Write(p []byte) (int, error) {
	w.g.out = append(w.g.out, p...)
	return len(p), nil
}
