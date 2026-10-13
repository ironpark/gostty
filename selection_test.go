package gostty

import "testing"

// Selection operations take and return values in screen coordinates.
func TestSelectionOperations(t *testing.T) {
	term, stream := newStreamPair(t, 10, 3)
	feed(t, stream, "hello\r\nworld")
	screen := term.ActiveScreen()
	sel := Selection{StartX: 1, StartY: 0, EndX: 2, EndY: 1}

	for _, tc := range []struct {
		x    uint16
		y    uint32
		want bool
	}{
		{1, 0, true}, {4, 0, true}, {0, 1, true}, {2, 1, true}, {3, 1, false}, {0, 0, false},
	} {
		got := screen.SelectionContains(sel, tc.x, tc.y)
		if got != tc.want {
			t.Errorf("SelectionContains(%d,%d) = %v; want %v", tc.x, tc.y, got, tc.want)
		}
	}

	moved, ok := screen.SelectionAdjust(sel, SelectionAdjustmentRight)
	if !ok || moved.EndX != 3 || moved.EndY != 1 || moved.StartX != 1 {
		t.Errorf("SelectionAdjust(right) = %+v, %v", moved, ok)
	}

	if _, ok := screen.SelectionAdjust(Selection{StartY: 99, EndY: 99}, SelectionAdjustmentLeft); ok {
		t.Error("SelectionAdjust outside the screen reported ok")
	}
	if adj, err := ParseSelectionAdjustment("end_of_line"); err != nil || adj != SelectionAdjustmentEndOfLine {
		t.Errorf("ParseSelectionAdjustment = %v, %v", adj, err)
	}
}
