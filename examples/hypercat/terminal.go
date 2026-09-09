package main

import (
	"github.com/ironpark/gostty"
	"github.com/ironpark/gostty/examples/hypercat/shell"
)

// How much scrollback a tab keeps.
//
// The native default is unbounded, which is the right default for a library --
// it cannot know how many tabs an application will open, or how long they live
// -- and the wrong one for a window with a tab bar: a shell that prints for a
// week would keep every line of it. The line count is pruned at page
// boundaries rather than exactly, and the byte ceiling is what actually bounds
// the memory, so both are set.
const (
	scrollbackMaxLines = 10_000
	scrollbackMaxBytes = 16 << 20
)

func (tab *terminalTab) start() error {
	var err error
	if tab.vt, err = gostty.NewTerminalWithConfig(tab.terminalConfig()); err != nil {
		return err
	}
	// The stream must be closed before the terminal: its handler reaches
	// through the terminal for an allocator when it tears down.
	if tab.stream, err = tab.vt.NewStreamWithConfig(tab.streamConfig()); err != nil {
		return err
	}
	// Drag and drop (OSC 72) is a callback rather than a setting: a program
	// says which types it will take, and is told when a drag happens over a
	// window it cannot see. Without it the protocol still works -- the stream
	// answers it -- but this program would not know a drop had anywhere to go.
	if err := tab.stream.OnDrag(tab.onDrag); err != nil {
		return err
	}
	if tab.state, err = gostty.NewRenderState(); err != nil {
		return err
	}
	if tab.images, err = newImageCache(tab.vt); err != nil {
		return err
	}
	if err := tab.startGesture(); err != nil {
		return err
	}
	// A tab opens in the window's theme, palette included.
	if err := tab.applyPalette(); err != nil {
		return err
	}
	tab.shell, err = shell.Start(tab.cols, tab.rows)
	if err != nil {
		return err
	}
	tab.startCat()
	return nil
}

// terminalConfig is everything the terminal is opened with, in one value
// rather than a handful of setters afterwards: a setting that fails closes the
// terminal, so no half-configured tab can escape this call.
func (tab *terminalTab) terminalConfig() gostty.TerminalConfig {
	lines, bytes := uint(scrollbackMaxLines), uint(scrollbackMaxBytes)
	return gostty.TerminalConfig{
		Cols:               uint16(tab.cols),
		Rows:               uint16(tab.rows),
		ScrollbackMaxLines: &lines,
		ScrollbackMaxBytes: &bytes,
		ModeDefaults: []gostty.ModeDefault{
			// Grapheme clustering (mode 2027). Off, a flag is two cells of
			// regional indicator and a family is three cells of people: the
			// terminal counts codepoints because it cannot know whether the
			// thing drawing them can put a cluster in one cell. This one can --
			// the renderer reads the cluster back and the emoji font shapes it
			// -- so it says so.
			//
			// A default rather than a mode set afterwards, so a program's full
			// reset does not quietly take it away again.
			{Mode: gostty.ModeGraphemeCluster, Enabled: true},
		},
	}
}

// streamConfig is the parser's settings and this program's identity.
func (tab *terminalTab) streamConfig() gostty.StreamConfig {
	scheme := tab.colorScheme()
	return gostty.StreamConfig{
		// Capture the sequences this library does not implement, so a program
		// speaking a graphics protocol hypercat never wired up says so in the
		// log instead of drawing nothing.
		UnknownMaxBytes: 256,
		// Tell programs who they are talking to. Without this XTVERSION names
		// the parser ("libghostty"), which is true and useless: a program
		// checking whether the terminal supports something wants the
		// emulator's identity.
		Version: &gostty.VersionReport{Name: reportName, Version: reportVersion},
		// The color scheme answers CSI ? 996 n and, once a program subscribes
		// with mode 2031, is reported the moment it changes. A real emulator
		// would hook this to the OS appearance notification; this one only
		// knows its own theme, so it says that.
		ColorScheme: &scheme,
		// A clipboard write must be answered from inside the callback: it runs
		// while Feed is still on the stack and the program is blocked on it.
		ClipboardWrite: func(req *gostty.ClipboardRequest) {
			n, err := req.ContentCount()
			if err == nil && n > 0 {
				if data, err := req.ContentData(0); err == nil {
					tab.clipboard.hold(data)
				}
			}
			_ = req.Allow(false)
		},
		ClipboardRead: func(req *gostty.ClipboardRequest) {
			// A read hands the running program whatever the user copied, so a
			// real emulator would ask the user first. This one answers
			// immediately, which is the wrong default for anything but a demo.
			_ = req.ReplyText(string(tab.clipboard.paste()), false)
		},
	}
}

func (tab *terminalTab) close() {
	tab.shell.Close()
	// Reverse construction order; the stream is a child of the terminal, and
	// closing the terminal first would be refused. The search is a child of a
	// screen, which is borrowed from the terminal, so it goes first of all.
	tab.closeSearch()
	if tab.sel.gesture != nil {
		_ = tab.sel.gesture.Close()
		tab.sel.gesture = nil
	}
	tab.layers.close()
	tab.images.close()
	_ = tab.state.Close()
	_ = tab.stream.Close()
	_ = tab.vt.Close()
}
