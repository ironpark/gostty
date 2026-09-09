package main

import (
	"github.com/ironpark/gostty"
	"github.com/ironpark/gostty/examples/hypercat/fonts"
	"github.com/ironpark/gostty/examples/hypercat/keys"
	"github.com/ironpark/gostty/examples/hypercat/shell"
	"github.com/ironpark/gostty/examples/hypercat/thecat"
	"github.com/ironpark/gostty/examples/hypercat/ui"
)

// terminalTab owns one terminal, shell, and viewport. Shared appearance and
// clipboard come from the app; UI actions flow back without an app reference.
type terminalTab struct {
	// gostty resources and the shell producing/consuming terminal bytes.
	vt     *gostty.Terminal
	stream *gostty.Stream
	state  *gostty.RenderState
	shell  *shell.Session

	// Draw consumes the snapshot and caches; it never reads native state.
	frame  frame
	layers gridCanvas
	images *imageCache

	// Grid dimensions and position below the window's tab bar.
	cols, rows int
	offsetY    int
	relayout   bool // font changes require Layout even at the same window size

	// Per-tab interactions and optional features.
	sel     selection
	search  tabSearch
	panels  ui.Panels
	reports reportState
	title   windowTitle
	bell    int
	cat     *thecat.Companion

	// Shared window resources.
	settings  *appearance
	clipboard *sharedClipboard

	// Reused keyboard, panel text, and encoder buffers.
	keys        keys.Reader
	chars       []rune
	out, report frameBuffer
}

// onScreen borrows the currently active screen for one operation.
// Fetch it each time because programs can switch between primary and alternate screens.
func (tab *terminalTab) onScreen(f func(*gostty.Screen) error) error {
	screen, err := tab.vt.ActiveScreen()
	if err != nil {
		return err
	}
	return f(screen)
}

// fonts and emoji are the window's, shared with every other tab: what a tab
// draws with follows the settings panel wherever it was opened.
func (tab *terminalTab) fonts() *fonts.Set   { return tab.settings.fonts }
func (tab *terminalTab) emoji() *fonts.Emoji { return tab.settings.emoji }

// Tab switches reset interaction state and invalidate the visible grid.
func (tab *terminalTab) activate() {
	tab.reports.focusedFrames = 0
	tab.frame.redraw.markAll()
}

func (tab *terminalTab) deactivate() {
	tab.endGesture()
	tab.reports.mouseGrabbed = false
	tab.cat.ClearHover()
}
