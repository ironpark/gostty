// Package desktop provides desktop clipboard and URL services.
package desktop

import (
	"context"
	"log"

	"golang.design/x/clipboard"
)

// Clipboard is the window's clipboard, shared by every tab.
//
// The system one when there is one -- a headless Linux box has none, which is
// a degradation rather than a failure -- and otherwise a process-local buffer,
// which at least lets OSC 52 and paste agree. It keeps bytes, because that is
// what the system clipboard and OSC 52 both deal in.
type Clipboard struct {
	local  []byte
	system bool
}

// Init falls back to the process-local clipboard when no system service exists.
func (c *Clipboard) Init() {
	if err := clipboard.Init(); err != nil {
		log.Printf("no system clipboard, staying in-process: %v", err)
		return
	}
	c.system = true
}

// Hold keeps bytes for this process alone. This is what a program writing
// OSC 52 gets: it asked the terminal to remember something, and putting that
// on the user's system clipboard unasked is not the terminal's to decide.
func (c *Clipboard) Hold(data []byte) { c.local = append(c.local[:0], data...) }

// Copy is the user's own copy, which does reach the system clipboard.
func (c *Clipboard) Copy(text string) error {
	c.Hold([]byte(text))
	if !c.system {
		return nil
	}
	_, err := clipboard.Write(context.Background(), clipboard.FmtText, c.local)
	return err
}

// Paste reads the system clipboard if there is one, and otherwise whatever was
// last held.
func (c *Clipboard) Paste() []byte {
	if c.system {
		if text, err := clipboard.Read(context.Background(), clipboard.FmtText); err == nil && len(text) > 0 {
			return text
		}
	}
	return c.local
}
