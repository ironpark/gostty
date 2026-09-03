//go:build !darwin

package main

import "github.com/hajimehoshi/ebiten/v2"

func currentMods() mods {
	return mods{
		shift: ebiten.IsKeyPressed(ebiten.KeyShift),
		ctrl:  ebiten.IsKeyPressed(ebiten.KeyControl),
		alt:   ebiten.IsKeyPressed(ebiten.KeyAlt),
		super: ebiten.IsKeyPressed(ebiten.KeyMeta),
	}
}

func isKeyPhysicallyPressed(key ebiten.Key) bool {
	return true
}
