//go:build darwin

package main

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

func currentMods() mods {
	if cgEventSourceFlagsState != nil {
		flags := cgEventSourceFlagsState(0) // kCGEventSourceStateCombinedSessionState
		return mods{
			shift: flags&kCGEventFlagMaskShift != 0,
			ctrl:  flags&kCGEventFlagMaskControl != 0,
			alt:   flags&kCGEventFlagMaskAlternate != 0,
			super: flags&kCGEventFlagMaskCommand != 0,
		}
	}
	return mods{
		shift: ebiten.IsKeyPressed(ebiten.KeyShift),
		ctrl:  ebiten.IsKeyPressed(ebiten.KeyControl),
		alt:   ebiten.IsKeyPressed(ebiten.KeyAlt),
		super: ebiten.IsKeyPressed(ebiten.KeyMeta),
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

func isKeyPhysicallyPressed(key ebiten.Key) bool {
	if vk, ok := darwinKeyCodes[key]; ok && cgEventSourceKeyState != nil {
		return cgEventSourceKeyState(0, vk)
	}
	return true
}
