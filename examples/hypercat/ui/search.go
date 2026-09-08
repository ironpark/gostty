package ui

import (
	"fmt"
	"unicode"

	"github.com/hajimehoshi/ebiten/v2/vector"
)

// SearchBar owns the editable query and displays results supplied by the host.
type SearchBar struct {
	Query   []rune
	Matches uint
	Failure string
}

func (s *SearchBar) Handle(in Input) Actions {
	result := Actions{Consumed: true}
	for _, r := range in.Chars {
		if unicode.IsControl(r) {
			continue
		}
		s.Query = append(s.Query, r)
		result.QueryChanged = true
	}
	if in.Backspace && len(s.Query) > 0 {
		s.Query = s.Query[:len(s.Query)-1]
		result.QueryChanged = true
	}
	if result.QueryChanged || in.Enter {
		result.MatchStep = 1
		if !result.QueryChanged && in.Shift {
			result.MatchStep = -1
		}
	}
	return result
}

func (s *SearchBar) Draw(c Canvas) {
	h := c.CellHeight + 8
	y := c.Height - h
	c.Panel(0, y, c.Width, h)
	count := ""
	switch {
	case s.Failure != "":
		count = s.Failure
	case len(s.Query) == 0:
		count = "type to search, enter for the next match, shift+enter for the previous"
	case s.Matches == 0:
		count = "no matches"
	default:
		count = fmt.Sprintf("%d matches", s.Matches)
	}
	x := c.Text("/", 4, y+4, c.Theme.Accent)
	x = c.Text(string(s.Query), x, y+4, c.Theme.Text)
	vector.FillRect(c.Screen, float32(x), float32(y+4), float32(c.CellWidth), float32(c.CellHeight), c.Theme.Accent, false)
	c.Text(count, x+2*c.CellWidth, y+4, c.Theme.Dim)
}
