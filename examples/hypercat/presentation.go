package main

import (
	"github.com/ironpark/gostty/examples/hypercat/internal/frontend"
	"github.com/ironpark/gostty/examples/hypercat/ui"
)

var _ frontend.Application = (*window)(nil)

// Present supplies cached terminal data to the window. There are no gostty
// calls here: Ebitengine may draw without an intervening terminal update.
func (win *window) Present() frontend.Presentation {
	tab := win.current()
	if tab == nil {
		return frontend.Presentation{}
	}
	settings := ui.SettingsValues{}
	if tab.panels.Mode == ui.Settings {
		settings = win.settingsValues()
	}
	return frontend.Presentation{
		Renderer: &tab.renderer, Frame: tab.presentation(),
		Panels: &tab.panels, Settings: settings,
		TabBar: &win.tabBar, Active: win.active, Count: len(win.tabs), TabTitle: win.tabTitle,
		Title: win.windowTitle, LinkPointer: tab.reports.focused && tab.frame.link.valid(),
	}
}

// presentation borrows the slices collected by refreshSnapshot. Native read
// bookkeeping remains in frame; frontend receives only what it needs to draw.
func (tab *terminal) presentation() frontend.Frame {
	f := &tab.frame
	return frontend.Frame{
		Cols: tab.cols, Rows: tab.rows, Cells: f.cells, Clusters: f.clusters,
		Matches: tab.search.cells, Blink: f.blink, Damage: &f.redraw,
		Colors: frontend.Colors{
			TerminalBg: f.colors.terminalBg, TerminalFg: f.colors.terminalFg,
			Bg: f.colors.bg, Fg: f.colors.fg,
		},
		Cursor: frontend.Cursor{
			X: f.cursor.x, Y: f.cursor.y, Visible: f.cursor.visible,
			Blinking: f.cursor.blinking, WideTail: f.cursor.wideTail,
			Password: f.cursor.password, Style: f.cursor.style,
			Color: f.cursor.color, HasColor: f.cursor.hasColor,
		},
		Link:      frontend.Link{URI: f.link.uri, Row: f.link.row, Start: f.link.start, End: f.link.end},
		Scrollbar: frontend.Scrollbar{Bar: f.scrollbar.bar, Visible: f.scrollbar.visible},
		Fonts:     tab.fonts(), Emoji: tab.emoji(), Theme: tab.currentTheme(), Scale: tab.settings.dsf,
		Images: tab.images, Cat: tab.cat,
	}
}

func (r *redrawSet) Full() bool { return r.all }
