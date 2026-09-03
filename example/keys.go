package main

import (
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/ironpark/gostty/input"
)

// Key repeat, since Ebitengine reports state rather than repeats.
const (
	repeatDelay    = 400 * time.Millisecond
	repeatInterval = 33 * time.Millisecond
)

type keyState struct {
	held map[ebiten.Key]time.Time
	next map[ebiten.Key]time.Time
}

// handleInput turns this frame's keyboard state into bytes for the pty.
//
// The encoding is not ours to invent: what Ctrl+C or an arrow key means on the
// wire depends on modes the running program has set (DECCKM, the Kitty keyboard
// protocol, bracketed paste). `input.EncodeKey` reads those modes off the
// terminal, so this only has to describe which key was pressed.
func (g *game) handleInput() error {
	if g.keys.held == nil {
		g.keys.held = map[ebiten.Key]time.Time{}
		g.keys.next = map[ebiten.Key]time.Time{}
	}
	now := time.Now()
	mods := currentMods()
	g.out = g.out[:0]

	// Copy and paste are the two bindings this emulator keeps for itself.
	// Ctrl+Shift+C/V, or Cmd+C/V where that is the convention.
	if (mods.ctrl && mods.shift) || mods.super {
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
	if !mods.ctrl && !mods.alt && !mods.super {
		for _, r := range ebiten.AppendInputChars(nil) {
			if err := g.sendKey(input.KeyUnidentified, string(r), mods); err != nil {
				return err
			}
		}
	}

	for key, code := range keyMap {
		switch {
		case inpututil.IsKeyJustPressed(key):
			g.keys.held[key] = now
			g.keys.next[key] = now.Add(repeatDelay)
		case !ebiten.IsKeyPressed(key):
			delete(g.keys.held, key)
			continue
		case now.Before(g.keys.next[key]):
			continue
		default:
			g.keys.next[key] = now.Add(repeatInterval)
		}
		if err := g.sendKey(code, "", mods); err != nil {
			return err
		}
	}

	// Letters and digits produce text on their own, so they are only described
	// as keys when a modifier swallowed the text.
	if mods.ctrl || mods.alt || mods.super {
		for key, code := range modifiedMap {
			if inpututil.IsKeyJustPressed(key) {
				if err := g.sendKey(code, "", mods); err != nil {
					return err
				}
			}
		}
	}

	if len(g.out) > 0 {
		if _, err := g.ptmx.Write(g.out); err != nil {
			return err
		}
	}
	return nil
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
func (g *game) sendKey(key input.Key, text string, m mods) error {
	if err := g.ev.Reset(); err != nil {
		return err
	}
	if err := g.ev.SetAction(input.KeyActionPress); err != nil {
		return err
	}
	if err := g.ev.SetKey(key); err != nil {
		return err
	}
	if text != "" {
		if err := g.ev.SetUTF8([]byte(text)); err != nil {
			return err
		}
	}
	for mod, on := range map[input.KeyMod]bool{
		input.KeyModShift: m.shift,
		input.KeyModCtrl:  m.ctrl,
		input.KeyModAlt:   m.alt,
		input.KeyModSuper: m.super,
	} {
		if on {
			if err := g.ev.SetMod(mod, true); err != nil {
				return err
			}
		}
	}

	var buf appender
	if err := input.EncodeKey(&buf, g.vt, g.ev); err != nil {
		return err
	}
	g.out = append(g.out, buf...)
	return nil
}

// appender is an io.Writer over a byte slice; EncodeKey writes at most a few
// bytes and a bytes.Buffer per keystroke is more machinery than that deserves.
type appender []byte

func (a *appender) Write(p []byte) (int, error) {
	*a = append(*a, p...)
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
