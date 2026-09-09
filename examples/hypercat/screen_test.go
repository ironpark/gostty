package main

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ironpark/gostty"
	"github.com/ironpark/gostty/examples/hypercat/ui"
)

// The scrollback is bounded by the tab, not by the library: the native default
// is unbounded, which a window with a tab bar cannot afford.
func TestScrollbackIsBounded(t *testing.T) {
	app := newTabTestApp(t)
	tab := app.current()

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
	app := newTabTestApp(t)
	tab := app.current()

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
	if err := tab.refresh(); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if tab.frame.scrollbar.bar.Offset != 0 {
		t.Errorf("Scrollbar().Offset at the top = %d, want 0", tab.frame.scrollbar.bar.Offset)
	}
	if tab.frame.scrollbar.visible <= 0 {
		t.Error("no scrollbar while the viewport is away from the bottom")
	}
}

// Select-all is the whole scrollback, not the viewport: what is off the top of
// the screen is still selected text.
func TestSelectAllTakesTheScrollback(t *testing.T) {
	app := newTabTestApp(t)
	tab := app.current()
	feedTab(t, tab, strings.Repeat("line\r\n", 60)+"last")

	if err := tab.selectAll(); err != nil {
		t.Fatalf("selectAll: %v", err)
	}
	text := selected(t, tab)
	if !strings.Contains(text, "last") {
		t.Error("the selection does not reach the bottom of the screen")
	}
	if strings.Count(text, "line") <= tab.rows {
		t.Errorf("the selection covers %d lines, want the scrollback too", strings.Count(text, "line"))
	}
}

// Moving the end of a selection is the terminal's arithmetic: the end of a row
// steps to the start of the next one rather than off the edge of the grid.
func TestSelectionAdjustMovesTheEnd(t *testing.T) {
	app := newTabTestApp(t)
	tab := app.current()
	feedTab(t, tab, "hello world")

	screen := screenOf(t, tab)
	if _, err := screen.SetSelection(gostty.Selection{StartX: 0, StartY: 0, EndX: 0, EndY: 0}); err != nil {
		t.Fatalf("SetSelection: %v", err)
	}
	sel, ok, err := screen.Selection()
	if err != nil || !ok {
		t.Fatalf("Selection() = %v, %v", ok, err)
	}
	grown, ok, err := screen.SelectionAdjust(sel, gostty.SelectionAdjustmentRight)
	if err != nil || !ok {
		t.Fatalf("SelectionAdjust() = %v, %v", ok, err)
	}
	if _, err := screen.SetSelection(grown); err != nil {
		t.Fatalf("SetSelection: %v", err)
	}
	if got, want := selected(t, tab), "he"; got != want {
		t.Errorf("selection after one step right = %q, want %q", got, want)
	}
}

// The scrollback is written out by the formatter, which keeps the styles and
// unwraps the soft wraps. A loop over the cells would keep neither.
func TestExportScrollbackWritesStyledHTML(t *testing.T) {
	app := newTabTestApp(t)
	tab := app.current()
	feedTab(t, tab, "\x1b[31mred\x1b[0m plain")

	before := time.Now()
	if err := tab.exportScrollback(); err != nil {
		t.Fatalf("exportScrollback: %v", err)
	}
	name := newestExport(t, before)
	body, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read export: %v", err)
	}
	t.Cleanup(func() { os.Remove(name) })
	text := string(body)
	if !strings.Contains(text, "red") || !strings.Contains(text, "plain") {
		t.Errorf("the export is missing the screen: %q", text)
	}
	if !strings.Contains(text, "<") {
		t.Errorf("the export carries no markup, so the colours were lost: %q", text)
	}
}

// newestExport is the file exportScrollback just wrote.
func newestExport(t *testing.T, after time.Time) string {
	t.Helper()
	entries, err := os.ReadDir(os.TempDir())
	if err != nil {
		t.Fatalf("read temp dir: %v", err)
	}
	newest := ""
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "hypercat-") || !strings.HasSuffix(entry.Name(), ".html") {
			continue
		}
		info, err := entry.Info()
		if err != nil || info.ModTime().Before(after.Add(-time.Second)) {
			continue
		}
		newest = os.TempDir() + string(os.PathSeparator) + entry.Name()
	}
	if newest == "" {
		t.Fatal("exportScrollback wrote no file")
	}
	return newest
}

// A theme is the sixteen ANSI colours as well as the two default ones: a
// program that asked for red by name gets the theme's red.
func TestThemePalette(t *testing.T) {
	app := newTabTestApp(t)
	tab := app.current()
	feedTab(t, tab, "\x1b[31mred")
	plain := tab.frame.cells[0].Fg

	// Step to a theme that carries a palette.
	for i := range ui.ThemeCount() {
		if ui.ThemeAt(i).Palette == nil {
			continue
		}
		if err := app.setTheme(i); err != nil {
			t.Fatalf("setTheme: %v", err)
		}
		if err := tab.refresh(); err != nil {
			t.Fatalf("refresh: %v", err)
		}
		if tab.frame.cells[0].Fg == plain {
			t.Errorf("theme %q left ANSI red as %06x", ui.ThemeAt(i).Name, plain)
		}
		want := ui.ThemeAt(i).Palette[1]
		if got := tab.frame.cells[0].Fg; got != uint32(want.R)<<16|uint32(want.G)<<8|uint32(want.B) {
			t.Errorf("ANSI red under %q = %06x, want the theme's %v", ui.ThemeAt(i).Name, got, want)
		}
		return
	}
	t.Skip("no theme carries a palette")
}
