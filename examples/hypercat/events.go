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
			tab.title.pwd = event.Pwd
			tab.retitle()
		case gostty.StreamEventDesktopNotification:
			log.Printf("notification: %s %s", event.Title, event.Body)
		case gostty.StreamEventUnknownSequence:
			log.Printf("unhandled APC: %q", event.Sequence)
		case gostty.StreamEventProgressReport:
			tab.progressReport(event)
		case gostty.StreamEventTitleChanged:
			tab.title.program = event.Title
			tab.retitle()
		}
	}
	return nil
}

// progressReport applies the report captured with this event.
func (tab *terminalTab) progressReport(event gostty.Event) {
	switch {
	case event.ProgressState == gostty.ProgressStateRemove:
		tab.title.progress = ""
	case event.HasProgress:
		tab.title.progress = fmt.Sprintf("%d%%", event.Progress)
	default:
		tab.title.progress = event.ProgressState.String()
	}
	tab.retitle()
}

// windowTitle is what a program has said about itself. The three are kept
// apart rather than folded into one string as they arrive, because they arrive
// separately: a program that sets a title should not lose the directory a
// previous OSC 7 reported.
type windowTitle struct {
	// What the program called itself (OSC 0/2), which is also the tab's label.
	program  string
	pwd      string
	progress string
}

func (t windowTitle) String() string {
	title := "gostty"
	if t.program != "" {
		title += " - " + t.program
	}
	if t.pwd != "" {
		title += " (" + t.pwd + ")"
	}
	if t.progress != "" {
		title += " [" + t.progress + "]"
	}
	return title
}

// retitle names the window after the tab the user is looking at; a background
// tab that renames itself is left to show up in the tab bar alone.
func (tab *terminalTab) retitle() {
	if tab.owner != nil && tab.owner.current() != tab {
		return
	}
	ebiten.SetWindowTitle(tab.title.String())
}
