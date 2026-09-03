// Command example is a small GUI terminal emulator built on libghostty-vt.
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
	"fmt"
	"image/color"
	"log"
	"os"
	"os/exec"

	"github.com/creack/pty"
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
	"github.com/ironpark/gostty"
	"github.com/ironpark/gostty/example/thecat"
	"github.com/ironpark/gostty/input"
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
	// The families are found once, at startup: the settings panel offers this
	// list, and one of them is what the window opens with.
	families := discoverFonts()
	chosen, at := defaultFamily(families)

	// Everything below is in device pixels, so a HiDPI screen is drawn at its
	// own resolution rather than at a quarter of it and stretched. The scale
	// factor is the only place the two systems meet: the settings panel talks
	// about a 14px font, and on a 2x display that is a 28px face.
	g := &game{
		dsf:  deviceScale(),
		cols: initialCols,
		rows: initialRows,
	}
	g.ui.families = families
	g.ui.family = at
	g.ui.size = defaultFontSize
	g.fonts = loadFonts(chosen, defaultFontSize*g.dsf)
	// Emoji come out of their own font as pictures; without one they fall back
	// to the ordinary faces, which is what they did before.
	g.emoji = loadEmoji()
	if err := g.start(); err != nil {
		return err
	}
	defer g.close()

	// A system clipboard is not guaranteed (a headless Linux box has none), so
	// its absence is a degradation rather than a failure.
	if err := clipboard.Init(); err != nil {
		log.Printf("no system clipboard, staying in-process: %v", err)
	} else {
		g.systemClipboard = true
	}

	// The window is asked for in device-independent pixels, which is the one
	// place the grid's own units have to be converted back.
	ebiten.SetWindowSize(
		int(g.fonts.cellW*initialCols/g.dsf),
		int(g.fonts.cellH*initialRows/g.dsf),
	)
	ebiten.SetWindowTitle("Hyper Cat Term /ᐠ ˵> ⩊ <˵マ")
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)
	ebiten.SetScreenClearedEveryFrame(false)
	return ebiten.RunGame(g)
}

type game struct {
	// The terminal side.
	vt     *gostty.Terminal
	stream *gostty.Stream
	state  *gostty.RenderState
	images *gostty.KittyImages
	ev     *input.KeyEvent
	mev    *input.MouseEvent
	cells  []gostty.RenderCell

	// The process side.
	ptmx   *os.File
	output chan []byte

	// The pixel side.
	fonts      *fontSet
	emoji      *emojiFont
	cols, rows int
	// The terminal's resolved defaults and the themed colors used to draw
	// them. Explicit ANSI colors continue to come from the terminal.
	terminalBg, terminalFg color.RGBA
	bg, fg                 color.RGBA
	cursor                 cursorState
	bell                   int
	drawOp                 text.DrawOptions

	// What the program has said about itself, all of which lands in the window
	// title because there is nowhere else to put it.
	title, pwd, progress string

	// The clipboard. The system one when it is available; otherwise a
	// process-local buffer, which at least lets OSC 52 and paste agree.
	clipboard       []byte
	systemClipboard bool

	sel selection

	// The search bar and the settings panel, which take the keyboard while
	// they are open.
	ui ui
	// Set when the faces changed, so Layout redoes the grid even if the window
	// did not move.
	relayout bool
	// The display's scale factor. Every pixel in this program is a device
	// pixel; this is what the window's own units are converted with.
	dsf float64

	// The cat that walks on the output.
	cat                      *thecat.Cat
	catOn                    bool
	catAttention             int
	catPointerX, catPointerY int
	// Whether the pointer is on the cat, and a frame counter for the pulse of
	// the marks that says so.
	catHover bool
	frame    int

	// Kitty graphics: the placements for this frame and the textures they
	// point at, kept across frames and keyed by the image's generation.
	placements []gostty.KittyPlacement
	textures   map[uint32]*texture
	imageBuf   []byte

	// Mouse and focus reporting: what the program is told about the pointer,
	// as opposed to what the window does with it itself.
	report             []byte
	mouseCol, mouseRow int
	wheel              float64
	mouseGrabbed       bool
	focused            bool
	focusedFrames      int

	// Per-frame input scratch, reused so a keystroke allocates nothing.
	chars   []rune
	pressed []ebiten.Key
	utf8    []byte
	out     []byte
	enc     outputWriter
}

// cursorState is what Draw needs to paint the cursor, read once per frame.
type cursorState struct {
	x, y    uint16
	visible bool
	style   gostty.CursorStyle
}

func (g *game) start() error {
	g.enc = outputWriter{g: g}

	var err error
	if g.vt, err = gostty.NewTerminal(uint16(g.cols), uint16(g.rows)); err != nil {
		return err
	}
	// The stream must be closed before the terminal: its handler reaches
	// through the terminal for an allocator when it tears down.
	if g.stream, err = g.vt.NewStream(0); err != nil {
		return err
	}
	if g.state, err = gostty.NewRenderState(); err != nil {
		return err
	}
	if g.images, err = gostty.NewKittyImages(); err != nil {
		return err
	}
	g.textures = make(map[uint32]*texture)
	if g.ev, err = input.NewKeyEvent(); err != nil {
		return err
	}
	if g.mev, err = input.NewMouseEvent(); err != nil {
		return err
	}
	g.focused = ebiten.IsFocused()
	if g.focused {
		g.focusedFrames = 1
	}

	// A clipboard write must be answered from inside the callback: it runs
	// while Feed is still on the stack and the program is blocked on it.
	if err := g.stream.OnClipboardWriteRequest(func() {
		n, err := g.stream.ClipboardContentCount()
		if err == nil && n > 0 {
			if data, err := g.stream.ClipboardContentData(0); err == nil {
				g.clipboard = append(g.clipboard[:0], data...)
			}
		}
		_ = g.stream.AllowClipboard(false)
	}); err != nil {
		return err
	}
	if err := g.stream.OnClipboardReadRequest(func() {
		// A read hands the running program whatever the user copied, so a real
		// emulator would ask the user first. This one answers immediately,
		// which is the wrong default for anything but a demo.
		_ = g.stream.ReplyClipboardText(string(g.pasteText()), false)
	}); err != nil {
		return err
	}

	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	cmd := exec.Command(shell)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	g.ptmx, err = pty.StartWithSize(cmd, &pty.Winsize{
		Cols: uint16(g.cols), Rows: uint16(g.rows),
		X: uint16(g.fonts.cellW) * uint16(g.cols), Y: uint16(g.fonts.cellH) * uint16(g.rows),
	})
	if err != nil {
		return fmt.Errorf("start %s: %w", shell, err)
	}

	g.startCat()

	g.output = make(chan []byte, 64)
	go func() {
		defer close(g.output)
		buf := make([]byte, 1<<16)
		for {
			n, err := g.ptmx.Read(buf)
			if n > 0 {
				g.output <- append([]byte(nil), buf[:n]...)
			}
			if err != nil {
				return
			}
		}
	}()
	return nil
}

func (g *game) close() {
	if g.ptmx != nil {
		g.ptmx.Close()
	}
	// Reverse construction order; the stream is a child of the terminal, and
	// closing the terminal first would be refused. The search is a child of a
	// screen, which is borrowed from the terminal, so it goes first of all.
	g.ui.closeSearch()
	for _, tex := range g.textures {
		if tex.image != nil {
			tex.image.Deallocate()
		}
	}
	_ = g.images.Close()
	_ = g.mev.Close()
	_ = g.ev.Close()
	_ = g.state.Close()
	_ = g.stream.Close()
	_ = g.vt.Close()
}

// Update advances the emulator by one frame: bytes in from the shell, the
// render state refreshed, keystrokes out.
func (g *game) Update() error {
	fed := false
	for draining := true; draining; {
		select {
		case chunk, ok := <-g.output:
			if !ok {
				return ebiten.Termination // the shell exited
			}
			if err := g.stream.Feed(chunk); err != nil {
				return fmt.Errorf("feed: %w", err)
			}
			fed = true
		default:
			draining = false
		}
	}

	if fed {
		if err := g.drainEvents(); err != nil {
			return err
		}
		// The terminal answers some sequences itself -- device status, size
		// reports, Kitty graphics acknowledgements -- and a program that asked
		// is blocked until the answer arrives. Nothing is written from inside
		// the feed, so the replies are drained right after it.
		if err := g.stream.WriteReplies(g.ptmx); err != nil {
			return fmt.Errorf("reply: %w", err)
		}
	}
	// Input before the refresh, so a selection made this frame is drawn this
	// frame rather than one behind. The modifier state is read once and shared:
	// the mouse needs Alt for block selection and the keyboard needs all four.
	m := currentMods()
	if err := g.reportFocus(); err != nil {
		return err
	}
	if g.focused {
		// A panel takes the keyboard while it is open, so nothing typed into the
		// search bar reaches the shell. The mouse is left alone: a selection is
		// still worth being able to make.
		taken, err := g.handleUI(m)
		if err != nil {
			return err
		}
		// A click on the cat is the cat's, not the shell's: it must not also start
		// a selection or be reported to the program.
		if !g.pokeCat() {
			if err := g.handleMouse(m); err != nil {
				return err
			}
		}
		if err := g.handleWheel(m); err != nil {
			return err
		}
		if !taken {
			if err := g.handleInput(m); err != nil {
				return err
			}
		} else if fed && g.ui.mode == uiSearch {
			// New output moves the scrollback the matches point into, so the
			// search is rebuilt rather than left pointing at what has moved.
			if err := g.runSearch(); err != nil {
				return err
			}
		}
	}
	if err := g.refresh(); err != nil {
		return err
	}
	// After the refresh: the cat walks on the cells this frame is about to
	// draw, not the ones the last frame drew.
	g.updateCat()
	return nil
}

// drainEvents acts on what the program asked of the emulator rather than of the
// screen. libghostty-vt parses OSC; doing something about it is ours.
func (g *game) drainEvents() error {
	for {
		event, ok, err := g.stream.NextEvent()
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		switch event {
		case gostty.StreamEventBell:
			g.bell = 6 // frames of visual bell
		case gostty.StreamEventPwdChanged:
			// OSC 7. A real emulator opens new tabs here; this one has none, so
			// the directory rides along in the window title.
			if pwd, ok, err := g.vt.GetPwd(); err == nil && ok {
				g.pwd = pwd
				g.retitle()
			}
		case gostty.StreamEventDesktopNotification:
			// OSC 9 and OSC 777. There is no notification service to hand this
			// to from a window this small, so it goes to the log.
			title, err := g.stream.EventTitle()
			if err != nil {
				return err
			}
			body, err := g.stream.EventBody()
			if err != nil {
				return err
			}
			log.Printf("notification: %s %s", title, body)
		case gostty.StreamEventProgressReport:
			if err := g.progressReport(); err != nil {
				return err
			}
		case gostty.StreamEventTitleChanged:
			// The event only announces the change; the value lives on the
			// terminal, which is where ghostty keeps it.
			if title, ok, err := g.vt.GetTitle(); err == nil && ok {
				g.title = title
				g.retitle()
			}
		}
	}
}

// progressReport reads OSC 9;4, which a long-running command uses to say how
// far along it is. The state says what to show; the percentage is only there
// for one of them, which is why it comes back with a flag.
func (g *game) progressReport() error {
	state, err := g.stream.EventProgressState()
	if err != nil {
		return err
	}
	percent, ok, err := g.stream.EventProgress()
	if err != nil {
		return err
	}
	switch {
	case state == gostty.ProgressStateRemove:
		g.progress = ""
	case ok:
		g.progress = fmt.Sprintf("%d%%", percent)
	default:
		g.progress = state.String()
	}
	g.retitle()
	return nil
}

// retitle rebuilds the window title out of everything the program has told us
// about itself.
func (g *game) retitle() {
	title := "gostty"
	if g.title != "" {
		title += " - " + g.title
	}
	if g.pwd != "" {
		title += " (" + g.pwd + ")"
	}
	if g.progress != "" {
		title += " [" + g.progress + "]"
	}
	ebiten.SetWindowTitle(title)
}

// refresh pulls the viewport out of the render state. This is the only place
// cell data crosses the boundary, and it is one call for the whole grid.
func (g *game) refresh() error {
	if err := g.state.Update(g.vt); err != nil {
		return fmt.Errorf("render update: %w", err)
	}
	n, err := g.state.CellCount()
	if err != nil {
		return err
	}
	if uint(cap(g.cells)) < n {
		g.cells = make([]gostty.RenderCell, n)
	}
	g.cells = g.cells[:n]
	if _, err := g.state.Cells(g.cells); err != nil {
		return fmt.Errorf("render cells: %w", err)
	}
	if err := g.refreshImages(); err != nil {
		return err
	}
	bg, err := g.state.Background()
	if err != nil {
		return err
	}
	g.terminalBg = rgb(bg)
	fg, err := g.state.Foreground()
	if err != nil {
		return err
	}
	g.terminalFg = rgb(fg)
	theme := g.currentTheme()
	if theme.terminal {
		g.bg, g.fg = g.terminalBg, g.terminalFg
	} else {
		g.bg, g.fg = theme.background, theme.foreground
	}
	return g.refreshCursor()
}

// refreshCursor reads the cursor out of the render state, so Draw does not have
// to reach across the boundary from a place that cannot report a failure.
func (g *game) refreshCursor() error {
	visible, err := g.state.CursorVisible()
	if err != nil {
		return err
	}
	x, onScreen, err := g.state.CursorX()
	if err != nil {
		return err
	}
	y, _, err := g.state.CursorY()
	if err != nil {
		return err
	}
	style, err := g.state.CursorStyle()
	if err != nil {
		return err
	}
	// `onScreen` is false when the viewport has been scrolled away from it.
	g.cursor = cursorState{x: x, y: y, visible: visible && onScreen, style: style}
	return nil
}

// LayoutF resizes the emulated screen and the pty to match the window.
//
// It returns the window in device pixels, so that is the size of the image
// `Draw` is handed and the resolution the text is rasterised at: on a 2x
// display the alternative is drawing a 14px face into a quarter of the pixels
// the screen has and letting the compositor blow it up. Ebitengine reports the
// pointer in the same coordinates it hands out here, so nothing else in this
// program has to know which kind of pixel it is holding.
func (g *game) LayoutF(outsideWidth, outsideHeight float64) (float64, float64) {
	// The window can be dragged to a display with a different scale.
	if dsf := deviceScale(); dsf != g.dsf {
		g.dsf = dsf
		g.applyFont()
	}
	width, height := outsideWidth*g.dsf, outsideHeight*g.dsf

	cols := max(int(width/g.fonts.cellW), 1)
	rows := max(int(height/g.fonts.cellH), 1)
	if cols != g.cols || rows != g.rows || g.relayout {
		g.relayout = false
		if err := g.resize(cols, rows); err != nil {
			// Layout cannot fail, and a terminal at the wrong size is better
			// than no terminal, so this is reported and the frame goes on.
			log.Printf("resize to %dx%d: %v", cols, rows, err)
		}
	}
	return width, height
}

// Layout is what the Game interface asks for; Ebitengine calls LayoutF instead
// whenever it is implemented, which it is.
func (g *game) Layout(outsideWidth, outsideHeight int) (int, int) {
	w, h := g.LayoutF(float64(outsideWidth), float64(outsideHeight))
	return int(w), int(h)
}

// resize moves the emulated screen and the pty together. They have to agree:
// the program asks the pty how big it is and writes for the terminal.
func (g *game) resize(cols, rows int) error {
	g.cols, g.rows = cols, rows
	cellW, cellH := uint16(g.fonts.cellW), uint16(g.fonts.cellH)
	// `ResizeCells` rather than `Resize`: a Kitty image sized in cells is
	// measured in pixels through the cell size, and the terminal stores the
	// pixel size of the whole grid, so it goes stale on every column change.
	if err := g.vt.ResizeCells(uint16(cols), uint16(rows), uint32(cellW), uint32(cellH)); err != nil {
		return err
	}
	// The pty carries the same two sizes, which is where a program that has not
	// asked the terminal directly reads them from.
	return pty.Setsize(g.ptmx, &pty.Winsize{
		Cols: uint16(cols), Rows: uint16(rows),
		X: cellW * uint16(cols), Y: cellH * uint16(rows),
	})
}

// deviceScale is how many pixels the display has per device-independent pixel.
//
// One before there is a window to ask about, which is the case for the first
// font load: the first LayoutF picks up the real answer and reloads, so the
// only cost of guessing is one frame at the wrong size.
func deviceScale() float64 {
	monitor := ebiten.Monitor()
	if monitor == nil {
		return 1
	}
	if scale := monitor.DeviceScaleFactor(); scale > 0 {
		return scale
	}
	return 1
}

func rgb(v uint32) color.RGBA {
	return color.RGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xff}
}
