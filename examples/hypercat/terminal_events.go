package main

import (
	"fmt"
	"log"

	"github.com/ironpark/gostty"
)

// drainEvents acts on what the program asked of the emulator rather than of the
// screen. libghostty-vt parses OSC; doing something about it is ours.
func (tab *terminal) drainEvents() error {
	for event, err := range tab.stream.EventValues() {
		if err != nil {
			return err
		}
		switch event.Kind {
		case gostty.StreamEventBell:
			tab.renderer.Bell = 6 // frames of visual bell
		case gostty.StreamEventPwdChanged:
			tab.title.pwd = event.Pwd
		case gostty.StreamEventDesktopNotification:
			log.Printf("notification: %s %s", event.Title, event.Body)
		case gostty.StreamEventUnknownSequence:
			log.Printf("unhandled APC: %q", event.Sequence)
		case gostty.StreamEventProgressReport:
			tab.progressReport(event)
		case gostty.StreamEventTitleChanged:
			tab.title.program = event.Title
		}
	}
	return nil
}

// progressReport applies the report captured with this event.
func (tab *terminal) progressReport(event gostty.Event) {
	switch {
	case event.ProgressState == gostty.ProgressStateRemove:
		tab.title.progress = ""
	case event.HasProgress:
		tab.title.progress = fmt.Sprintf("%d%%", event.Progress)
	default:
		tab.title.progress = event.ProgressState.String()
	}
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

// String is what the window is called while this tab is the visible one: the
// emulator's name, and whatever the program has said on top of it.
func (t windowTitle) String() string {
	title := appName
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

// Clipboard requests must be answered inside the callback, while Feed runs.
func (tab *terminal) writeClipboard(req *gostty.ClipboardRequest) {
	n, err := req.ContentCount()
	if err == nil && n > 0 {
		if data, err := req.ContentData(0); err == nil {
			tab.clipboard.hold(data)
		}
	}
	_ = req.Allow(false)
}

// This demo immediately shares clipboard contents with the running program.
// A production emulator should ask for permission before answering OSC 52 reads.
func (tab *terminal) readClipboard(req *gostty.ClipboardRequest) {
	_ = req.ReplyText(string(tab.clipboard.paste()), false)
}
