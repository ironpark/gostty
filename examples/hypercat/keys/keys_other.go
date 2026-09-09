//go:build !darwin

package keys

import "github.com/hajimehoshi/ebiten/v2"

// Current reports the modifier state for this frame.
func Current() Mods {
	return Mods{
		Shift: ebiten.IsKeyPressed(ebiten.KeyShift),
		Ctrl:  ebiten.IsKeyPressed(ebiten.KeyControl),
		Alt:   ebiten.IsKeyPressed(ebiten.KeyAlt),
		Super: ebiten.IsKeyPressed(ebiten.KeyMeta),
	}
}

// altProducesText is whether holding Alt makes the platform report different
// text rather than suppressing it. It is a macOS habit: Option+e is a dead
// key there and Alt+e is a text-less shortcut everywhere else.
const altProducesText = false

// PhysicallyPressed reports whether the key is really down. Only macOS needs
// to ask: see the darwin build of this file.
func PhysicallyPressed(key ebiten.Key) bool {
	return true
}
