package main

import (
	"fmt"
	"log"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/ironpark/gostty"
)

// drainEvents acts on what the program asked of the emulator rather than of the
// screen. libghostty-vt parses OSC; doing something about it is ours.
func (tab *terminalTab) drainEvents() error {
	for event, err := range tab.stream.Events() {
		if err != nil {
			return err
		}
		switch event {
		case gostty.StreamEventBell:
			tab.bell = 6 // frames of visual bell
		case gostty.StreamEventPwdChanged:
			// OSC 7. A real emulator opens new tabs here; this one has none, so
			// the directory rides along in the window title.
			if pwd, ok, err := tab.vt.GetPwd(); err == nil && ok {
				tab.pwd = pwd
				tab.retitle()
			}
		case gostty.StreamEventDesktopNotification:
			// OSC 9 and OSC 777. There is no notification service to hand this
			// to from a window this small, so it goes to the log.
			title, err := tab.stream.EventTitle()
			if err != nil {
				return err
			}
			body, err := tab.stream.EventBody()
			if err != nil {
				return err
			}
			log.Printf("notification: %s %s", title, body)
		case gostty.StreamEventUnknownSequence:
			// An APC nothing here handles. Only visible because capture was
			// turned on at startup.
			data, err := tab.stream.EventSequence()
			if err != nil {
				return err
			}
			log.Printf("unhandled APC: %q", data)
		case gostty.StreamEventProgressReport:
			if err := tab.progressReport(); err != nil {
				return err
			}
		case gostty.StreamEventTitleChanged:
			// The event only announces the change; the value lives on the
			// terminal, which is where ghostty keeps it.
			if title, ok, err := tab.vt.GetTitle(); err == nil && ok {
				tab.title = title
				tab.retitle()
			}
		}
	}
	return nil
}

// progressReport reads OSC 9;4, which a long-running command uses to say how
// far along it is. The state says what to show; the percentage is only there
// for one of them, which is why it comes back with a flag.
func (tab *terminalTab) progressReport() error {
	state, err := tab.stream.EventProgressState()
	if err != nil {
		return err
	}
	percent, ok, err := tab.stream.EventProgress()
	if err != nil {
		return err
	}
	switch {
	case state == gostty.ProgressStateRemove:
		tab.progress = ""
	case ok:
		tab.progress = fmt.Sprintf("%d%%", percent)
	default:
		tab.progress = state.String()
	}
	tab.retitle()
	return nil
}

// retitle rebuilds the window title out of everything the program has told us
// about itself.
func (tab *terminalTab) retitle() {
	if tab.owner != nil && tab.owner.current() != tab {
		return
	}
	title := "gostty"
	if tab.title != "" {
		title += " - " + tab.title
	}
	if tab.pwd != "" {
		title += " (" + tab.pwd + ")"
	}
	if tab.progress != "" {
		title += " [" + tab.progress + "]"
	}
	ebiten.SetWindowTitle(title)
}
