package main

import (
	"io"

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

// pasteText hands text to the program as a paste.
//
// Bracketed paste and the safety check both come from the binding:
// `IsSafePaste` is what refuses text containing a newline when the program has
// not asked for bracketed paste, where it would run as typed commands rather
// than arrive as data.
func (tab *terminalTab) pasteText(text string) error {
	if text == "" || !input.IsSafePaste([]byte(text)) {
		return nil
	}
	return input.EncodePaste(tab.shell.Pty, tab.vt, []byte(text))
}

// sendKey describes one key event to the binding and appends whatever it
// encodes to this frame's output.
//
// The whole event is described, not only the key and the modifiers. Under the
// Kitty keyboard protocol a program is told which key was pressed, what it
// would have typed unshifted, which modifiers went into the text, and whether
// this was a press, a repeat or a release -- and the encoder needs all of it:
// given a key with no unshifted codepoint it has nothing to name and falls
// back to the legacy bytes, which is the protocol quietly not working.
//
// None of it costs anything under the legacy encoding, which ignores the extra
// fields, drops releases entirely, and treats a repeat as a press.
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
