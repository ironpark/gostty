package main

import (
	"github.com/ironpark/gostty"
	"github.com/ironpark/gostty/examples/hypercat/shell"
)

func (tab *terminalTab) start() error {
	var err error
	if tab.vt, err = gostty.NewTerminal(uint16(tab.cols), uint16(tab.rows)); err != nil {
		return err
	}
	// The stream must be closed before the terminal: its handler reaches
	// through the terminal for an allocator when it tears down.
	if tab.stream, err = tab.vt.NewStream(0); err != nil {
		return err
	}
	if tab.state, err = gostty.NewRenderState(); err != nil {
		return err
	}
	if tab.images, err = newImageCache(tab.vt); err != nil {
		return err
	}

	if err := tab.configureStream(); err != nil {
		return err
	}
	tab.shell, err = shell.Start(tab.cols, tab.rows)
	if err != nil {
		return err
	}
	tab.startCat()
	return nil
}

func (tab *terminalTab) close() {
	tab.shell.Close()
	// Reverse construction order; the stream is a child of the terminal, and
	// closing the terminal first would be refused. The search is a child of a
	// screen, which is borrowed from the terminal, so it goes first of all.
	tab.closeSearch()
	tab.grid.close()
	tab.images.close()
	_ = tab.state.Close()
	_ = tab.stream.Close()
	_ = tab.vt.Close()
}

func (tab *terminalTab) configureStream() error {
	// A clipboard write must be answered from inside the callback: it runs
	// while Feed is still on the stack and the program is blocked on it.
	if err := tab.stream.OnClipboardWriteRequest(func(req *gostty.ClipboardRequest) {
		n, err := req.ContentCount()
		if err == nil && n > 0 {
			if data, err := req.ContentData(0); err == nil {
				tab.clipboard.hold(data)
			}
		}
		_ = req.Allow(false)
	}); err != nil {
		return err
	}
	if err := tab.stream.OnClipboardReadRequest(func(req *gostty.ClipboardRequest) {
		// A read hands the running program whatever the user copied, so a real
		// emulator would ask the user first. This one answers immediately,
		// which is the wrong default for anything but a demo.
		_ = req.ReplyText(string(tab.clipboard.paste()), false)
	}); err != nil {
		return err
	}

	// Tell programs who they are talking to. Without this XTVERSION names the
	// parser ("libghostty"), which is true and useless: a program checking
	// whether the terminal supports something wants the emulator's identity.
	if err := tab.stream.SetVersionReport("hypercat", "0.1.0"); err != nil {
		return err
	}
	// The color scheme answers CSI ? 996 n and, once a program subscribes with
	// mode 2031, is reported the moment it changes. A real emulator would hook
	// this to the OS appearance notification; this one only knows its own
	// theme, so it says that.
	if err := tab.stream.ColorSchemeChanged(tab.colorScheme()); err != nil {
		return err
	}
	// Capture the sequences this library does not implement, so a program
	// speaking a graphics protocol hypercat never wired up says so in the log
	// instead of drawing nothing.
	if err := tab.stream.SetUnknownMaxBytes(256); err != nil {
		return err
	}

	return nil
}
