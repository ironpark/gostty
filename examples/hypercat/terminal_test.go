package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ironpark/gostty"
	"github.com/ironpark/gostty/examples/hypercat/shell"
	"github.com/ironpark/gostty/examples/hypercat/ui"
)

func TestClosePartiallyInitializedTerminalTab(t *testing.T) {
	tab := &terminal{}
	tab.close()
	var err error
	tab.vt, err = gostty.NewTerminal(80, 24)
	if err != nil {
		t.Fatal(err)
	}
	tab.stream, err = tab.vt.NewStream(0)
	if err != nil {
		tab.close()
		t.Fatal(err)
	}
	tab.close()
	// Close would leave the terminal alive if its stream were still a child.
	if _, err := tab.vt.NewStream(0); err == nil {
		t.Fatal("terminal remained open after cleanup")
	}
	tab.close()
}

// Fail after native resources have been acquired, before the shell can start.
func TestStartFailureClosesTerminalResources(t *testing.T) {
	t.Setenv(shell.Var, filepath.Join(t.TempDir(), "missing-shell"))
	tab := &terminal{
		cols: 80, rows: 24,
		settings:  testSettings(),
		clipboard: &clipboard{},
	}
	t.Cleanup(tab.close)
	if err := tab.start(); err == nil {
		t.Fatal("starting a missing shell succeeded")
	}
	if tab.vt == nil || tab.stream == nil || tab.state == nil || tab.images == nil {
		t.Fatal("startup failed before acquiring native resources")
	}
	if err := tab.stream.Feed([]byte("closed")); err == nil {
		t.Error("stream remained open after startup failure")
	}
	if _, err := tab.state.CellCount(); err == nil {
		t.Error("render state remained open after startup failure")
	}
	if tab.sel.gesture != nil {
		t.Error("selection gesture remained open after startup failure")
	}
	if stream, err := tab.vt.NewStream(0); err == nil {
		_ = stream.Close()
		t.Error("terminal remained open after startup failure")
	}
}

// The scrollback is bounded by the tab, not by the library: the native default
// is unbounded, which a window with a tab bar cannot afford.
func TestScrollbackIsBounded(t *testing.T) {
	win := newTabTestApp(t)
	tab := win.current()

	cfg := tab.terminalConfig()
	if cfg.ScrollbackMaxLines == nil || *cfg.ScrollbackMaxLines == 0 {
		t.Error("no scrollback line limit was configured")
	}
	if cfg.ScrollbackMaxBytes == nil || *cfg.ScrollbackMaxBytes == 0 {
		t.Error("no scrollback byte limit was configured")
	}
}

// The scrollbar's three numbers are the terminal's: how much there is, how
// much is on screen, and where the screen sits in it.
func TestScrollbarFollowsTheViewport(t *testing.T) {
	win := newTabTestApp(t)
	tab := win.current()

	feedTab(t, tab, "idle")
	if tab.frame.scrollbar.visible > 0 {
		t.Error("a screen with no scrollback showed a scrollbar")
	}

	feedTab(t, tab, strings.Repeat("line\r\n", 100))
	bottom := tab.frame.scrollbar.bar
	if bottom.Total <= bottom.Len {
		t.Fatalf("Scrollbar() = %+v, want more rows than the viewport", bottom)
	}
	if bottom.Offset+bottom.Len != bottom.Total {
		t.Errorf("Scrollbar() = %+v, want the thumb at the bottom", bottom)
	}

	if err := tab.vt.ScrollViewport(gostty.ScrollViewportTop()); err != nil {
		t.Fatalf("ScrollViewport: %v", err)
	}
	if err := tab.refreshSnapshot(); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if tab.frame.scrollbar.bar.Offset != 0 {
		t.Errorf("Scrollbar().Offset at the top = %d, want 0", tab.frame.scrollbar.bar.Offset)
	}
	if tab.frame.scrollbar.visible <= 0 {
		t.Error("no scrollbar while the viewport is away from the bottom")
	}
}

// The scrollback is written out by the formatter, which keeps the styles and
// unwraps the soft wraps. A loop over the cells would keep neither.
func TestExportScrollbackWritesStyledHTML(t *testing.T) {
	win := newTabTestApp(t)
	tab := win.current()
	wantColor := gostty.NewRGB(0x12, 0x34, 0x56)
	if err := tab.vt.SetPaletteColor(1, wantColor); err != nil {
		t.Fatal(err)
	}
	feedTab(t, tab, "\x1b[31mred\x1b[0m plain")

	var out bytes.Buffer
	if err := tab.formatScrollback(&out); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if !strings.Contains(text, "red") || !strings.Contains(text, "plain") {
		t.Errorf("the export is missing the screen: %q", text)
	}
	if !strings.Contains(text, "<") {
		t.Errorf("the export carries no markup, so the colours were lost: %q", text)
	}
	if !strings.Contains(strings.ToLower(text), wantColor.String()) {
		t.Errorf("the export did not resolve palette color %s: %q", wantColor, text)
	}
}

func TestExportFormatsOnlyTheSelection(t *testing.T) {
	win := newTabTestApp(t)
	tab := win.current()
	feedTab(t, tab, "selected omitted")
	if _, err := screenOf(t, tab).SetSelection(gostty.Selection{StartX: 0, StartY: 0, EndX: 7, EndY: 0}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := tab.formatScrollback(&out); err != nil {
		t.Fatal(err)
	}
	if text := out.String(); !strings.Contains(text, "selected") || strings.Contains(text, "omitted") {
		t.Fatalf("selection export = %q", text)
	}
}

// A theme is the sixteen ANSI colours as well as the two default ones: a
// program that asked for red by name gets the theme's red.
func TestThemePalette(t *testing.T) {
	win := newTabTestApp(t)
	tab := win.current()
	feedTab(t, tab, "\x1b[31mred")
	plain := tab.frame.cells[0].Fg

	// Step to a theme that carries a palette.
	for i := range ui.ThemeCount() {
		if ui.ThemeAt(i).Palette == nil {
			continue
		}
		if err := win.setTheme(i); err != nil {
			t.Fatalf("setTheme: %v", err)
		}
		if err := tab.refreshSnapshot(); err != nil {
			t.Fatalf("refresh: %v", err)
		}
		if tab.frame.cells[0].Fg == plain {
			t.Errorf("theme %q left ANSI red as %06x", ui.ThemeAt(i).Name, plain)
		}
		want := ui.ThemeAt(i).Palette[1]
		if got := tab.frame.cells[0].Fg; got.Uint32() != ui.Packed(want) {
			t.Errorf("ANSI red under %q = %06x, want the theme's %v", ui.ThemeAt(i).Name, got, want)
		}
		return
	}
	t.Skip("no theme carries a palette")
}
