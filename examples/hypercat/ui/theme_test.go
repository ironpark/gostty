package ui

import (
	"image/color"
	"testing"
)

func TestThemeReplacesOnlyTerminalDefaults(t *testing.T) {
	background := color.RGBA{R: 1, G: 2, B: 3, A: 0xff}
	foreground := color.RGBA{R: 4, G: 5, B: 6, A: 0xff}
	ansiRed := color.RGBA{R: 0xff, A: 0xff}
	theme := ThemeAt(1)

	if got := theme.ResolveColor(background, background, foreground); got != ThemeAt(1).Background {
		t.Errorf("background = %v, want %v", got, ThemeAt(1).Background)
	}
	if got := theme.ResolveColor(foreground, background, foreground); got != ThemeAt(1).Foreground {
		t.Errorf("foreground = %v, want %v", got, ThemeAt(1).Foreground)
	}
	if got := theme.ResolveColor(ansiRed, background, foreground); got != ansiRed {
		t.Errorf("explicit ANSI color changed from %v to %v", ansiRed, got)
	}
}
