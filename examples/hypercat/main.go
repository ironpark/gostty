// Command hypercat demonstrates the PTY → gostty → window flow.
// Read main.go, terminal.go, terminal_input.go, and render_frame.go.
// internal/frontend owns the Ebitengine window, input polling, and rendering.
package main

import (
	"log"

	"github.com/ironpark/gostty/examples/hypercat/internal/frontend"
	"github.com/ironpark/gostty/examples/hypercat/internal/graphics"
	"github.com/ironpark/gostty/examples/hypercat/ui"
	"github.com/ironpark/gostty/sys"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

const (
	initialCols = 100
	initialRows = 30

	// appName is what the window is called before a program has said anything
	// about itself, and the name every title it does set is shown under.
	appName = "Hyper Cat Term /ᐠ ˵> ⩊ <˵マ"

	// What this program calls itself to the programs running in it, which is
	// the window title with the decoration taken off: XTVERSION is answered
	// with it, and something parsing that answer wants a name and a number.
	reportName    = "hypercat"
	reportVersion = "0.1.0"
)

// run creates the first tab and runs the window until all tabs close.
// Call it from main: Ebitengine owns the main thread.
func run() error {
	// libghostty-vt has no PNG decoder of its own, so without this a Kitty
	// `f=100` transmission is refused. With it, PNGs are decoded as they
	// arrive and reach the renderer as RGBA like every other format.
	sys.OnPngDecodeRequest(graphics.DecodePNG)
	defer sys.Clear()

	win := newWindow()
	defer win.close()
	if err := win.addTab(); err != nil {
		return err
	}

	win.startCat()
	win.clipboard.Init()

	return frontend.Run(win, frontend.Config{
		Title:  appName,
		Width:  int(win.settings.Fonts().CellWidth * initialCols / win.dsf),
		Height: int(win.settings.Fonts().CellHeight*initialRows/win.dsf + ui.TabBarHeight),
	})
}
