// Command hypercat is a small GUI terminal emulator built on libghostty-vt.
//
// It runs your shell on a pty and draws the screen in a window. Everything a
// terminal has to know -- how the bytes from the shell change the grid, what a
// keystroke encodes to given the modes the program has set, which colors a cell
// ended up with -- comes from gostty. This program only owns the pixels.
//
//	shell --pty--> Stream.Feed --> Terminal --> RenderState --> Ebitengine
//	                                                        \-> the cat walks on it
//	shell <--pty-- input.EncodeKey   <-- KeyEvent   <-- Ebitengine
//	shell <--pty-- input.EncodeMouse <-- MouseEvent <-- Ebitengine
//
// It is deliberately small, so it stops well short of a terminal you would use:
// no ligatures, no font fallback, and no reflowing of wide glyphs beyond a
// two-cell advance.
package main

import (
	"errors"
	"log"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/ironpark/gostty/examples/hypercat/ui"
	"github.com/ironpark/gostty/sys"
	"golang.design/x/clipboard"
)

const (
	initialCols = 100
	initialRows = 30
)

func main() {
	if err := run(); err != nil && !errors.Is(err, ebiten.Termination) {
		log.Fatal(err)
	}
}

func run() error {
	// libghostty-vt has no PNG decoder of its own, so without this a Kitty
	// `f=100` transmission is refused. With it, PNGs are decoded as they
	// arrive and reach the renderer as RGBA like every other format.
	sys.OnPngDecodeRequest(decodePNG)
	defer sys.Clear()

	app := newApp()
	defer app.close()
	if err := app.addTab(); err != nil {
		return err
	}

	// A system clipboard is not guaranteed (a headless Linux box has none), so
	// its absence is a degradation rather than a failure.
	if err := clipboard.Init(); err != nil {
		log.Printf("no system clipboard, staying in-process: %v", err)
	} else {
		app.systemClipboard = true
	}

	// The window is asked for in device-independent pixels, which is the one
	// place the grid's own units have to be converted back.
	ebiten.SetWindowSize(
		int(app.settings.fonts.CellWidth*initialCols/app.dsf),
		int(app.settings.fonts.CellHeight*initialRows/app.dsf+ui.TabBarHeight),
	)
	ebiten.SetWindowTitle("Hyper Cat Term /ᐠ ˵> ⩊ <˵マ")
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)
	ebiten.SetScreenClearedEveryFrame(false)
	return ebiten.RunGame(app)
}
