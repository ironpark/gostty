package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ironpark/gostty"
	"github.com/ironpark/gostty/examples/hypercat/fonts"
	"github.com/ironpark/gostty/examples/hypercat/internal/shelltest"
	"github.com/ironpark/gostty/examples/hypercat/keys"
	"github.com/ironpark/gostty/examples/hypercat/shell"
	"github.com/ironpark/gostty/examples/hypercat/thecat"
	"github.com/ironpark/gostty/examples/hypercat/ui"
	"github.com/ironpark/gostty/input"
	"io"
)

func TestTabsKeepIndependentTerminalsAndShareClipboard(t *testing.T) {
	win := newTabTestApp(t)
	first := win.current()
	first.panels.Search.Query = []rune("first search")
	win.setCatMode(thecat.ModeHyper)
	if err := win.addTab(); err != nil {
		t.Fatal(err)
	}
	second := win.current()
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
		tab  *terminal
		want string
	}{{first, "first"}, {second, "second"}} {
		if err := test.tab.refreshSnapshot(); err != nil {
			t.Fatal(err)
		}
		for i, r := range test.want {
			if test.tab.frame.cells[i].Codepoint != r {
				t.Fatalf("tab text differs at cell %d", i)
			}
		}
	}
	win.width, win.height = 1000, 634
	win.layoutTabs()
	for _, tab := range win.tabs {
		if tab.cols != 100 || tab.rows != 30 {
			t.Fatalf("tab did not resize: grid=%dx%d", tab.cols, tab.rows)
		}
		if tab.offsetY != 34 {
			t.Fatal("terminal pointer offset does not match tab bar")
		}
	}
	first.clipboard.hold([]byte("shared"))
	if string(second.clipboard.paste()) != "shared" {
		t.Fatal("fallback clipboard is not shared")
	}
	win.selectTab(0)
	if win.current() != first || string(first.panels.Search.Query) != "first search" {
		t.Fatal("tab state lost on switch")
	}
	win.closeTab(1)
	if win.current() != first || len(win.tabs) != 1 {
		t.Fatal("closing background tab changed the active terminal")
	}
	select {
	case <-second.shell.Reaped:
	default:
		t.Fatal("closed tab's shell was not reaped")
	}
}

func TestBackgroundTabProcessesOutputAndExitsIndependently(t *testing.T) {
	shelltest.SkipWithoutIO(t)
	win := newTabTestApp(t)
	background := win.current()
	if err := win.addTab(); err != nil {
		t.Fatal(err)
	}
	active := win.current()
	// readOutput must run while this tab is inactive.
	if _, err := background.shell.Pty.Write([]byte("background-ready\n")); err != nil {
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
	if _, err := background.shell.Pty.Write([]byte("exit\n")); err != nil {
		t.Fatal(err)
	}
	for {
		_, err := background.readOutput()
		if errors.Is(err, io.EOF) {
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
	win.closeTab(0)
	if win.current() != active || win.active != 0 {
		t.Fatal("closing earlier tab lost active tab")
	}
	win.closeTab(0)
	if win.current() != nil {
		t.Fatal("last tab was not removed")
	}
}

func TestFailedNewTabLeavesExistingTabAlive(t *testing.T) {
	win := newTabTestApp(t)
	existing := win.current()
	t.Setenv(shell.Var, filepath.Join(t.TempDir(), "nonexistent-shell"))
	if err := win.addTab(); err == nil {
		t.Fatal("expected shell startup failure")
	}
	if len(win.tabs) != 1 || win.current() != existing {
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
	tab := &terminal{vt: vt, shell: &shell.Session{Pty: fileDevice{output}}}
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
	if tab.reports.focusedFrames != 0 {
		t.Fatal("inactive tab retained its input repeat window")
	}
}

func TestTabShortcuts(t *testing.T) {
	win := newTabTestApp(t)
	press := func(mods keys.Mods, key input.Key) bool {
		t.Helper()
		consumed, err := win.handleTabKeys(mods, func(candidate input.Key) bool { return candidate == key })
		if err != nil {
			t.Fatal(err)
		}
		return consumed
	}
	if press(keys.Mods{}, input.KeyKeyT) {
		t.Fatal("plain T was consumed")
	}
	if !press(keys.Mods{Super: true}, input.KeyKeyT) || len(win.tabs) != 2 || win.active != 1 {
		t.Fatal("Cmd+T did not open a tab")
	}
	if !press(keys.Mods{Super: true}, input.KeyDigit1) || win.active != 0 {
		t.Fatal("Cmd+1 did not switch to first tab")
	}
	if !press(keys.Mods{Ctrl: true, Shift: true}, input.KeyTab) || win.active != 1 {
		t.Fatal("previous tab did not wrap")
	}
	if !press(keys.Mods{Ctrl: true}, input.KeyTab) || win.active != 0 {
		t.Fatal("next tab did not wrap")
	}
	if !press(keys.Mods{Super: true}, input.KeyDigit9) || win.active != 1 {
		t.Fatal("Cmd+9 did not select last tab")
	}
	if !press(keys.Mods{Ctrl: true, Shift: true}, input.KeyKeyW) || len(win.tabs) != 1 {
		t.Fatal("Ctrl+Shift+W did not close tab")
	}
	if !press(keys.Mods{Super: true}, input.KeyKeyW) || len(win.tabs) != 0 {
		t.Fatal("last tab did not close")
	}
}

// A `shell.Device` that is only ever written to, for the tests that want to
// read back what the tab sent rather than run a shell.
type fileDevice struct{ *os.File }

func (fileDevice) Resize(cols, rows int) error { return nil }

// Whether the tab's grid shows this text anywhere. `refreshSnapshot` is what fills the
// cells from the terminal, so it is part of looking.
func tabHasText(t *testing.T, tab *terminal, want string) bool {
	t.Helper()
	if err := tab.refreshSnapshot(); err != nil {
		t.Fatal(err)
	}
	var text []rune
	for _, cell := range tab.frame.cells {
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
	win := newTabTestApp(t)
	first := win.current()
	if err := win.addTab(); err != nil {
		t.Fatal(err)
	}
	second := win.current()
	// The change is made from the second tab, with the settings panel open
	// there, exactly as a user would.
	second.panels.OpenSettings()

	second.panels.Settings.Row = ui.SettingTheme
	before := first.currentTheme().Name
	if _, err := win.applyPanel(second, ui.Actions{SettingsDelta: 1}); err != nil {
		t.Fatal(err)
	}
	if first.currentTheme().Name == before {
		t.Error("the theme changed only in the tab it was changed from")
	}
	if first.currentTheme().Name != second.currentTheme().Name {
		t.Errorf("tabs disagree on the theme: %q and %q", first.currentTheme().Name, second.currentTheme().Name)
	}
	if !first.frame.redraw.all {
		t.Error("the other tab was not repainted in the new theme")
	}

	second.panels.Settings.Row = ui.SettingCat
	if _, err := win.applyPanel(second, ui.Actions{SettingsDelta: 1}); err != nil {
		t.Fatal(err)
	}
	if first.cat != nil && first.cat.Mode() != win.settings.cat {
		t.Errorf("the other tab's cat is %v, want %v", first.cat.Mode(), win.settings.cat)
	}

	// The test window carries stub metrics rather than real faces, so the font
	// rows need the system fonts the panel would be stepping through.
	win.settings.families = fonts.Discover()
	if len(win.settings.families) == 0 {
		return // no system fonts on this machine
	}
	first.relayout, second.relayout = false, false
	sizeBefore := win.settings.size
	second.panels.Settings.Row = ui.SettingSize
	if _, err := win.applyPanel(second, ui.Actions{SettingsDelta: 1}); err != nil {
		t.Fatal(err)
	}
	if win.settings.size == sizeBefore {
		t.Fatal("the font size did not change")
	}
	if first.settings.fonts != second.settings.fonts {
		t.Error("the tabs are drawing with different faces")
	}
	if !first.relayout {
		t.Error("the other tab's grid was not remeasured for the new cell size")
	}
}

func TestCloseTabPreservesOrActivatesCurrentTab(t *testing.T) {
	for _, tt := range []struct {
		name                  string
		count, active, closed int
		wantIndex             int
	}{
		{"background before active", 3, 1, 0, 0},
		{"background after active", 3, 1, 2, 1},
		{"active first", 3, 0, 0, 0},
		{"active middle", 3, 1, 1, 1},
		{"active last", 3, 2, 2, 1},
		{"only tab", 1, 0, 0, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			win := &window{active: tt.active}
			for range tt.count {
				win.tabs = append(win.tabs, &terminal{})
			}
			t.Cleanup(win.close)
			current := win.current()
			current.sel.dragging = true
			current.reports.mouseGrabbed = true
			current.reports.focusedFrames = 12
			win.closeTab(tt.closed)
			if len(win.tabs) != tt.count-1 || win.active != tt.wantIndex {
				t.Fatalf("tabs=%d active=%d, want tabs=%d active=%d", len(win.tabs), win.active, tt.count-1, tt.wantIndex)
			}
			if tt.count == 1 {
				if win.current() != nil {
					t.Fatal("closing the last tab retained the current tab")
				}
				return
			}
			if tt.closed == tt.active {
				if win.current() == current || !win.current().frame.redraw.all {
					t.Fatal("replacement tab was not activated for a full redraw")
				}
				return
			}
			if win.current() != current || !current.sel.dragging || !current.reports.mouseGrabbed || current.reports.focusedFrames != 12 || current.frame.redraw.all {
				t.Fatal("closing a background tab reset the active tab's interaction state")
			}
		})
	}
}
