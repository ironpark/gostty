package ui

import "image/color"

type Theme struct {
	Name                                  string
	Terminal                              bool
	Background, Foreground, Panel, Border color.RGBA
	Text, Dim, Accent                     color.RGBA
	// Palette is the sixteen ANSI colours the theme asks the terminal to use,
	// or nil to leave the terminal's own. Setting the defaults is what makes a
	// theme reach the colours a program picked by name -- red is whatever the
	// terminal says red is -- rather than only the two default ones.
	Palette []color.RGBA
}

var themes = []Theme{
	{
		Name: "terminal", Terminal: true,
		Panel: color.RGBA{R: 0x20, G: 0x20, B: 0x28, A: 0xf0}, Border: color.RGBA{R: 0x50, G: 0x50, B: 0x60, A: 0xff},
		Text: color.RGBA{R: 0xe0, G: 0xe0, B: 0xe0, A: 0xff}, Dim: color.RGBA{R: 0x90, G: 0x90, B: 0xa0, A: 0xff},
		Accent: color.RGBA{R: 0x7a, G: 0xc0, B: 0xff, A: 0xff},
	},
	{
		Name:       "midnight",
		Background: color.RGBA{R: 0x0b, G: 0x10, B: 0x1f, A: 0xff}, Foreground: color.RGBA{R: 0xd7, G: 0xe3, B: 0xfc, A: 0xff},
		Panel: color.RGBA{R: 0x12, G: 0x1a, B: 0x30, A: 0xf4}, Border: color.RGBA{R: 0x38, G: 0x55, B: 0x83, A: 0xff},
		Text: color.RGBA{R: 0xea, G: 0xf2, B: 0xff, A: 0xff}, Dim: color.RGBA{R: 0x82, G: 0x97, B: 0xb8, A: 0xff},
		Accent: color.RGBA{R: 0x57, G: 0xd9, B: 0xff, A: 0xff},
	},
	{
		Name: "catppuccin",
		// Catppuccin Mocha's published ANSI set.
		Palette: hexPalette(
			0x45475a, 0xf38ba8, 0xa6e3a1, 0xf9e2af, 0x89b4fa, 0xf5c2e7, 0x94e2d5, 0xbac2de,
			0x585b70, 0xf38ba8, 0xa6e3a1, 0xf9e2af, 0x89b4fa, 0xf5c2e7, 0x94e2d5, 0xa6adc8,
		),
		Background: color.RGBA{R: 0x1e, G: 0x1e, B: 0x2e, A: 0xff}, Foreground: color.RGBA{R: 0xcd, G: 0xd6, B: 0xf4, A: 0xff},
		Panel: color.RGBA{R: 0x31, G: 0x32, B: 0x44, A: 0xf4}, Border: color.RGBA{R: 0x58, G: 0x5b, B: 0x70, A: 0xff},
		Text: color.RGBA{R: 0xcd, G: 0xd6, B: 0xf4, A: 0xff}, Dim: color.RGBA{R: 0x93, G: 0x9a, B: 0xb7, A: 0xff},
		Accent: color.RGBA{R: 0x89, G: 0xdc, B: 0xeb, A: 0xff},
	},
	{
		Name: "solarized",
		// Solarized dark, as Ethan Schoonover published it: the "bright"
		// half is the base tones rather than lighter versions of the eight.
		Palette: hexPalette(
			0x073642, 0xdc322f, 0x859900, 0xb58900, 0x268bd2, 0xd33682, 0x2aa198, 0xeee8d5,
			0x002b36, 0xcb4b16, 0x586e75, 0x657b83, 0x839496, 0x6c71c4, 0x93a1a1, 0xfdf6e3,
		),
		Background: color.RGBA{R: 0x00, G: 0x2b, B: 0x36, A: 0xff}, Foreground: color.RGBA{R: 0x93, G: 0xa1, B: 0xa1, A: 0xff},
		Panel: color.RGBA{R: 0x07, G: 0x36, B: 0x42, A: 0xf4}, Border: color.RGBA{R: 0x58, G: 0x6e, B: 0x75, A: 0xff},
		Text: color.RGBA{R: 0xee, G: 0xe8, B: 0xd5, A: 0xff}, Dim: color.RGBA{R: 0x83, G: 0x94, B: 0x96, A: 0xff},
		Accent: color.RGBA{R: 0x2a, G: 0xa1, B: 0x98, A: 0xff},
	},
}

// hexPalette spells the sixteen ANSI colours the way palettes are published.
func hexPalette(values ...uint32) []color.RGBA {
	palette := make([]color.RGBA, len(values))
	for i, v := range values {
		palette[i] = color.RGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xff}
	}
	return palette
}

// ThemeAt wraps the index into the built-in themes.
func ThemeAt(index int) Theme { return themes[(index%len(themes)+len(themes))%len(themes)] }
func ThemeCount() int         { return len(themes) }

// Light resolves the background before classifying its luminance.
func (t Theme) Light(terminalBackground color.RGBA) bool {
	bg := t.Background
	if t.Terminal {
		bg = terminalBackground
	}
	return 0.299*float64(bg.R)+0.587*float64(bg.G)+0.114*float64(bg.B) > 0x80
}

// ResolveColor replaces terminal defaults while preserving explicit ANSI colors.
func (t Theme) ResolveColor(c, background, foreground color.RGBA) color.RGBA {
	if t.Terminal {
		return c
	}
	if c == background {
		return t.Background
	}
	if c == foreground {
		return t.Foreground
	}
	return c
}
