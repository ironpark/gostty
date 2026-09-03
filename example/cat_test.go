package main

import (
	"image/color"
	"testing"

	"github.com/ironpark/gostty"
	"github.com/ironpark/gostty/example/thecat"
)

func TestThemeReplacesOnlyTerminalDefaults(t *testing.T) {
	background := color.RGBA{R: 1, G: 2, B: 3, A: 0xff}
	foreground := color.RGBA{R: 4, G: 5, B: 6, A: 0xff}
	ansiRed := color.RGBA{R: 0xff, A: 0xff}
	g := &game{
		terminalBg: background,
		terminalFg: foreground,
		ui:         ui{theme: 1},
	}

	if got := g.themeColor(background); got != themes[1].background {
		t.Errorf("background = %v, want %v", got, themes[1].background)
	}
	if got := g.themeColor(foreground); got != themes[1].foreground {
		t.Errorf("foreground = %v, want %v", got, themes[1].foreground)
	}
	if got := g.themeColor(ansiRed); got != ansiRed {
		t.Errorf("explicit ANSI color changed from %v to %v", ansiRed, got)
	}
}

// grid builds a game with just enough of it to answer the cat's questions:
// each string is a row, and anything that is not a space is something to stand
// on.
func grid(rows ...string) *game {
	cols := 0
	for _, row := range rows {
		cols = max(cols, len(row))
	}
	g := &game{
		cols:  cols,
		rows:  len(rows),
		fonts: &fontSet{cellW: 10, cellH: 20},
		cells: make([]gostty.RenderCell, cols*len(rows)),
	}
	for y, row := range rows {
		for x, r := range row {
			g.cells[y*cols+x] = gostty.RenderCell{Codepoint: uint32(r)}
		}
	}
	return g
}

// The ground is the text: the top of the first row with a glyph in that column,
// at or below where the cat is looking.
func TestGroundIsTheText(t *testing.T) {
	g := grid(
		"  ####    ",
		"          ",
		"##  #     ",
		"          ",
	)
	floor := float64(g.rows) * g.fonts.cellH // 80

	for _, c := range []struct {
		name string
		x    float64
		from float64
		want float64
	}{
		{"the top line, from above", 35, 0, 0},
		{"nothing below that line in the same column", 35, 1, floor},
		// The column under the fifth cell has text on two rows, so which one
		// is the ground depends on where the cat is looking from.
		{"two lines in one column, from above", 45, 0, 0},
		{"two lines in one column, from between them", 45, 1, 40},
		{"a column whose only text is further down", 5, 0, 40},
		{"a column with nothing in it", 85, 0, floor},
		{"below everything", 35, 60, floor},
		{"just off the left of the grid", -5, 0, floor},
		{"off the right of the grid", 1000, 0, floor},
	} {
		got, ok := g.GroundBelow(c.x, c.from)
		if !ok {
			t.Errorf("%s: no ground, and there is always a floor", c.name)
		}
		if got != c.want {
			t.Errorf("%s: GroundBelow(%v, %v) = %v, want %v", c.name, c.x, c.from, got, c.want)
		}
	}
}

// A blank screen is all floor, which is what the cat walks on before anything
// has been printed.
func TestGroundOnAnEmptyScreen(t *testing.T) {
	g := grid("    ", "    ")
	floor := float64(g.rows) * g.fonts.cellH
	for x := 0.0; x < float64(g.cols)*g.fonts.cellW; x += 5 {
		if got, _ := g.GroundBelow(x, 0); got != floor {
			t.Fatalf("GroundBelow(%v, 0) = %v, want the floor at %v", x, got, floor)
		}
	}
}

// Spaces are not ground: a cat cannot stand on the gap between two words.
func TestSpacesAreNotGround(t *testing.T) {
	g := grid("a b")
	floor := float64(g.rows) * g.fonts.cellH
	if got, _ := g.GroundBelow(15, 0); got != floor {
		t.Errorf("the gap between the letters gives ground at %v, want the floor at %v", got, floor)
	}
	if got, _ := g.GroundBelow(5, 0); got != 0 {
		t.Errorf("the letter gives ground at %v, want the top of its row", got)
	}
}

// The bounds the cat is kept inside are the grid, not the window, so a partial
// last column is not somewhere it can stand.
func TestBoundsAreTheGrid(t *testing.T) {
	g := grid("###", "###")
	w, h := g.Bounds()
	if w != 30 || h != 40 {
		t.Errorf("bounds = %vx%v, want 30x40", w, h)
	}
}

// The cat and the text together: a screen that looks like a shell session, and
// a cat let loose on it for a while.
//
// The bug this pins is the one that made the cat a pogo stick. The pointer is
// nearly always somewhere up the screen, and every line of output on the way to
// it is a ledge, so "jump when the target is above" fired on almost every tick.
func TestCatOnAShell(t *testing.T) {
	g := grid(
		"$ ls                                    ",
		"README.md  go.mod    main.go   render.go",
		"cat.go     go.sum    mouse.go  ui.go    ",
		"                                        ",
		"$ go test ./...                         ",
		"ok  github.com/ironpark/gostty   0.2s   ",
		"                                        ",
		"$                                       ",
		"                                        ",
		"                                        ",
	)
	cat, err := thecat.New(catRows * g.fonts.cellH)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	width, height := g.Bounds()
	cat.Place(width/2, height)

	// The pointer sits up in the output, which is where a pointer usually is,
	// and moves across the screen every few seconds so the cat has to keep
	// working for it.
	g.catAttention = 1 << 30
	corners := [][2]float64{{0.15, 0.1}, {0.85, 0.9}, {0.15, 0.9}, {0.85, 0.1}}

	const dt = 1.0 / 60
	seen := map[thecat.State]int{}
	minX, maxX := width, 0.0
	for i := range int(40 / dt) {
		corner := corners[int(float64(i)*dt/5)%len(corners)]
		g.catPointerX = int(width * corner[0])
		g.catPointerY = int(height * corner[1])

		cat.Update(g, dt)
		seen[cat.State()]++
		x, y := cat.Feet()
		if x < 0 || x > width || y < 0 || y > height {
			t.Fatalf("the cat left the screen: (%v, %v) is outside %vx%v", x, y, width, height)
		}
		minX, maxX = min(minX, x), max(maxX, x)
	}

	// It got about, rather than stalling against the first letter it met.
	if maxX-minX < width/2 {
		t.Errorf("it only covered %v of the %v-wide screen; it is stuck", maxX-minX, width)
	}

	// It spends most of its time on its feet, not in the air.
	air := seen[thecat.Jump] + seen[thecat.Apex] + seen[thecat.Fall] + seen[thecat.Spin]
	total := 0
	for _, n := range seen {
		total += n
	}
	if air*2 > total {
		t.Errorf("airborne for %d of %d ticks; it is bouncing rather than walking", air, total)
	}
	// And it does something other than stand still: this screen has ledges to
	// get onto and a pointer worth crossing it for.
	if seen[thecat.Walk]+seen[thecat.Run]+seen[thecat.Climb] == 0 {
		t.Error("it never moved")
	}
	// It jumps, because there are lines of output to get onto -- but a jump is
	// an event, not a way of getting about.
	if jumps := seen[thecat.Jump]; jumps > total/4 {
		t.Errorf("in a jump for %d of %d ticks; it is pogoing", jumps, total)
	}
}
