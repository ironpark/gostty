package main

import "testing"

// What a click count means is the gesture's, not this program's: the second
// click selects the word and the third the command output around it, and both
// know about wrapping and word boundaries that this program does not.
func TestClickCountsSelectWordAndOutput(t *testing.T) {
	app := newTabTestApp(t)
	tab := app.current()
	feedTab(t, tab, "hello world")

	tab.pressAtCell(t, 1, 0)
	if got := selected(t, tab); got != "" {
		t.Errorf("a single click selected %q, want nothing but an anchor", got)
	}
	tab.pressAtCell(t, 1, 0)
	if got, want := selected(t, tab), "hello"; got != want {
		t.Errorf("a double click selected %q, want %q", got, want)
	}
	if count, err := tab.sel.gesture.ClickCount(); err != nil || count != 2 {
		t.Errorf("ClickCount() = %d, %v; want 2", count, err)
	}
}

// A drag grows the selection from the anchor the press set, across whatever
// the terminal knows about the layout in between.
func TestDragSelectsFromTheAnchor(t *testing.T) {
	app := newTabTestApp(t)
	tab := app.current()
	feedTab(t, tab, "hello world")

	tab.pressAtCell(t, 0, 0)
	tab.dragToCell(t, 4, 0, false)
	if got, want := selected(t, tab), "hello"; got != want {
		t.Errorf("drag selected %q, want %q", got, want)
	}
	tab.dragToCell(t, 10, 0, false)
	if got, want := selected(t, tab), "hello world"; got != want {
		t.Errorf("extending the drag selected %q, want %q", got, want)
	}
	if dragged, err := tab.sel.gesture.Dragged(); err != nil || !dragged {
		t.Errorf("Dragged() = %v, %v; want true", dragged, err)
	}
}

// The word boundaries are this program's configuration, which is why they are
// handed over rather than assumed: ghostty ships no default set.
func TestWordBoundariesAreConfigured(t *testing.T) {
	app := newTabTestApp(t)
	tab := app.current()
	feedTab(t, tab, "path:line")

	tab.pressAtCell(t, 1, 0) // inside "path"
	tab.pressAtCell(t, 1, 0)
	// The colon is in this program's boundary set, so the word stops there
	// rather than running to the space at the end of the line.
	if got, want := selected(t, tab), "path"; got != want {
		t.Errorf("a double click selected %q, want %q", got, want)
	}
}

// Ending the gesture ends the sequence, so a press in the next tab the pointer
// visits is a first click rather than a second one.
func TestEndGestureResetsTheClickCount(t *testing.T) {
	app := newTabTestApp(t)
	tab := app.current()
	feedTab(t, tab, "hello world")

	tab.pressAtCell(t, 1, 0)
	tab.endGesture()
	if count, err := tab.sel.gesture.ClickCount(); err != nil || count != 0 {
		t.Errorf("ClickCount() after endGesture = %d, %v; want 0", count, err)
	}
	if tab.sel.dragging {
		t.Error("a drag survived the end of the gesture")
	}
}

// The geometry the gesture measures the pointer against follows the grid: a
// resize that changes the cell size must not leave it working from the old one.
func TestGestureGeometryFollowsAResize(t *testing.T) {
	app := newTabTestApp(t)
	tab := app.current()
	feedTab(t, tab, "hello world")

	if err := tab.resize(40, 12); err != nil {
		t.Fatalf("resize: %v", err)
	}
	// A press and a drag on the resized grid still land where they are aimed,
	// which they would not if the gesture were still measuring 80 columns.
	tab.pressAtCell(t, 0, 0)
	tab.dragToCell(t, 4, 0, false)
	if got, want := selected(t, tab), "hello"; got != want {
		t.Errorf("drag after a resize selected %q, want %q", got, want)
	}
}

// Alt selects the block between the corners rather than the flow of text, and
// it is the gesture that applies it as the selection grows.
func TestRectangleDrag(t *testing.T) {
	app := newTabTestApp(t)
	tab := app.current()
	feedTab(t, tab, "abcdef\r\nghijkl")

	tab.pressAtCell(t, 1, 0)
	tab.dragToCell(t, 2, 1, true)
	got := selected(t, tab)
	if got == "" {
		t.Fatal("a rectangular drag selected nothing")
	}
	// A flowing selection would run through the end of the first line; a
	// rectangular one takes the same two columns from both rows.
	if len(got) > len("bc\nhi") {
		t.Errorf("rectangular drag selected %q, want the block alone", got)
	}
}

// The gesture is a native handle owned by the tab, so it is released with it
// and before the terminal it belongs to.
func TestClosingATabReleasesTheGesture(t *testing.T) {
	app := newTabTestApp(t)
	tab := app.current()
	if tab.sel.gesture == nil {
		t.Fatal("a started tab has no gesture")
	}
	app.closeTab(0)
	if tab.sel.gesture != nil {
		t.Error("the gesture handle outlived the tab")
	}
}
