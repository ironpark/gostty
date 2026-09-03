package main

import (
	"unicode/utf8"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/ironpark/gostty"
	"github.com/ironpark/gostty/input"
)

// Key repeat, in ticks, since Ebitengine reports how long a key has been held
// rather than repeats. The default tick rate is 60Hz, so this is 0.4s then 30
// times a second.
const (
	repeatDelayTicks    = 24
	repeatIntervalTicks = 2
)

// handleInput turns this frame's keyboard state into bytes for the pty.
//
// The encoding is not ours to invent: what Ctrl+C or an arrow key means on the
// wire depends on modes the running program has set (DECCKM, the Kitty keyboard
// protocol, bracketed paste). `input.EncodeKey` reads those modes off the
// terminal, so this only has to describe which key was pressed.
func (g *game) handleInput(m mods) error {
	g.out = g.out[:0]

	// Copy and paste are the two bindings this emulator keeps for itself.
	// Ctrl+Shift+C/V, or Cmd+C/V where that is the convention.
	if (m.ctrl && m.shift) || m.super {
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

	// Text first: a rune the platform produced already accounts for the layout,
	// dead keys and IME. Only when a modifier makes it text-less do we fall
	// back to describing the physical key.
	if !m.ctrl && !m.alt && !m.super {
		g.chars = ebiten.AppendInputChars(g.chars[:0])
		for _, r := range g.chars {
			g.utf8 = utf8.AppendRune(g.utf8[:0], r)
			if err := g.sendKey(input.KeyUnidentified, g.utf8, m); err != nil {
				return err
			}
		}
	}

	g.pressed = inpututil.AppendPressedKeys(g.pressed[:0])
	for _, key := range g.pressed {
		held := inpututil.KeyPressDuration(key)
		if code, ok := keyMap[key]; ok {
			if repeating(held) {
				if err := g.sendKey(code, nil, m); err != nil {
					return err
				}
			}
			continue
		}
		// Letters and digits produce text on their own, so they are only
		// described as keys when a modifier swallowed the text. They do not
		// repeat: Ctrl+C held down should interrupt once.
		if held == 1 && (m.ctrl || m.alt || m.super) {
			if code, ok := modifiedMap[key]; ok {
				if err := g.sendKey(code, nil, m); err != nil {
					return err
				}
			}
		}
	}

	if len(g.out) > 0 {
		// Typing means you want to see what you are typing, so anything that
		// goes to the program snaps the viewport back to the bottom. Every
		// terminal does this; the program has no idea the view had moved.
		if err := g.vt.ScrollViewport(gostty.ScrollViewportBottom()); err != nil {
			return err
		}
		if _, err := g.ptmx.Write(g.out); err != nil {
			return err
		}
	}
	return nil
}

// repeating reports whether a key held for this many ticks should fire now.
func repeating(held int) bool {
	if held == 1 {
		return true
	}
	if held <= repeatDelayTicks {
		return false
	}
	return (held-repeatDelayTicks)%repeatIntervalTicks == 0
}

type mods struct{ shift, ctrl, alt, super bool }

func currentMods() mods {
	return mods{
		shift: ebiten.IsKeyPressed(ebiten.KeyShift),
		ctrl:  ebiten.IsKeyPressed(ebiten.KeyControl),
		alt:   ebiten.IsKeyPressed(ebiten.KeyAlt),
		super: ebiten.IsKeyPressed(ebiten.KeyMeta),
	}
}

// sendKey describes one key press to the binding and appends whatever it
// encodes to this frame's output.
func (g *game) sendKey(key input.Key, text []byte, m mods) error {
	if err := g.ev.Reset(); err != nil {
		return err
	}
	if err := g.ev.SetAction(input.KeyActionPress); err != nil {
		return err
	}
	if err := g.ev.SetKey(key); err != nil {
		return err
	}
	if len(text) > 0 {
		if err := g.ev.SetUTF8(text); err != nil {
			return err
		}
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
			if err := g.ev.SetMod(set.mod, true); err != nil {
				return err
			}
		}
	}
	return input.EncodeKey(g.enc, g.vt, g.ev)
}

// outputWriter appends to the frame's output buffer. EncodeKey writes at most a
// few bytes and a bytes.Buffer per keystroke is more machinery than that
// deserves; one of these lives on the game so nothing is allocated per key.
type outputWriter struct{ g *game }

func (w outputWriter) Write(p []byte) (int, error) {
	w.g.out = append(w.g.out, p...)
	return len(p), nil
}

// Keys that do not produce text, and so must be described to the encoder.
var keyMap = map[ebiten.Key]input.Key{
	ebiten.KeyArrowUp:    input.KeyArrowUp,
	ebiten.KeyArrowDown:  input.KeyArrowDown,
	ebiten.KeyArrowLeft:  input.KeyArrowLeft,
	ebiten.KeyArrowRight: input.KeyArrowRight,
	ebiten.KeyEnter:      input.KeyEnter,
	ebiten.KeyBackspace:  input.KeyBackspace,
	ebiten.KeyTab:        input.KeyTab,
	ebiten.KeyEscape:     input.KeyEscape,
	ebiten.KeyDelete:     input.KeyDelete,
	ebiten.KeyInsert:     input.KeyInsert,
	ebiten.KeyHome:       input.KeyHome,
	ebiten.KeyEnd:        input.KeyEnd,
	ebiten.KeyPageUp:     input.KeyPageUp,
	ebiten.KeyPageDown:   input.KeyPageDown,
	ebiten.KeyF1:         input.KeyF1,
	ebiten.KeyF2:         input.KeyF2,
	ebiten.KeyF3:         input.KeyF3,
	ebiten.KeyF4:         input.KeyF4,
	ebiten.KeyF5:         input.KeyF5,
	ebiten.KeyF6:         input.KeyF6,
	ebiten.KeyF7:         input.KeyF7,
	ebiten.KeyF8:         input.KeyF8,
	ebiten.KeyF9:         input.KeyF9,
	ebiten.KeyF10:        input.KeyF10,
	ebiten.KeyF11:        input.KeyF11,
	ebiten.KeyF12:        input.KeyF12,
}

// Keys that normally produce text, described only when a modifier is held.
var modifiedMap = map[ebiten.Key]input.Key{
	ebiten.KeyA: input.KeyKeyA, ebiten.KeyB: input.KeyKeyB, ebiten.KeyC: input.KeyKeyC,
	ebiten.KeyD: input.KeyKeyD, ebiten.KeyE: input.KeyKeyE, ebiten.KeyF: input.KeyKeyF,
	ebiten.KeyG: input.KeyKeyG, ebiten.KeyH: input.KeyKeyH, ebiten.KeyI: input.KeyKeyI,
	ebiten.KeyJ: input.KeyKeyJ, ebiten.KeyK: input.KeyKeyK, ebiten.KeyL: input.KeyKeyL,
	ebiten.KeyM: input.KeyKeyM, ebiten.KeyN: input.KeyKeyN, ebiten.KeyO: input.KeyKeyO,
	ebiten.KeyP: input.KeyKeyP, ebiten.KeyQ: input.KeyKeyQ, ebiten.KeyR: input.KeyKeyR,
	ebiten.KeyS: input.KeyKeyS, ebiten.KeyT: input.KeyKeyT, ebiten.KeyU: input.KeyKeyU,
	ebiten.KeyV: input.KeyKeyV, ebiten.KeyW: input.KeyKeyW, ebiten.KeyX: input.KeyKeyX,
	ebiten.KeyY: input.KeyKeyY, ebiten.KeyZ: input.KeyKeyZ,
	ebiten.KeyDigit0: input.KeyDigit0, ebiten.KeyDigit1: input.KeyDigit1,
	ebiten.KeyDigit2: input.KeyDigit2, ebiten.KeyDigit3: input.KeyDigit3,
	ebiten.KeyDigit4: input.KeyDigit4, ebiten.KeyDigit5: input.KeyDigit5,
	ebiten.KeyDigit6: input.KeyDigit6, ebiten.KeyDigit7: input.KeyDigit7,
	ebiten.KeyDigit8: input.KeyDigit8, ebiten.KeyDigit9: input.KeyDigit9,
	ebiten.KeySpace: input.KeySpace, ebiten.KeyMinus: input.KeyMinus,
	ebiten.KeyEqual: input.KeyEqual, ebiten.KeySlash: input.KeySlash,
	ebiten.KeyBackslash: input.KeyBackslash, ebiten.KeyComma: input.KeyComma,
	ebiten.KeyPeriod: input.KeyPeriod, ebiten.KeySemicolon: input.KeySemicolon,
	ebiten.KeyQuote: input.KeyQuote, ebiten.KeyBackquote: input.KeyBackquote,
	ebiten.KeyBracketLeft: input.KeyBracketLeft, ebiten.KeyBracketRight: input.KeyBracketRight,
}
