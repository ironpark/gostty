package gostty

import "testing"

// A surface where a cell is 10px wide and the grid starts at the left edge, so
// a pixel position converts to a cell by dividing by ten and the within-cell
// threshold (60% of the width) sits at 6.
const (
	testCellWidth   = 10
	testCellHeight  = 10
	testRepeatNanos = 500 * 1000 * 1000
)

func newGesture(t *testing.T, term *Terminal, cols, rows uint32) *Gesture {
	t.Helper()
	g := term.NewGesture()
	t.Cleanup(func() { g.Close() })
	// ghostty has no default word boundary set, so word selection would run to
	// the end of the line without one. A space is the minimum an emulator sets.
	if err := g.SetWordBoundaries([]rune{' '}); err != nil {
		t.Fatalf("SetWordBoundaries: %v", err)
	}
	g.SetGeometry(GestureGeometry{
		Columns:      cols,
		CellWidth:    testCellWidth,
		PaddingLeft:  0,
		ScreenHeight: rows * testCellHeight,
	})
	return g
}

// press at cell (x, y), landing one pixel into the cell so a left-to-right drag
// includes it, at time now.
func pressAt(t *testing.T, g *Gesture, x, y uint16, now int64) (Selection, bool) {
	t.Helper()
	sel, ok, err := g.Press(GesturePressEvent{
		X:                x,
		Y:                y,
		Xpos:             float64(x)*testCellWidth + 1,
		Ypos:             float64(y)*testCellHeight + 1,
		MaxDistance:      testCellWidth,
		RepeatIntervalNs: testRepeatNanos,
		TimeNs:           now,
	})
	if err != nil {
		t.Fatalf("Press: %v", err)
	}
	return sel, ok
}

// selectionText applies sel to the active screen and reads it back, which is
// what an emulator does with every selection a gesture hands it.
func selectionText(t *testing.T, term *Terminal, sel Selection) string {
	t.Helper()
	scr := term.ActiveScreen()
	ok, err := scr.SetSelection(sel)
	if err != nil || !ok {
		t.Fatalf("SetSelection(%+v) = %v, %v; want true, nil", sel, ok, err)
	}
	text, ok, err := scr.SelectionString()
	if err != nil || !ok {
		t.Fatalf("SelectionString() = %q, %v, %v; want text, true, nil", text, ok, err)
	}
	return text
}

// The click count drives the granularity: one click selects nothing so the
// caller clears, two select the word, three the line.
func TestGestureClickCounts(t *testing.T) {
	term := newTerm(t, 20, 3)
	if err := term.PrintString("hello world"); err != nil {
		t.Fatalf("PrintString: %v", err)
	}
	g := newGesture(t, term, 20, 3)

	if _, ok := pressAt(t, g, 1, 0, 0); ok {
		t.Error("first press produced a selection; want none so the caller clears")
	}
	if count := g.ClickCount(); count != 1 {
		t.Fatalf("ClickCount() = %d; want 1", count)
	}

	sel, ok := pressAt(t, g, 1, 0, 1000)
	if !ok {
		t.Fatal("second press produced no selection; want the word")
	}
	if count := g.ClickCount(); count != 2 {
		t.Errorf("ClickCount() = %d; want 2", count)
	}
	if got, want := selectionText(t, term, sel), "hello"; got != want {
		t.Errorf("double click selected %q, want %q", got, want)
	}

	sel, ok = pressAt(t, g, 1, 0, 2000)
	if !ok {
		t.Fatal("third press produced no selection; want the line")
	}
	if count := g.ClickCount(); count != 3 {
		t.Errorf("ClickCount() = %d; want 3", count)
	}
	if got, want := selectionText(t, term, sel), "hello world"; got != want {
		t.Errorf("triple click selected %q, want %q", got, want)
	}

	// A press after the repeat interval starts over rather than going to four.
	if _, ok := pressAt(t, g, 1, 0, 2000+testRepeatNanos+1); ok {
		t.Error("press after the repeat interval produced a selection; want a fresh single click")
	}
	if count := g.ClickCount(); count != 1 {
		t.Errorf("ClickCount() = %d; want 1", count)
	}
}

// A press too far from the previous one is a new gesture, not a double click.
func TestGesturePressDistanceResets(t *testing.T) {
	term := newTerm(t, 20, 3)
	if err := term.PrintString("hello world"); err != nil {
		t.Fatalf("PrintString: %v", err)
	}
	g := newGesture(t, term, 20, 3)

	pressAt(t, g, 1, 0, 0)
	if _, ok := pressAt(t, g, 8, 0, 1000); ok {
		t.Error("press a screen away produced a selection; want a fresh single click")
	}
	if count := g.ClickCount(); count != 1 {
		t.Errorf("ClickCount() = %d; want 1", count)
	}
}

// The whole single-click flow: press, drag across a word, release. The drag
// selection grows as the pointer moves, and the release settles it.
func TestGestureDragAndRelease(t *testing.T) {
	term := newTerm(t, 20, 3)
	if err := term.PrintString("hello world"); err != nil {
		t.Fatalf("PrintString: %v", err)
	}
	g := newGesture(t, term, 20, 3)

	pressAt(t, g, 0, 0, 0)
	if dragged := g.Dragged(); dragged {
		t.Errorf("Dragged() right after the press = %v; want false", dragged)
	}

	// Past 60% of cell 4, so cell 4 is included: "hello".
	sel, ok := g.Drag(GestureDragEvent{X: 4, Y: 0, Xpos: 4*testCellWidth + 9, Ypos: 5})
	if !ok {
		t.Fatalf("Drag() = %v, %v; want a selection", sel, ok)
	}
	if got, want := selectionText(t, term, sel), "hello"; got != want {
		t.Errorf("drag to cell 4 selected %q, want %q", got, want)
	}
	if dragged := g.Dragged(); !dragged {
		t.Errorf("Dragged() after moving = %v; want true", dragged)
	}

	// Extending the drag extends the selection rather than starting a new one.
	sel, ok = g.Drag(GestureDragEvent{X: 10, Y: 0, Xpos: 10*testCellWidth + 9, Ypos: 5})
	if !ok {
		t.Fatalf("Drag() = %v, %v; want a selection", sel, ok)
	}
	if got, want := selectionText(t, term, sel), "hello world"; got != want {
		t.Errorf("drag to cell 10 selected %q, want %q", got, want)
	}

	// A drag inside the surface asks for no autoscroll.
	if dir := g.Autoscroll(); dir != GestureAutoscrollDirectionNone {
		t.Errorf("Autoscroll() during an in-bounds drag = %v; want none", dir)
	}

	g.Release(10, 0)
	if dragged := g.Dragged(); !dragged {
		t.Errorf("Dragged() after release = %v; want true", dragged)
	}
	// Release keeps the click count so the next nearby press can be a double
	// click; it is Reset that ends the sequence.
	if count := g.ClickCount(); count != 1 {
		t.Errorf("ClickCount() after release = %d; want 1", count)
	}
	g.Reset()
	if count := g.ClickCount(); count != 0 {
		t.Errorf("ClickCount() after reset = %d; want 0", count)
	}

	// With no gesture in progress a drag selects nothing.
	if _, ok := g.Drag(GestureDragEvent{X: 4, Y: 0, Xpos: 49, Ypos: 5}); ok {
		t.Errorf("Drag() after Reset = %v; want no selection", ok)
	}
}

// A double-click drag stays word-granular: it snaps out to whole words rather
// than stopping at the cell under the pointer.
func TestGestureWordDrag(t *testing.T) {
	term := newTerm(t, 20, 3)
	if err := term.PrintString("hello world"); err != nil {
		t.Fatalf("PrintString: %v", err)
	}
	g := newGesture(t, term, 20, 3)

	pressAt(t, g, 1, 0, 0)
	if _, ok := pressAt(t, g, 1, 0, 1000); !ok {
		t.Fatal("double click produced no selection")
	}
	// Only one cell into "world", but the whole word comes along.
	sel, ok := g.Drag(GestureDragEvent{X: 7, Y: 0, Xpos: 7*testCellWidth + 1, Ypos: 5})
	if !ok {
		t.Fatalf("Drag() = %v, %v; want a selection", sel, ok)
	}
	if got, want := selectionText(t, term, sel), "hello world"; got != want {
		t.Errorf("word drag selected %q, want %q", got, want)
	}
}

// Dragging past the bottom edge asks for autoscroll, and a tick scrolls the
// viewport one row and keeps the selection growing.
func TestGestureAutoscroll(t *testing.T) {
	term := newTerm(t, 20, 3)
	for _, line := range []string{"one\r\n", "two\r\n", "three\r\n", "four\r\n", "five"} {
		if err := term.PrintString(line); err != nil {
			t.Fatalf("PrintString: %v", err)
		}
	}
	g := newGesture(t, term, 20, 3)

	// Scroll back so there is somewhere to scroll down to.
	term.ScrollViewport(ScrollViewportTop())

	pressAt(t, g, 0, 0, 0)
	bottom := GestureDragEvent{X: 0, Y: 2, Xpos: 9, Ypos: 3 * testCellHeight}
	g.Drag(bottom)
	dir := g.Autoscroll()
	if dir != GestureAutoscrollDirectionDown {
		t.Fatalf("Autoscroll() at the bottom edge = %v; want down", dir)
	}

	sel, ok := g.AutoscrollTick(bottom)
	if !ok {
		t.Fatal("AutoscrollTick produced no selection")
	}
	// One row of scrollback came into view, so the selection now reaches one
	// row further down the screen than the three-row viewport could show.
	if sel.EndY < 3 {
		t.Errorf("selection after an autoscroll tick ends at row %d, want past the first viewport", sel.EndY)
	}

	// Dragging back inside stops the timer.
	g.Drag(GestureDragEvent{X: 0, Y: 1, Xpos: 9, Ypos: testCellHeight})
	if dir := g.Autoscroll(); dir != GestureAutoscrollDirectionNone {
		t.Errorf("Autoscroll() back inside the surface = %v; want none", dir)
	}
}

// Word boundaries are configuration: with only a space set a word runs through
// punctuation, and adding the punctuation stops it there.
func TestGestureWordBoundaries(t *testing.T) {
	term := newTerm(t, 20, 3)
	if err := term.PrintString("foo.bar baz"); err != nil {
		t.Fatalf("PrintString: %v", err)
	}
	g := newGesture(t, term, 20, 3)

	pressAt(t, g, 1, 0, 0)
	sel, ok := pressAt(t, g, 1, 0, 1000)
	if !ok {
		t.Fatal("double click produced no selection")
	}
	if got, want := selectionText(t, term, sel), "foo.bar"; got != want {
		t.Errorf("double click with only a space boundary selected %q, want %q", got, want)
	}

	if err := g.SetWordBoundaries([]rune{' ', '.'}); err != nil {
		t.Fatalf("SetWordBoundaries: %v", err)
	}
	g.Reset()
	pressAt(t, g, 1, 0, 5000)
	sel, ok = pressAt(t, g, 1, 0, 6000)
	if !ok {
		t.Fatal("double click produced no selection")
	}
	if got, want := selectionText(t, term, sel), "foo"; got != want {
		t.Errorf("double click with '.' as a boundary selected %q, want %q", got, want)
	}
}

// The click-count mapping is configurable: an emulator that wants triple click
// to select command output says so once.
func TestGestureSetBehaviors(t *testing.T) {
	term := newTerm(t, 20, 3)
	if err := term.PrintString("hello world"); err != nil {
		t.Fatalf("PrintString: %v", err)
	}
	g := newGesture(t, term, 20, 3)

	g.SetBehaviors(GestureBehaviorLine, GestureBehaviorWord, GestureBehaviorLine)
	sel, ok := pressAt(t, g, 1, 0, 0)
	if !ok {
		t.Fatal("single press with line behavior produced no selection")
	}
	if got, want := selectionText(t, term, sel), "hello world"; got != want {
		t.Errorf("single click with line behavior selected %q, want %q", got, want)
	}
}

// A deep press selects the word under the press and ends the gesture, so the
// pointer moving afterwards no longer drags.
func TestGestureDeepPress(t *testing.T) {
	term := newTerm(t, 20, 3)
	if err := term.PrintString("hello world"); err != nil {
		t.Fatalf("PrintString: %v", err)
	}
	g := newGesture(t, term, 20, 3)

	g.DeepPress()

	pressAt(t, g, 1, 0, 0)
	sel, ok := g.DeepPress()
	if !ok {
		t.Fatalf("DeepPress() = %v, %v; want the word under the press", sel, ok)
	}
	if got, want := selectionText(t, term, sel), "hello"; got != want {
		t.Errorf("deep press selected %q, want %q", got, want)
	}
	if count := g.ClickCount(); count != 0 {
		t.Errorf("ClickCount() after a deep press = %d; want 0", count)
	}
	if _, ok := g.Drag(GestureDragEvent{X: 10, Y: 0, Xpos: 109, Ypos: 5}); ok {
		t.Errorf("Drag() after a deep press = %v; want no selection", ok)
	}
}

// A press outside the viewport starts nothing, rather than anchoring the
// gesture somewhere arbitrary.
func TestGesturePressOutsideViewport(t *testing.T) {
	term := newTerm(t, 20, 3)
	g := newGesture(t, term, 20, 3)

	if _, ok := pressAt(t, g, 0, 9, 0); ok {
		t.Error("press below the viewport produced a selection")
	}
	if count := g.ClickCount(); count != 0 {
		t.Errorf("ClickCount() after an out-of-bounds press = %d; want 0", count)
	}
}
