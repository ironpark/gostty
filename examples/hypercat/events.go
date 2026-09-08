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
	for event, err := range tab.stream.EventValues() {
		if err != nil {
			return err
		}
		switch event.Kind {
		case gostty.StreamEventBell:
			tab.bell = 6 // frames of visual bell
		case gostty.StreamEventPwdChanged:
			tab.pwd = event.Pwd
			tab.retitle()
		case gostty.StreamEventDesktopNotification:
			log.Printf("notification: %s %s", event.Title, event.Body)
		case gostty.StreamEventUnknownSequence:
			log.Printf("unhandled APC: %q", event.Sequence)
		case gostty.StreamEventProgressReport:
			tab.progressReport(event)
		case gostty.StreamEventTitleChanged:
			tab.title = event.Title
			tab.retitle()
		}
	}
	return nil
}

// progressReport applies the report captured with this event.
func (tab *terminalTab) progressReport(event gostty.Event) {
	switch {
	case event.ProgressState == gostty.ProgressStateRemove:
		tab.progress = ""
	case event.HasProgress:
		tab.progress = fmt.Sprintf("%d%%", event.Progress)
	default:
		tab.progress = event.ProgressState.String()
	}
	tab.retitle()
}

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
