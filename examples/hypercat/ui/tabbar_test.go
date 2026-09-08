package ui

import "testing"

func TestTabBarOverflowAndHitTesting(t *testing.T) {
	var bar TabBar
	l := bar.Layout(900, 2, 11, 12)
	if 11 < l.First || 11 >= l.First+l.Slots {
		t.Fatal("active tab is offscreen")
	}
	if float64(l.Slots)*l.TabWidth > l.Width-l.Height {
		t.Fatal("tabs overlap add button")
	}
	if got := l.Hit(10, 10); got.Kind != TabSelect || got.Index != l.First {
		t.Fatalf("select = %+v", got)
	}
	if got := l.Hit(l.TabWidth-10, 10); got.Kind != TabClose || got.Index != l.First {
		t.Fatalf("close = %+v", got)
	}
	if got := l.Hit(899, 10); got.Kind != TabAdd {
		t.Fatalf("add = %+v", got)
	}
	for _, point := range [][2]float64{{-1, 10}, {900, 10}, {10, -1}, {10, l.Height}} {
		if got := l.Hit(point[0], point[1]); got.Kind != TabNone {
			t.Fatalf("outside hit = %+v", got)
		}
	}
	if first := bar.Layout(900, 2, 0, 12).First; first != 0 {
		t.Fatal("switching back did not reveal first tab")
	}
	// Empty space between a short tab list and the add button is inert.
	l = bar.Layout(900, 1, 0, 1)
	if got := l.Hit(500, 10); got.Kind != TabNone {
		t.Fatalf("blank bar selected a tab: %+v", got)
	}
}

func TestTabLabelFitsWideCharacters(t *testing.T) {
	width := func(r rune) int {
		if r >= '가' && r <= '힣' {
			return 2
		}
		return 1
	}
	if got := FitLabel("1 한글 title", 6, width); got != "1 한글" {
		t.Fatalf("wide title = %q", got)
	}
	if got := FitLabel("abc", 0, width); got != "" {
		t.Fatalf("zero-width title = %q", got)
	}
}
