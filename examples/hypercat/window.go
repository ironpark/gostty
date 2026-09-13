package main

import (
	"errors"
	"io"
	"log"
	"slices"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/ironpark/gostty/examples/hypercat/keys"
	"github.com/ironpark/gostty/examples/hypercat/thecat"
	"github.com/ironpark/gostty/examples/hypercat/ui"
	"github.com/ironpark/gostty/input"
)

// window is the Ebitengine game: it runs the frame loop, owns the tab list,
// and keeps what every tab shares -- the settings, the clipboard and the cat.
// Each tab is a terminal with its own shell, scrollback, selection and search.
type window struct {
	tabs   []*terminal
	active int
	tabBar ui.TabBar

	// The window in device pixels, and the device scale factor.
	width, height, dsf float64
	// The terminal area below the tab bar, drawn once a frame and composed
	// onto the screen.
	content *ebiten.Image

	poll inputPoller // reads Ebitengine's state at the start of each update
	host hostInput   // this update's input

	settings  *settings
	clipboard clipboard
	cat       *thecat.Companion // one companion for the whole window

	// The title the window shows, and the tab and title it was formatted
	// from: it is only formatted again when a program renames itself.
	title       string
	titled      windowTitle
	titledTab   int
	shownTitle  string
	linkPointer bool
}

var _ ebiten.Game = (*window)(nil)

// newWindow discovers the fonts once, here, rather than once per tab.
func newWindow() *window {
	dsf := deviceScale()
	return &window{dsf: dsf, settings: defaultSettings(dsf), title: appName}
}

func (win *window) close() {
	for _, tab := range win.tabs {
		tab.close()
	}
	win.tabs, win.active, win.cat = nil, 0, nil
	if win.content != nil {
		win.content.Deallocate()
		win.content = nil
	}
}

// Frame loop -----------------------------------------------------------------

// Update is Ebitengine's; the work is in update, which the tests drive with
// input of their own.
func (win *window) Update() error { return win.update(win.poll.read()) }

// update runs one frame: every tab is serviced, the window takes the input
// that is its own, and what is left over goes to the tab the user is looking
// at. Returning ebiten.Termination ends the loop after the last tab closes.
func (win *window) update(in hostInput) error {
	win.host = in
	if err := win.serviceTabs(); err != nil {
		return err
	}
	if len(win.tabs) == 0 {
		return ebiten.Termination
	}
	tabInputConsumed, err := win.routeInput(in.Mods, in.Focused)
	if err != nil {
		return err
	}
	// Snapshot after input so a selection is visible immediately. The cat
	// then walks on the same cells Draw will paint.
	tab := win.current()
	if err := tab.refreshSnapshot(); err != nil {
		return err
	}
	win.syncCatWorld()
	if !tabInputConsumed {
		win.updateCat()
	}
	return nil
}

// serviceTabs feeds every tab what its shell wrote, background ones included,
// and drops the tabs whose shell has exited.
func (win *window) serviceTabs() error {
	for i := 0; i < len(win.tabs); {
		tab := win.tabs[i]
		fed, err := tab.readOutput()
		if errors.Is(err, io.EOF) {
			win.closeTab(i)
			continue
		}
		if err != nil {
			return err
		}
		// New output is new scrollback, which an open search has to rescan.
		if fed && tab.panels.Mode == ui.Search {
			if err := tab.runSearch(); err != nil {
				return err
			}
		}
		i++
	}
	return nil
}

// routeInput applies input priority: tab shortcuts and the tab bar, then the
// panels, then the terminal. It reports whether tab input consumed the frame;
// those frames refresh the snapshot without advancing the cat.
func (win *window) routeInput(m keys.Mods, focused bool) (bool, error) {
	consumed := false
	if focused {
		var err error
		if consumed, err = win.handleTabs(m); err != nil {
			log.Printf("new tab: %v", err)
		}
		if len(win.tabs) == 0 {
			return consumed, ebiten.Termination
		}
	}
	for i, tab := range win.tabs {
		tab.input = win.host
		if err := tab.reportFocus(i == win.active && focused); err != nil {
			return consumed, err
		}
	}

	tab := win.current()
	win.syncTitle(tab)
	if consumed {
		return true, nil
	}
	panelTook := false
	if focused {
		var err error
		if panelTook, err = win.applyPanel(tab, tab.panels.Handle(win.panelInput(tab))); err != nil {
			return false, err
		}
	}
	return false, tab.updateInput(m, panelTook, focused && win.pokeCat())
}

// applyPanel gives a panel result its effect in two halves: the tab does what
// is its own (the native search) and the window applies a settings step,
// because the font, theme and cat the panel steps through are every tab's.
func (win *window) applyPanel(tab *terminal, result ui.Actions) (bool, error) {
	consumed, err := tab.applyUIActions(result)
	if err != nil || result.SettingsDelta == 0 {
		return consumed, err
	}
	return consumed, win.settingsAdjust(tab.panels.Settings.Row, result.SettingsDelta)
}

// panelInput omits typed text unless search already owned the keyboard.
func (win *window) panelInput(tab *terminal) ui.Input {
	in := win.host.Panel
	if tab.panels.Mode != ui.Search {
		in.Chars = nil
	}
	return in
}

// Draw composes the frame: the active tab's content, its panels, and the tab
// bar. No gostty call happens here -- Ebitengine may draw without an
// intervening update, so everything drawn was read into the frame by update.
func (win *window) Draw(screen *ebiten.Image) {
	tab := win.current()
	if tab == nil {
		return
	}
	if win.title != win.shownTitle {
		win.shownTitle = win.title
		ebiten.SetWindowTitle(win.title)
	}
	if pointer := tab.reports.focused && tab.frame.link.valid(); pointer != win.linkPointer {
		win.linkPointer = pointer
		shape := ebiten.CursorShapeDefault
		if pointer {
			shape = ebiten.CursorShapePointer
		}
		ebiten.SetCursorShape(shape)
	}

	bar := int(win.barHeight())
	width, height := screen.Bounds().Dx(), max(screen.Bounds().Dy()-bar, 1)
	if win.content == nil || win.content.Bounds().Dx() != width || win.content.Bounds().Dy() != height {
		if win.content != nil {
			win.content.Deallocate()
		}
		win.content = ebiten.NewImage(width, height)
	}
	tab.draw(win.content, win.cat)
	canvas := tab.canvas(win.content)
	values := ui.SettingsValues{}
	if tab.panels.Mode == ui.Settings {
		values = win.settingsValues()
	}
	tab.panels.Draw(canvas, values)

	screen.Fill(tab.currentTheme().Panel)
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Translate(0, float64(bar))
	screen.DrawImage(win.content, op)
	canvas.Screen, canvas.Width, canvas.Height = screen, win.width, float64(bar)
	win.tabBar.Draw(canvas, win.active, len(win.tabs), win.tabTitle)
}

// Layout and LayoutF receive the window size in logical pixels and answer in
// device pixels, so a HiDPI display is drawn at its own resolution.
func (win *window) Layout(width, height int) (int, int) {
	w, h := win.LayoutF(float64(width), float64(height))
	return int(w), int(h)
}

func (win *window) LayoutF(width, height float64) (float64, float64) {
	win.dsf = deviceScale()
	win.width, win.height = width*win.dsf, height*win.dsf
	win.layoutTabs()
	return win.width, win.height
}

// layoutTabs sizes every tab to the content area below the tab bar.
func (win *window) layoutTabs() {
	// The faces are built in device pixels, so a window that moved to a
	// display with a different scale factor needs them rebuilt: once, for all.
	if win.settings.dsf != win.dsf {
		win.applyFont()
	}
	for _, tab := range win.tabs {
		tab.offsetY = int(win.barHeight())
		if win.width > 0 {
			tab.layout(win.width, max(win.height-win.barHeight(), 1))
		}
	}
}

func (win *window) barHeight() float64 { return float64(int(ui.TabBarHeight * win.dsf)) }

// deviceScale is device pixels per logical pixel, or one before a monitor is
// known. LayoutF supplies the current scale on later resizes.
func deviceScale() float64 {
	if monitor := ebiten.Monitor(); monitor != nil {
		if scale := monitor.DeviceScaleFactor(); scale > 0 {
			return scale
		}
	}
	return 1
}

// Tabs -----------------------------------------------------------------------

func (win *window) current() *terminal {
	if len(win.tabs) == 0 {
		return nil
	}
	return win.tabs[win.active]
}

func (win *window) addTab() error {
	// A new tab starts at the visible tab's grid size, before Layout has
	// measured it; the fonts, theme and clipboard are the window's.
	tab := &terminal{
		clipboard: &win.clipboard, settings: win.settings,
		cols: initialCols, rows: initialRows,
	}
	if previous := win.current(); previous != nil {
		tab.cols, tab.rows = previous.cols, previous.rows
	}
	if err := tab.start(); err != nil {
		return err
	}
	win.tabs = append(win.tabs, tab)
	win.selectTab(len(win.tabs) - 1)
	win.layoutTabs()
	return nil
}

func (win *window) selectTab(index int) {
	if index < 0 || index >= len(win.tabs) {
		return
	}
	if old := win.current(); old != nil {
		old.deactivate()
	}
	win.active = index
	win.current().activate()
	win.cat.ClearHover()
	win.syncCatWorld()
}

// cycleTab selects the tab `step` away from the active one, wrapping.
func (win *window) cycleTab(step int) { win.selectTab(wrap(win.active+step, len(win.tabs))) }

// closeTab keeps the active tab's interaction state when a background shell
// exits; only closing the active tab activates its replacement.
func (win *window) closeTab(index int) {
	if index < 0 || index >= len(win.tabs) {
		return
	}
	wasActive := index == win.active
	win.tabs[index].close()
	win.tabs = slices.Delete(win.tabs, index, index+1)
	if index < win.active {
		win.active--
	}
	win.active = max(min(win.active, len(win.tabs)-1), 0)
	if !wasActive {
		return
	}
	if tab := win.current(); tab != nil {
		tab.activate()
	}
	win.cat.ClearHover()
	win.syncCatWorld()
}

// handleTabs consumes the window's own input -- tab shortcuts and the tab
// bar -- before it can reach a shell, and reports whether it did.
func (win *window) handleTabs(m keys.Mods) (bool, error) {
	if consumed, err := win.handleTabKeys(m, win.host.KeyPressed); consumed || err != nil {
		return consumed, err
	}
	x, y := win.host.X, win.host.Y
	if y < 0 || y >= int(win.barHeight()) {
		return false, nil
	}
	if win.host.Left.Pressed {
		layout := win.tabBar.Layout(win.width, win.dsf, win.active, len(win.tabs))
		switch action := layout.Hit(float64(x), float64(y)); action.Kind {
		case ui.TabAdd:
			return true, win.addTab()
		case ui.TabClose:
			win.closeTab(action.Index)
		case ui.TabSelect:
			win.selectTab(action.Index)
		}
		return true, nil
	}
	switch wheel := win.host.Wheel; {
	case wheel > 0:
		win.cycleTab(-1)
	case wheel < 0:
		win.cycleTab(1)
	default:
		return false, nil
	}
	return true, nil
}

// handleTabKeys consumes window shortcuts before they can reach a shell.
func (win *window) handleTabKeys(m keys.Mods, pressed func(input.Key) bool) (bool, error) {
	if m.Shortcut() {
		switch {
		case pressed(input.KeyKeyT):
			return true, win.addTab()
		case pressed(input.KeyKeyW):
			win.closeTab(win.active)
			return true, nil
		}
		digits := [...]input.Key{input.KeyDigit1, input.KeyDigit2, input.KeyDigit3, input.KeyDigit4, input.KeyDigit5, input.KeyDigit6, input.KeyDigit7, input.KeyDigit8, input.KeyDigit9}
		for i, key := range digits {
			if pressed(key) {
				if i == 8 {
					i = len(win.tabs) - 1 // Cmd+9 is the last tab
				}
				win.selectTab(i)
				return true, nil
			}
		}
	}
	if m.Ctrl && pressed(input.KeyTab) {
		if m.Shift {
			win.cycleTab(-1)
		} else {
			win.cycleTab(1)
		}
		return true, nil
	}
	return false, nil
}

// syncTitle names the window after the visible tab; a background tab that
// renames itself only shows up in the tab bar.
func (win *window) syncTitle(tab *terminal) {
	if tab.title == win.titled && win.active == win.titledTab {
		return
	}
	win.titled, win.titledTab = tab.title, win.active
	win.title = tab.title.String()
}

func (win *window) tabTitle(index int) string { return win.tabs[index].title.program }

// wrap brings an index back into [0, n).
func wrap(i, n int) int { return (i%n + n) % n }

// The cat ------------------------------------------------------------------

// catGrid describes the active tab's cells to the companion, which walks on
// the text and treats stacked lines as walls.
func (tab *terminal) catGrid() thecat.GridWorld {
	g := tab.grid()
	return thecat.GridWorld{
		Cols: g.cols, Rows: g.rows,
		CellWidth: g.cellW, CellHeight: g.cellH,
		HasInk: func(col, row int) bool {
			if !g.holds(len(tab.frame.cells)) {
				return false
			}
			cell := tab.frame.cells[g.index(col, row)]
			return cell.Codepoint > ' ' && !cell.Flags.Invisible
		},
	}
}

// startCat creates the window's companion after the first terminal opens.
func (win *window) startCat() {
	tab := win.current()
	if tab == nil || win.cat != nil {
		return
	}
	cat, err := thecat.NewCompanion(tab.catGrid())
	if err != nil {
		log.Printf("no cat: %v", err)
		return
	}
	win.cat = cat
	win.cat.SetMode(win.settings.cat)
}

// syncCatWorld follows the visible tab without resetting position or
// animation. An empty world when the last tab closes releases its cells too.
func (win *window) syncCatWorld() {
	if win.cat == nil {
		return
	}
	if tab := win.current(); tab != nil {
		win.cat.GridWorld = tab.catGrid()
	} else {
		win.cat.GridWorld = thecat.GridWorld{}
	}
}

func (win *window) updateCat() {
	tab := win.current()
	x, y := tab.cursorPosition()
	win.cat.Update(tab.catGrid(), x, y, win.host.DeltaSeconds)
}

// pokeCat opens settings in the active tab when the cat is clicked, and
// reports that the click was taken so no selection starts under it.
func (win *window) pokeCat() bool {
	if !win.host.Left.Pressed {
		return false
	}
	tab := win.current()
	x, y := tab.cursorPosition()
	if y < 0 || !win.cat.PokeAt(x, y) {
		return false
	}
	tab.panels.OpenSettings()
	return true
}
