//go:build darwin

package keys

import (
	"github.com/ebitengine/purego"
	"github.com/hajimehoshi/ebiten/v2"
)

var (
	cgEventSourceFlagsState func(int32) uint64
	cgEventSourceKeyState   func(int32, uint16) bool
)

func init() {
	cg, err := purego.Dlopen("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics", purego.RTLD_LAZY|purego.RTLD_GLOBAL)
	if err == nil {
		purego.RegisterLibFunc(&cgEventSourceFlagsState, cg, "CGEventSourceFlagsState")
		purego.RegisterLibFunc(&cgEventSourceKeyState, cg, "CGEventSourceKeyState")
	}
}

const (
	kCGEventFlagMaskShift     = 0x00020000
	kCGEventFlagMaskControl   = 0x00040000
	kCGEventFlagMaskAlternate = 0x00080000
	kCGEventFlagMaskCommand   = 0x00100000
)

// Current reports the modifier state for this frame.
func Current() Mods {
	if cgEventSourceFlagsState != nil {
		flags := cgEventSourceFlagsState(0) // kCGEventSourceStateCombinedSessionState
		return Mods{
			Shift: flags&kCGEventFlagMaskShift != 0,
			Ctrl:  flags&kCGEventFlagMaskControl != 0,
			Alt:   flags&kCGEventFlagMaskAlternate != 0,
			Super: flags&kCGEventFlagMaskCommand != 0,
		}
	}
	return Mods{
		Shift: ebiten.IsKeyPressed(ebiten.KeyShift),
		Ctrl:  ebiten.IsKeyPressed(ebiten.KeyControl),
		Alt:   ebiten.IsKeyPressed(ebiten.KeyAlt),
		Super: ebiten.IsKeyPressed(ebiten.KeyMeta),
	}
}

var darwinKeyCodes = map[ebiten.Key]uint16{
	ebiten.KeyArrowUp:     0x7E,
	ebiten.KeyArrowDown:   0x7D,
	ebiten.KeyArrowLeft:   0x7B,
	ebiten.KeyArrowRight:  0x7C,
	ebiten.KeyEnter:       0x24,
	ebiten.KeyNumpadEnter: 0x4C,
	ebiten.KeyBackspace:   0x33,
	ebiten.KeyTab:         0x30,
	ebiten.KeyEscape:      0x35,
	ebiten.KeyDelete:      0x75,
	ebiten.KeyInsert:      0x72,
	ebiten.KeyHome:        0x73,
	ebiten.KeyEnd:         0x77,
	ebiten.KeyPageUp:      0x74,
	ebiten.KeyPageDown:    0x79,
	ebiten.KeyF1:          0x7A,
	ebiten.KeyF2:          0x78,
	ebiten.KeyF3:          0x63,
	ebiten.KeyF4:          0x76,
	ebiten.KeyF5:          0x60,
	ebiten.KeyF6:          0x61,
	ebiten.KeyF7:          0x62,
	ebiten.KeyF8:          0x64,
	ebiten.KeyF9:          0x65,
	ebiten.KeyF10:         0x6D,
	ebiten.KeyF11:         0x67,
	ebiten.KeyF12:         0x6F,
}

// PhysicallyPressed reports whether the key is really down.
//
// Ebitengine keeps reporting a key as pressed after a Command chord releases
// it, because macOS does not deliver the key-up while Command is held. Asking
// CoreGraphics for the hardware state is what stops one Cmd+K from repeating
// for as long as the window has focus.
func PhysicallyPressed(key ebiten.Key) bool {
	if vk, ok := darwinKeyCodes[key]; ok && cgEventSourceKeyState != nil {
		return cgEventSourceKeyState(0, vk)
	}
	return true
}
