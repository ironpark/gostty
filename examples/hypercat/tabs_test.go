package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/ironpark/gostty"
	"github.com/ironpark/gostty/examples/hypercat/fonts"
	"github.com/ironpark/gostty/examples/hypercat/keys"
	"github.com/ironpark/gostty/examples/hypercat/thecat"
	"github.com/ironpark/gostty/examples/hypercat/ui"
)

func newTabTestApp(t *testing.T) *terminalApp {
	t.Helper()
	helperShell(t, "echo")
	app := &terminalApp{dsf: 1, settings: &appearance{
		size: fonts.DefaultSize, dsf: 1,
		fonts: &fonts.Set{CellWidth: 10, CellHeight: 20},
	}}
	tab := &terminalTab{
		owner: app, clipboardState: &app.clipboardState, settings: app.settings,
		cols: 80, rows: 24,
	}
	if err := tab.start(); err != nil {
		tab.close()
		t.Fatal(err)
	}
	app.tabs = []*terminalTab{tab}
	t.Cleanup(app.close)
	return app
}

func TestTabsKeepIndependentTerminalsAndShareClipboard(t *testing.T) {
	app := newTabTestApp(t)
	first := app.current()
	first.panels.Search.Query = []rune("first search")
	app.setCatMode(thecat.ModeHyper)
	if err := app.addTab(); err != nil {
		t.Fatal(err)
	}
	second := app.current()
	if first == second || first.vt == second.vt || first.shell == second.shell {
		t.Fatal("tabs share a terminal or shell")
	}
	if len(second.panels.Search.Query) != 0 {
		t.Fatal("search state leaked to new tab")
	}
	if second.settings.cat != thecat.ModeHyper || (second.cat != nil && second.cat.Mode() != thecat.ModeHyper) {
		t.Fatal("new tab did not open in the window's cat mode")
	}
	if second.settings != first.settings {
		t.Fatal("new tab did not share the window's fonts and theme")
	}
	if err := first.stream.Feed([]byte("first")); err != nil {
		t.Fatal(err)
	}
	if err := second.stream.Feed([]byte("second")); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		tab  *terminalTab
		want string
	}{{first, "first"}, {second, "second"}} {
		if err := test.tab.refresh(); err != nil {
			t.Fatal(err)
		}
		for i, r := range test.want {
			if test.tab.cells[i].Codepoint != r {
				t.Fatalf("tab text differs at cell %d", i)
			}
		}
	}
	app.width, app.height = 1000, 634
	app.layoutTabs()
	for _, tab := range app.tabs {
		if tab.cols != 100 || tab.rows != 30 {
			t.Fatalf("tab did not resize: grid=%dx%d", tab.cols, tab.rows)
		}
		assertPtySize(t, tab.shell.pty, 100, 30)
		if tab.offsetY != 34 {
			t.Fatal("terminal pointer offset does not match tab bar")
		}
	}
	first.clipboard = []byte("shared")
	if string(second.pasteText()) != "shared" {
		t.Fatal("fallback clipboard is not shared")
	}
	app.selectTab(0)
	if app.current() != first || string(first.panels.Search.Query) != "first search" {
		t.Fatal("tab state lost on switch")
	}
	app.closeTab(1)
	if app.current() != first || len(app.tabs) != 1 {
		t.Fatal("closing background tab changed the active terminal")
	}
	select {
	case <-second.shell.processDone:
	default:
		t.Fatal("closed tab's shell was not reaped")
	}
}

func TestBackgroundTabProcessesOutputAndExitsIndependently(t *testing.T) {
	skipWithoutShellIO(t)
	app := newTabTestApp(t)
	background := app.current()
	if err := app.addTab(); err != nil {
		t.Fatal(err)
	}
	active := app.current()
	// readOutput must run while this tab is inactive.
	if _, err := background.shell.pty.Write([]byte("background-ready\n")); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for !tabHasText(t, background, "background-ready") {
		if _, err := background.readOutput(); err != nil {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatal("background output was not processed")
		}
		time.Sleep(time.Millisecond)
	}
	if tabHasText(t, active, "background-ready") {
		t.Fatal("background output leaked into active tab")
	}
	assertOSCReachesTheTab(t, background)
	if _, err := background.shell.pty.Write([]byte("exit\n")); err != nil {
		t.Fatal(err)
	}
	for {
		_, err := background.readOutput()
		if errors.Is(err, ebiten.Termination) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatal("background shell did not exit")
		}
		time.Sleep(time.Millisecond)
	}
	app.closeTab(0)
	if app.current() != active || app.active != 0 {
		t.Fatal("closing earlier tab lost active tab")
	}
	app.closeTab(0)
	if app.current() != nil {
		t.Fatal("last tab was not removed")
	}
}

func TestFailedNewTabLeavesExistingTabAlive(t *testing.T) {
	app := newTabTestApp(t)
	existing := app.current()
	t.Setenv(shellVar, filepath.Join(t.TempDir(), "nonexistent-shell"))
	if err := app.addTab(); err == nil {
		t.Fatal("expected shell startup failure")
	}
	if len(app.tabs) != 1 || app.current() != existing {
		t.Fatal("failed tab changed existing tabs")
	}
	if err := existing.stream.Feed([]byte("still alive")); err != nil {
		t.Fatal(err)
	}
}

func TestTabFocusReportsOnlyWhenRequested(t *testing.T) {
	vt, err := gostty.NewTerminal(80, 24)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = vt.Close() })
	output, err := os.CreateTemp(t.TempDir(), "focus")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = output.Close() })
	tab := &terminalTab{vt: vt, shell: &shellSession{pty: fileDevice{output}}}
	// A plain shell must never receive escape sequences merely from switching tabs.
	if err := tab.reportFocus(true); err != nil {
		t.Fatal(err)
	}
	if err := tab.reportFocus(false); err != nil {
		t.Fatal(err)
	}
	if err := vt.SetMode(gostty.ModeFocusEvent, true); err != nil {
		t.Fatal(err)
	}
	if err := tab.reportFocus(true); err != nil {
		t.Fatal(err)
	}
	if err := tab.reportFocus(false); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(output.Name())
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "\x1b[I\x1b[O" {
		t.Fatalf("focus reports = %q", data)
	}
	if tab.focusedFrames != 0 {
		t.Fatal("inactive tab retained its input repeat window")
	}
}

func TestTabShortcuts(t *testing.T) {
	app := newTabTestApp(t)
	press := func(mods keys.Mods, key ebiten.Key) bool {
		t.Helper()
		consumed, err := app.handleTabKeys(mods, func(candidate ebiten.Key) bool { return candidate == key })
		if err != nil {
			t.Fatal(err)
		}
		return consumed
	}
	if press(keys.Mods{}, ebiten.KeyT) {
		t.Fatal("plain T was consumed")
	}
	if !press(keys.Mods{Super: true}, ebiten.KeyT) || len(app.tabs) != 2 || app.active != 1 {
		t.Fatal("Cmd+T did not open a tab")
	}
	if !press(keys.Mods{Super: true}, ebiten.KeyDigit1) || app.active != 0 {
		t.Fatal("Cmd+1 did not switch to first tab")
	}
	if !press(keys.Mods{Ctrl: true, Shift: true}, ebiten.KeyTab) || app.active != 1 {
		t.Fatal("previous tab did not wrap")
	}
	if !press(keys.Mods{Ctrl: true}, ebiten.KeyTab) || app.active != 0 {
		t.Fatal("next tab did not wrap")
	}
	if !press(keys.Mods{Super: true}, ebiten.KeyDigit9) || app.active != 1 {
		t.Fatal("Cmd+9 did not select last tab")
	}
	if !press(keys.Mods{Ctrl: true, Shift: true}, ebiten.KeyW) || len(app.tabs) != 1 {
		t.Fatal("Ctrl+Shift+W did not close tab")
	}
	if !press(keys.Mods{Super: true}, ebiten.KeyW) || len(app.tabs) != 0 {
		t.Fatal("last tab did not close")
	}
}

// A `terminalDevice` that is only ever written to, for the tests that want to
// read back what the tab sent rather than run a shell.
type fileDevice struct{ *os.File }

func (fileDevice) Resize(cols, rows int) error { return nil }

// Whether the tab's grid shows this text anywhere. `refresh` is what fills the
// cells from the terminal, so it is part of looking.
func tabHasText(t *testing.T, tab *terminalTab, want string) bool {
	t.Helper()
	if err := tab.refresh(); err != nil {
		t.Fatal(err)
	}
	var text []rune
	for _, cell := range tab.cells {
		if cell.Codepoint == 0 {
			text = append(text, ' ')
			continue
		}
		text = append(text, rune(cell.Codepoint))
	}
	return strings.Contains(string(text), want)
}

// The settings panel is opened in one tab, but the fonts, the theme, and the
// cat belong to the window: every tab has to be drawn with the change, not just
// the one it was made from.
func TestSettingsChangeAppliesToEveryTab(t *testing.T) {
	app := newTabTestApp(t)
	first := app.current()
	if err := app.addTab(); err != nil {
		t.Fatal(err)
	}
	second := app.current()
	// The change is made from the second tab, with the settings panel open
	// there, exactly as a user would.
	second.panels.OpenSettings()

	second.panels.Settings.Row = ui.SettingTheme
	before := first.currentTheme()
	if _, err := second.applyUIActions(ui.Actions{SettingsDelta: 1}); err != nil {
		t.Fatal(err)
	}
	if first.currentTheme() == before {
		t.Error("the theme changed only in the tab it was changed from")
	}
	if first.currentTheme() != second.currentTheme() {
		t.Errorf("tabs disagree on the theme: %q and %q", first.currentTheme().Name, second.currentTheme().Name)
	}
	if !first.redraw.all {
		t.Error("the other tab was not repainted in the new theme")
	}

	second.panels.Settings.Row = ui.SettingCat
	if _, err := second.applyUIActions(ui.Actions{SettingsDelta: 1}); err != nil {
		t.Fatal(err)
	}
	if first.cat != nil && first.cat.Mode() != app.settings.cat {
		t.Errorf("the other tab's cat is %v, want %v", first.cat.Mode(), app.settings.cat)
	}

	// The test app carries stub metrics rather than real faces, so the font
	// rows need the system fonts the panel would be stepping through.
	app.settings.families = fonts.Discover()
	if len(app.settings.families) == 0 {
		return // no system fonts on this machine
	}
	first.relayout, second.relayout = false, false
	sizeBefore := app.settings.size
	second.panels.Settings.Row = ui.SettingSize
	if _, err := second.applyUIActions(ui.Actions{SettingsDelta: 1}); err != nil {
		t.Fatal(err)
	}
	if app.settings.size == sizeBefore {
		t.Fatal("the font size did not change")
	}
	if first.settings.fonts != second.settings.fonts {
		t.Error("the tabs are drawing with different faces")
	}
	if !first.relayout {
		t.Error("the other tab's grid was not remeasured for the new cell size")
	}
}
