package ui

import (
	"fmt"

	"github.com/hajimehoshi/ebiten/v2/vector"
)

const TabBarHeight = 34

type TabBar struct{ firstVisible int }

type TabActionKind int

const (
	TabNone TabActionKind = iota
	TabSelect
	TabClose
	TabAdd
)

type TabAction struct {
	Kind  TabActionKind
	Index int
}

// TabLayout is shared by drawing and hit testing, including overflow.
type TabLayout struct {
	First, Slots                   int
	Width, Height, TabWidth, Scale float64
}

func (bar *TabBar) Layout(width, scale float64, active, count int) TabLayout {
	if scale <= 0 {
		scale = 1
	}
	height := float64(int(TabBarHeight * scale))
	available := max(width-height, 1)
	slots := min(max(int(available/(140*scale)), 1), count)
	layout := TabLayout{Width: width, Height: height, Scale: scale, Slots: slots}
	if slots == 0 {
		return layout
	}
	bar.firstVisible = min(bar.firstVisible, max(count-slots, 0))
	if active < bar.firstVisible {
		bar.firstVisible = active
	}
	if active >= bar.firstVisible+slots {
		bar.firstVisible = active - slots + 1
	}
	layout.First, layout.TabWidth = bar.firstVisible, min(available/float64(slots), 240*scale)
	return layout
}

func (l TabLayout) Hit(x, y float64) TabAction {
	if x < 0 || x >= l.Width || y < 0 || y >= l.Height {
		return TabAction{}
	}
	if x >= l.Width-l.Height {
		return TabAction{Kind: TabAdd}
	}
	if l.Slots == 0 || l.TabWidth <= 0 {
		return TabAction{}
	}
	slot := int(x / l.TabWidth)
	if slot >= l.Slots {
		return TabAction{}
	}
	action := TabAction{Kind: TabSelect, Index: l.First + slot}
	if x-float64(slot)*l.TabWidth >= l.TabWidth-24*l.Scale {
		action.Kind = TabClose
	}
	return action
}

func (bar *TabBar) Draw(c Canvas, active, count int, title func(int) string) {
	l := bar.Layout(c.Width, c.Scale, active, count)
	for slot := 0; slot < l.Slots; slot++ {
		index := l.First + slot
		x := float64(slot) * l.TabWidth
		if index == active {
			vector.FillRect(c.Screen, float32(x), 0, float32(l.TabWidth), float32(l.Height), c.Theme.Background, false)
			vector.FillRect(c.Screen, float32(x), float32(l.Height-2*c.Scale), float32(l.TabWidth), float32(2*c.Scale), c.Theme.Accent, false)
		}
		label := title(index)
		if label == "" {
			label = "Shell"
		}
		label = FitLabel(fmt.Sprintf("%d %s", index+1, label), int((l.TabWidth-40*c.Scale)/c.CellWidth), c.RuneWidth)
		c.Text(label, x+8*c.Scale, (l.Height-c.CellHeight)/2, c.Theme.Text)
		c.Text("×", x+l.TabWidth-20*c.Scale, (l.Height-c.CellHeight)/2, c.Theme.Dim)
	}
	c.Text("+", c.Width-l.Height+10*c.Scale, (l.Height-c.CellHeight)/2, c.Theme.Text)
}

// FitLabel keeps wide titles within their allotted cell count.
func FitLabel(label string, cells int, runeWidth func(rune) int) string {
	out := []rune{}
	used := 0
	for _, r := range label {
		if r < ' ' {
			continue
		}
		width := max(runeWidth(r), 0)
		if used+width > cells {
			break
		}
		out = append(out, r)
		used += width
	}
	return string(out)
}
