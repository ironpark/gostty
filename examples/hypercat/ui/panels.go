// Package ui provides HyperCat's panels, tab bar, and themes. Components
// receive input snapshots and return actions; they own no terminal or shell.
package ui

type Mode int

const (
	None Mode = iota
	Search
	Settings
)

// Panels routes keyboard input to the visible component.
type Panels struct {
	Mode     Mode
	Search   SearchBar
	Settings SettingsPanel
}

type Input struct {
	OpenSearch, OpenSettings, Close bool
	Enter, Shift, Backspace         bool
	Up, Down, Left, Right           bool
	Chars                           []rune
}

// Actions describes work the host must perform after updating the UI.
type Actions struct {
	Consumed, ResetSearch, QueryChanged bool
	MatchStep                           int // +1 next match, -1 previous match
	SettingsDelta                       int
}

func (p *Panels) OpenSettings() { p.Mode = Settings; p.Settings.Row = SettingFont }

func (p *Panels) Handle(in Input) Actions {
	if in.OpenSearch {
		reset := p.Mode != Search
		if reset {
			p.Mode = Search
			p.Search = SearchBar{Query: p.Search.Query[:0]}
		}
		return Actions{Consumed: true, ResetSearch: reset}
	}
	if in.OpenSettings {
		p.OpenSettings()
		return Actions{Consumed: true}
	}
	if p.Mode == None {
		return Actions{}
	}
	if in.Close {
		p.Mode = None
		p.Search.Matches, p.Search.Failure = 0, ""
		return Actions{Consumed: true, ResetSearch: true}
	}
	if p.Mode == Search {
		return p.Search.Handle(in)
	}
	result := p.Settings.Handle(in)
	if in.Enter && !in.Up && !in.Down && !in.Left && !in.Right {
		p.Mode = None
		p.Search.Matches, p.Search.Failure = 0, ""
		result.ResetSearch = true
	}
	return result
}

func (p *Panels) Draw(c Canvas, values SettingsValues) {
	switch p.Mode {
	case Search:
		p.Search.Draw(c)
	case Settings:
		p.Settings.Draw(c, values)
	}
}
