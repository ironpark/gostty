package ui

import "math"

const (
	SettingFont = iota
	SettingSize
	SettingTheme
	SettingCat
	SettingCount
)

// SettingsPanel owns row navigation; the host applies requested changes.
type SettingsPanel struct{ Row int }
type SettingsValues [SettingCount]string

func (s *SettingsPanel) Handle(in Input) Actions {
	result := Actions{Consumed: true}
	switch {
	case in.Up:
		s.Row = (s.Row + SettingCount - 1) % SettingCount
	case in.Down:
		s.Row = (s.Row + 1) % SettingCount
	case in.Left:
		result.SettingsDelta = -1
	case in.Right:
		result.SettingsDelta = 1
	}
	return result
}

func (s *SettingsPanel) Draw(c Canvas, values SettingsValues) {
	labels := [SettingCount]string{"font", "size", "theme", "cat"}
	w := 0.0
	for i, label := range labels {
		w = max(w, float64(c.TextCells(label)+c.TextCells(values[i])+6)*c.CellWidth)
	}
	padding := math.Round(12 * c.Scale)
	w = max(w, 70*c.CellWidth) + 2*padding
	h := float64(SettingCount+3)*c.CellHeight + 2*padding
	x := math.Round((c.Width-w)/2/c.CellWidth) * c.CellWidth
	y := math.Round((c.Height-h)/2/c.CellHeight) * c.CellHeight
	c.Panel(x, y, w, h)
	for i, label := range labels {
		at := y + padding + float64(i)*c.CellHeight
		fg, marker := c.Theme.Dim, "  "
		if i == s.Row {
			fg, marker = c.Theme.Text, "> "
		}
		c.Text(marker+label, x+padding, at, fg)
		c.Text(values[i], x+padding+10*c.CellWidth, at, fg)
	}
	c.Text("up/down to move, left/right or -/+ to change, enter to close",
		x+padding, y+padding+float64(SettingCount+2)*c.CellHeight, c.Theme.Dim)
}
