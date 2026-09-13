// Command hypercat is a small GUI terminal emulator built on gostty and
// Ebitengine. It exists to show the binding in use, so read it in this order:
//
//   - terminal.go: one terminal. Shell output → Stream.Feed → events → replies.
//   - input.go:    host keyboard and mouse → input.EncodeKey/EncodeMouse → pty.
//   - frame.go:    RenderState → the cells, cursor and colours a frame is drawn from.
//   - draw.go:     those cells → pixels, with Ebitengine.
//   - window.go:   the frame loop, tabs, and what the window shares between them.
//
// selection.go, search.go, desktop.go and kitty.go are the optional features:
// the selection gesture, scrollback search, OSC 8/52/72 and Kitty graphics.
package main

import (
	"log"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/ironpark/gostty/examples/hypercat/ui"
	"github.com/ironpark/gostty/sys"
)

const (
	initialCols = 100
	initialRows = 30

	// appName is the window title until a program sets one, and the prefix of
	// every title a program does set.
	appName = "Hyper Cat Term /ᐠ ˵> ⩊ <˵マ"

	// What this program calls itself to the programs running in it: XTVERSION
	// is answered with a name and a number.
	reportName    = "hypercat"
	reportVersion = "0.1.0"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

// run creates the first tab and runs the window until the last tab closes.
// Ebitengine owns the main thread, so this is called from main directly.
func run() error {
	// libghostty-vt has no PNG decoder of its own, so without this a Kitty
	// `f=100` transmission is refused. With it, PNGs are decoded as they
	// arrive and reach the renderer as RGBA like every other format.
	sys.OnPngDecodeRequest(decodePNG)
	defer sys.Clear()

	win := newWindow()
	defer win.close()
	if err := win.addTab(); err != nil {
		return err
	}
	win.startCat()
	win.clipboard.init()

	cell := win.settings.fonts
	ebiten.SetWindowSize(int(cell.CellWidth*initialCols/win.dsf), int(cell.CellHeight*initialRows/win.dsf+ui.TabBarHeight))
	ebiten.SetWindowTitle(appName)
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)
	ebiten.SetScreenClearedEveryFrame(false)
	// RunGame returns nil when update ends the loop with ebiten.Termination.
	return ebiten.RunGame(win)
}
