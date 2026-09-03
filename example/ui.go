package main

import (
	"fmt"
	"image/color"
	"math"
	"strings"
	"unicode"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/vector"
	"github.com/ironpark/gostty"
)

// The two panels this window puts over the grid: a search bar and a settings
// list. Both take the keyboard while they are open, so nothing typed into them
// reaches the shell.
//
// The search itself is not here. `Screen.NewSearch` scans the scrollback,
// `Search.Select` moves between matches and puts the current one in the
// screen's selection -- which is the same selection a drag makes, so the match
// is drawn highlighted and copied by the same Ctrl+Shift+C, and the viewport is
// moved to it. All this owns is the query string.

type uiMode int

const (
	uiNone uiMode = iota
	uiSearch
	uiSettings
)

type ui struct {
	mode uiMode

	// Search.
	query   []rune
	search  *gostty.Search
	matches uint
	// What went wrong, if the search could not run at all.
	failure string

	// Settings.
	families []*fontFamily
	family   int // index into families
	size     float64
	theme    int
	// Which line of the settings panel the arrow keys move.
	row int
}

const (
	settingsRowFont = iota
	settingsRowSize
	settingsRowTheme
	settingsRowCat
	settingsRowCount
)

func (u *ui) open() bool { return u.mode != uiNone }

// closeSearch drops the search handle. It is a child of the screen, which is
// borrowed from the terminal, so it cannot outlive either.
func (u *ui) closeSearch() {
	if u.search != nil {
		u.search.Close()
		u.search = nil
	}
	u.matches = 0
	u.failure = ""
}

// handleUI runs the panel that has the keyboard. Returns whether it took the
// frame's input; when it did, nothing goes to the shell.
func (g *game) handleUI(m mods) (bool, error) {
	// Opening is a binding this window keeps for itself, like copy and paste.
	if (m.ctrl && m.shift) || m.super {
		switch {
		case inpututil.IsKeyJustPressed(ebiten.KeyF):
			return true, g.openSearch()
		case inpututil.IsKeyJustPressed(ebiten.KeyComma):
			return true, g.openSettings()
		}
	}
	if !g.ui.open() {
		return false, nil
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyEscape) {
		return true, g.closeUI()
	}
	switch g.ui.mode {
	case uiSearch:
		return true, g.searchKeys(m)
	case uiSettings:
		return true, g.settingsKeys()
	}
	return false, nil
}

func (g *game) openSearch() error {
	if g.ui.mode == uiSearch {
		return nil
	}
	g.ui.mode = uiSearch
	g.ui.query = g.ui.query[:0]
	g.ui.closeSearch()
	return nil
}

func (g *game) openSettings() error {
	g.ui.mode = uiSettings
	g.ui.row = settingsRowFont
	return nil
}

// closeUI puts the keyboard back. The match a search found is left selected on
// purpose: finding it is only half of what it was for, and the selection is
// what Ctrl+Shift+C copies.
func (g *game) closeUI() error {
	g.ui.mode = uiNone
	g.ui.closeSearch()
	return nil
}

func (g *game) searchKeys(m mods) error {
	changed := false
	g.chars = ebiten.AppendInputChars(g.chars[:0])
	for _, r := range g.chars {
		if unicode.IsControl(r) {
			continue
		}
		g.ui.query = append(g.ui.query, r)
		changed = true
	}
	if repeating(inpututil.KeyPressDuration(ebiten.KeyBackspace)) && len(g.ui.query) > 0 {
		g.ui.query = g.ui.query[:len(g.ui.query)-1]
		changed = true
	}
	if changed {
		if err := g.runSearch(); err != nil {
			return err
		}
		// Show a match as soon as there is one to show. `next` starts at the
		// one nearest the prompt, which is the one being looked for far more
		// often than the oldest.
		return g.moveMatch(gostty.SearchDirectionNext)
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyEnter) {
		// `next` walks backwards in time -- towards the top of the scrollback
		// -- because that is the direction a search through what already
		// happened goes.
		if m.shift {
			return g.moveMatch(gostty.SearchDirectionPrev)
		}
		return g.moveMatch(gostty.SearchDirectionNext)
	}
	return nil
}

// runSearch rebuilds the search for the current query.
//
// A search holds positions in the scrollback, and feeding the terminal moves
// them, so it is rebuilt rather than kept: the query is short and the scan is
// ghostty's, which makes this cheap enough to do on every keystroke.
func (g *game) runSearch() error {
	g.ui.closeSearch()
	if len(g.ui.query) == 0 {
		return nil
	}
	screen, err := g.vt.ActiveScreen()
	if err != nil {
		return err
	}
	search, err := screen.NewSearch(string(g.ui.query))
	if err != nil {
		// A needle the search cannot take (too long for its window) is the
		// user's problem to see, not a reason to stop the terminal.
		g.ui.failure = err.Error()
		return nil
	}
	g.ui.search = search
	if err := search.SearchAll(); err != nil {
		return err
	}
	g.ui.matches, err = search.MatchCount()
	return err
}

// moveMatch steps to the next or previous match. The binding puts it in the
// screen's selection and brings the viewport to it.
func (g *game) moveMatch(dir gostty.SearchDirection) error {
	if g.ui.search == nil || g.ui.matches == 0 {
		return nil
	}
	_, err := g.ui.search.Select(dir)
	return err
}

func (g *game) settingsKeys() error {
	switch {
	case inpututil.IsKeyJustPressed(ebiten.KeyArrowUp):
		g.ui.row = (g.ui.row + settingsRowCount - 1) % settingsRowCount
	case inpututil.IsKeyJustPressed(ebiten.KeyArrowDown):
		g.ui.row = (g.ui.row + 1) % settingsRowCount
	case inpututil.IsKeyJustPressed(ebiten.KeyArrowLeft):
		return g.settingsAdjust(-1)
	case inpututil.IsKeyJustPressed(ebiten.KeyArrowRight):
		return g.settingsAdjust(1)
	case inpututil.IsKeyJustPressed(ebiten.KeyMinus):
		return g.settingsAdjust(-1)
	case inpututil.IsKeyJustPressed(ebiten.KeyEqual): // the unshifted +
		return g.settingsAdjust(1)
	case inpututil.IsKeyJustPressed(ebiten.KeyEnter):
		return g.closeUI()
	}
	return nil
}

func (g *game) settingsAdjust(delta int) error {
	switch g.ui.row {
	case settingsRowFont:
		if len(g.ui.families) == 0 {
			return nil
		}
		next := (g.ui.family + delta + len(g.ui.families)) % len(g.ui.families)
		return g.setFont(next, g.ui.size)
	case settingsRowTheme:
		g.ui.theme = (g.ui.theme + delta + len(themes)) % len(themes)
		return nil
	case settingsRowCat:
		if g.cat == nil {
			return nil
		}
		mode := 0 // off
		if g.catOn {
			mode = 1 // on
			if g.cat.Hyper() {
				mode = 2 // hyper
			}
		}
		mode = (mode + delta + 3) % 3
		g.catOn = mode != 0
		g.cat.SetHyper(mode == 2)
		if g.catOn {
			g.catAttention = catAttentionTicks
		}
		return nil
	default:
		return g.setFont(g.ui.family, g.ui.size+float64(delta))
	}
}

// setFont swaps the faces and lets the window follow.
//
// Only the faces change here. The cell size comes out of them, so the next
// Layout works out how many columns and rows the window now holds and resizes
// the terminal and the pty to match -- which is the same path a window resize
// takes, so the program is told the way it expects.
func (g *game) setFont(family int, size float64) error {
	size = min(max(size, minFontSize), maxFontSize)
	if len(g.ui.families) == 0 {
		return nil
	}
	family = min(max(family, 0), len(g.ui.families)-1)
	if family == g.ui.family && size == g.ui.size {
		return nil
	}
	g.ui.family, g.ui.size = family, size
	g.applyFont()
	return nil
}

// applyFont rebuilds the faces from what the settings hold.
//
// The size in the panel is in device-independent pixels, because that is what a
// user means by "14px"; what the face is asked for is that times the display's
// scale factor. This is also called when the window moves to a display with a
// different one.
func (g *game) applyFont() {
	var family *fontFamily
	if len(g.ui.families) > 0 {
		family = g.ui.families[min(g.ui.family, len(g.ui.families)-1)]
	}
	g.fonts = loadFonts(family, g.ui.size*g.dsf)
	// The grid is measured in cells and the cell just changed shape, so the
	// window holds a different number of them. Layout is where that is worked
	// out; this only has to say that the answer it cached is stale, because the
	// column count can survive a size change while the pixel geometry the image
	// protocol measures in does not.
	g.relayout = true
}

// -- Drawing ---------------------------------------------------------------

type theme struct {
	name                                  string
	terminal                              bool
	background, foreground, panel, border color.RGBA
	text, dim, accent                     color.RGBA
}

var themes = []theme{
	{
		name: "terminal", terminal: true,
		panel: color.RGBA{R: 0x20, G: 0x20, B: 0x28, A: 0xf0}, border: color.RGBA{R: 0x50, G: 0x50, B: 0x60, A: 0xff},
		text: color.RGBA{R: 0xe0, G: 0xe0, B: 0xe0, A: 0xff}, dim: color.RGBA{R: 0x90, G: 0x90, B: 0xa0, A: 0xff},
		accent: color.RGBA{R: 0x7a, G: 0xc0, B: 0xff, A: 0xff},
	},
	{
		name:       "midnight",
		background: color.RGBA{R: 0x0b, G: 0x10, B: 0x1f, A: 0xff}, foreground: color.RGBA{R: 0xd7, G: 0xe3, B: 0xfc, A: 0xff},
		panel: color.RGBA{R: 0x12, G: 0x1a, B: 0x30, A: 0xf4}, border: color.RGBA{R: 0x38, G: 0x55, B: 0x83, A: 0xff},
		text: color.RGBA{R: 0xea, G: 0xf2, B: 0xff, A: 0xff}, dim: color.RGBA{R: 0x82, G: 0x97, B: 0xb8, A: 0xff},
		accent: color.RGBA{R: 0x57, G: 0xd9, B: 0xff, A: 0xff},
	},
	{
		name:       "catppuccin",
		background: color.RGBA{R: 0x1e, G: 0x1e, B: 0x2e, A: 0xff}, foreground: color.RGBA{R: 0xcd, G: 0xd6, B: 0xf4, A: 0xff},
		panel: color.RGBA{R: 0x31, G: 0x32, B: 0x44, A: 0xf4}, border: color.RGBA{R: 0x58, G: 0x5b, B: 0x70, A: 0xff},
		text: color.RGBA{R: 0xcd, G: 0xd6, B: 0xf4, A: 0xff}, dim: color.RGBA{R: 0x93, G: 0x9a, B: 0xb7, A: 0xff},
		accent: color.RGBA{R: 0x89, G: 0xdc, B: 0xeb, A: 0xff},
	},
	{
		name:       "solarized",
		background: color.RGBA{R: 0x00, G: 0x2b, B: 0x36, A: 0xff}, foreground: color.RGBA{R: 0x93, G: 0xa1, B: 0xa1, A: 0xff},
		panel: color.RGBA{R: 0x07, G: 0x36, B: 0x42, A: 0xf4}, border: color.RGBA{R: 0x58, G: 0x6e, B: 0x75, A: 0xff},
		text: color.RGBA{R: 0xee, G: 0xe8, B: 0xd5, A: 0xff}, dim: color.RGBA{R: 0x83, G: 0x94, B: 0x96, A: 0xff},
		accent: color.RGBA{R: 0x2a, G: 0xa1, B: 0x98, A: 0xff},
	},
}

func (g *game) currentTheme() theme { return themes[g.ui.theme%len(themes)] }

// themeColor substitutes only the terminal's default colors. Explicit ANSI
// colors belong to the application and remain untouched.
func (g *game) themeColor(c color.RGBA) color.RGBA {
	theme := g.currentTheme()
	if theme.terminal {
		return c
	}
	if c == g.terminalBg {
		return theme.background
	}
	if c == g.terminalFg {
		return theme.foreground
	}
	return c
}

func (g *game) drawUI(screen *ebiten.Image) {
	switch g.ui.mode {
	case uiSearch:
		g.drawSearch(screen)
	case uiSettings:
		g.drawSettings(screen)
	}
}

func (g *game) drawSearch(screen *ebiten.Image) {
	theme := g.currentTheme()
	w := float64(g.cols) * g.fonts.cellW
	h := g.fonts.cellH + 8
	y := float64(g.rows)*g.fonts.cellH - h
	g.panel(screen, 0, y, w, h)

	count := ""
	switch {
	case g.ui.failure != "":
		count = g.ui.failure
	case len(g.ui.query) == 0:
		count = "type to search, enter for the next match, shift+enter for the previous"
	case g.ui.matches == 0:
		count = "no matches"
	default:
		count = fmt.Sprintf("%d matches", g.ui.matches)
	}

	x := g.drawText(screen, "/", 4, y+4, theme.accent)
	x = g.drawText(screen, string(g.ui.query), x, y+4, theme.text)
	// A block for the caret, in the same cell grid as everything else.
	vector.DrawFilledRect(screen, float32(x), float32(y+4),
		float32(g.fonts.cellW), float32(g.fonts.cellH), theme.accent, false)
	g.drawText(screen, count, x+2*g.fonts.cellW, y+4, theme.dim)
}

func (g *game) drawSettings(screen *ebiten.Image) {
	theme := g.currentTheme()
	lines := []struct{ label, value string }{
		{"font", g.fontLabel()},
		{"size", fmt.Sprintf("%.0f px", g.ui.size)},
		{"theme", g.themeLabel()},
		{"cat", g.catLabel()},
	}
	w := 0.0
	for _, line := range lines {
		w = max(w, float64(len(line.label)+len(line.value)+6)*g.fonts.cellW)
	}
	// Room around the text, in whole pixels of whatever the display is.
	padding := math.Round(12 * g.dsf)
	// Wide enough for the longest hint, with a column of air at each end, and a
	// blank row above the rows and below the hint.
	w = max(w, 70*g.fonts.cellW) + 2*padding
	h := float64(len(lines)+3)*g.fonts.cellH + 2*padding

	// In the middle of the window, on the cell grid: a panel that starts
	// half-way through a column would blur every glyph in it.
	screenW := float64(g.cols) * g.fonts.cellW
	screenH := float64(g.rows) * g.fonts.cellH
	x := math.Round((screenW-w)/2/g.fonts.cellW) * g.fonts.cellW
	y := math.Round((screenH-h)/2/g.fonts.cellH) * g.fonts.cellH
	g.panel(screen, x, y, w, h)

	for i, line := range lines {
		at := y + padding + float64(i)*g.fonts.cellH
		fg, marker := theme.dim, "  "
		if i == g.ui.row {
			fg, marker = theme.text, "> "
		}
		g.drawText(screen, marker+line.label, x+padding, at, fg)
		g.drawText(screen, line.value, x+padding+10*g.fonts.cellW, at, fg)
	}
	g.drawText(screen, "up/down to move, left/right or -/+ to change, enter to close",
		x+padding, y+padding+float64(len(lines)+2)*g.fonts.cellH, theme.dim)
}

func (g *game) themeLabel() string {
	return fmt.Sprintf("%s  (%d/%d)", g.currentTheme().name, g.ui.theme+1, len(themes))
}

// fontLabel says which faces the chosen family actually has, because that is
// what decides whether bold and italic text looks any different.
func (g *game) fontLabel() string {
	if len(g.ui.families) == 0 {
		return g.fonts.family + " (bundled)"
	}
	family := g.ui.families[g.ui.family]
	var have []string
	if family.has(true, false) {
		have = append(have, "bold")
	}
	if family.has(false, true) {
		have = append(have, "italic")
	}
	if family.has(true, true) {
		have = append(have, "bold italic")
	}
	label := fmt.Sprintf("%s  (%d/%d)", family.name, g.ui.family+1, len(g.ui.families))
	if len(have) == 0 {
		return label + "  regular only"
	}
	return label + "  + " + strings.Join(have, ", ")
}

// catLabel says what the cat is up to, which is the only way to tell a cat that
// is switched off from one asleep behind the prompt.
func (g *game) catLabel() string {
	switch {
	case g.cat == nil:
		return "unavailable"
	case !g.catOn:
		return "off"
	case g.cat.Hyper():
		return "hyper"
	default:
		return "on"
	}
}

func (g *game) panel(screen *ebiten.Image, x, y, w, h float64) {
	theme := g.currentTheme()
	vector.DrawFilledRect(screen, float32(x), float32(y), float32(w), float32(h), theme.panel, false)
	vector.StrokeRect(screen, float32(x), float32(y), float32(w), float32(h), 1, theme.border, false)
}

// drawText writes a line in the grid's own cell width, so the panels line up
// with the terminal behind them, and returns where it ended.
func (g *game) drawText(screen *ebiten.Image, s string, x, y float64, fg color.RGBA) float64 {
	for _, r := range s {
		wide := runeWidth(r) == 2
		g.glyph(screen, r, x, y, wide, false, false, fg)
		x += g.fonts.cellW
		if wide {
			x += g.fonts.cellW
		}
	}
	return x
}

// runeWidth asks the binding how many columns a rune takes, which is the same
// answer the terminal used when it laid the grid out.
func runeWidth(r rune) int {
	w, err := gostty.CodepointWidth(uint32(r))
	if err != nil {
		return 1
	}
	return int(w)
}
