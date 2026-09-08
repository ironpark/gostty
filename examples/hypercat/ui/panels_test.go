package ui

import "testing"

func TestPanelsConsumeOnlyUIInput(t *testing.T) {
	var p Panels
	if got := p.Handle(Input{Chars: []rune("shell")}); got.Consumed {
		t.Fatal("closed panels consumed shell input")
	}
	if got := p.Handle(Input{OpenSearch: true}); !got.Consumed || !got.ResetSearch || p.Mode != Search {
		t.Fatal("search did not open")
	}
	got := p.Handle(Input{Chars: []rune("가a\n"), Backspace: true})
	if string(p.Search.Query) != "가" || !got.QueryChanged || got.MatchStep != 1 {
		t.Fatalf("query edit = %q, actions = %+v", p.Search.Query, got)
	}
	got = p.Handle(Input{Enter: true, Shift: true})
	if got.QueryChanged || got.MatchStep != -1 {
		t.Fatalf("previous match actions = %+v", got)
	}
	got = p.Handle(Input{OpenSearch: true})
	if got.ResetSearch || string(p.Search.Query) != "가" {
		t.Fatal("repeated search shortcut cleared the query")
	}
	p.Search.Matches = 4
	got = p.Handle(Input{Close: true})
	if p.Mode != None || !got.Consumed || !got.ResetSearch || p.Search.Matches != 0 {
		t.Fatal("closing search did not reset its results")
	}
	if got := p.Handle(Input{Enter: true}); got.Consumed {
		t.Fatal("closed panel retained keyboard input")
	}
}

func TestSettingsNavigationAndSearchTransition(t *testing.T) {
	var p Panels
	p.Handle(Input{OpenSettings: true})
	p.Handle(Input{Up: true})
	if p.Settings.Row != SettingCat {
		t.Fatal("up did not wrap to cat setting")
	}
	got := p.Handle(Input{Right: true})
	if !got.Consumed || got.SettingsDelta != 1 {
		t.Fatal("settings change was not returned to host")
	}
	p.Handle(Input{Down: true})
	if p.Settings.Row != SettingFont {
		t.Fatal("down did not wrap to font")
	}
	p.Search.Query = []rune("old")
	got = p.Handle(Input{OpenSearch: true})
	if !got.ResetSearch || len(p.Search.Query) != 0 {
		t.Fatal("switching to search retained stale query")
	}
	p.Handle(Input{OpenSettings: true})
	got = p.Handle(Input{Enter: true})
	if p.Mode != None || !got.ResetSearch || !got.Consumed {
		t.Fatal("enter did not close settings")
	}
}

func TestBackspaceRemovesOneRune(t *testing.T) {
	s := SearchBar{Query: []rune("a한")}
	s.Handle(Input{Backspace: true})
	if string(s.Query) != "a" {
		t.Fatalf("query = %q", s.Query)
	}
	s.Handle(Input{Backspace: true})
	if s.Handle(Input{Backspace: true}).QueryChanged {
		t.Fatal("empty backspace changed query")
	}
}
