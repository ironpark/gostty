// Command example is a small GUI terminal emulator built on libghostty-vt.
//
// It runs your shell on a pty and draws the screen in a window. Everything a
// terminal has to know -- how the bytes from the shell change the grid, what a
// keystroke encodes to given the modes the program has set, which colors a cell
// ended up with -- comes from gostty. This program only owns the pixels.
//
//	shell --pty--> Stream.Feed --> Terminal --> RenderState --> Ebitengine
//	shell <--pty-- input.EncodeKey <-- KeyEvent <-- Ebitengine
//
// It is deliberately small, so it stops well short of a terminal you would use:
// no selection, no scrollback view, no ligatures or fallback fonts, no
// reflowing of wide glyphs beyond a two-cell advance, and the clipboard is
// process-local.
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
	"github.com/ironpark/gostty"
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
	fonts, err := loadFonts()
	if err != nil {
		return err
	}

	g := &game{
		fonts: fonts,
		cols:  initialCols,
		rows:  initialRows,
	}
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

	ebiten.SetWindowSize(int(fonts.cellW*initialCols), int(fonts.cellH*initialRows))
	ebiten.SetWindowTitle("gostty")
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)
	ebiten.SetScreenClearedEveryFrame(false)
	return ebiten.RunGame(g)
}

type game struct {
	// The terminal side.
	vt     *gostty.Terminal
	stream *gostty.Stream
	state  *gostty.RenderState
	ev     *input.KeyEvent
	cells  []gostty.RenderCell

	// The process side.
	ptmx   *os.File
	output chan []byte

	// The pixel side.
	fonts      *fontSet
	cols, rows int
	bg         color.RGBA
	bell       int

	// The clipboard. The system one when it is available; otherwise a
	// process-local buffer, which at least lets OSC 52 and paste agree.
	clipboard       []byte
	systemClipboard bool

	sel  selection
	keys keyState
	out  []byte
}

func (g *game) start() error {
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
	if g.ev, err = input.NewKeyEvent(); err != nil {
		return err
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
		_ = g.stream.ReplyClipboardText(g.pasteText(), false)
	}); err != nil {
		return err
	}

	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	cmd := exec.Command(shell)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	g.ptmx, err = pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(g.cols), Rows: uint16(g.rows)})
	if err != nil {
		return fmt.Errorf("start %s: %w", shell, err)
	}

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
	// Reverse construction order; the stream is a child of the terminal.
	for _, h := range []interface{ Close() error }{g.ev, g.state, g.stream, g.vt} {
		if h != nil {
			_ = h.Close()
		}
	}
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
	}
	// Input before the refresh, so a selection made this frame is drawn this
	// frame rather than one behind.
	if err := g.handleMouse(); err != nil {
		return err
	}
	if err := g.handleInput(); err != nil {
		return err
	}
	return g.refresh()
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
		case gostty.StreamEventTitleChanged:
			// The event only announces the change; the value lives on the
			// terminal, which is where ghostty keeps it.
			if title, ok, err := g.vt.GetTitle(); err == nil && ok {
				ebiten.SetWindowTitle("gostty - " + string(title))
			}
		}
	}
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
	bg, err := g.state.Background()
	if err != nil {
		return err
	}
	g.bg = rgb(bg)
	return nil
}

// Layout resizes the emulated screen and the pty to match the window.
func (g *game) Layout(outsideWidth, outsideHeight int) (int, int) {
	cols := max(int(float64(outsideWidth)/g.fonts.cellW), 1)
	rows := max(int(float64(outsideHeight)/g.fonts.cellH), 1)
	if cols != g.cols || rows != g.rows {
		g.cols, g.rows = cols, rows
		if err := g.vt.Resize(uint16(cols), uint16(rows)); err != nil {
			log.Printf("resize: %v", err)
		}
		_ = pty.Setsize(g.ptmx, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	}
	return outsideWidth, outsideHeight
}

func rgb(v uint32) color.RGBA {
	return color.RGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xff}
}
