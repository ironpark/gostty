package gostty

import "testing"

// SwitchScreenMode is the DEC private mode form of the screen switch: 1049
// saves the cursor on entry and restores it, and the primary screen, on exit.
func TestSwitchScreenMode(t *testing.T) {
	term, _ := newStreamPair(t, 20, 3)
	if err := term.PrintString("primary"); err != nil {
		t.Fatal(err)
	}
	if err := term.SwitchScreenMode(SwitchScreenMode1049, true); err != nil {
		t.Fatal(err)
	}
	if key, err := term.ActiveScreenKey(); err != nil || key != ScreenKeyAlternate {
		t.Fatalf("ActiveScreenKey() = %v, %v; want alternate", key, err)
	}
	if err := term.PrintString("alternate"); err != nil {
		t.Fatal(err)
	}
	if err := term.SwitchScreenMode(SwitchScreenMode1049, false); err != nil {
		t.Fatal(err)
	}
	if key, err := term.ActiveScreenKey(); err != nil || key != ScreenKeyPrimary {
		t.Fatalf("ActiveScreenKey() = %v, %v; want primary", key, err)
	}
	got, err := term.PlainString()
	if err != nil || got != "primary" {
		t.Errorf("PlainString() = %q, %v; want %q", got, err, "primary")
	}
}

// A hyperlink opened on the screen wraps the cells printed until it ends;
// the text itself is unchanged.
func TestHyperlink(t *testing.T) {
	term, _ := newStreamPair(t, 20, 3)
	screen, err := term.ActiveScreen()
	if err != nil {
		t.Fatal(err)
	}
	if err := screen.StartHyperlink("https://example.com", "x"); err != nil {
		t.Fatal(err)
	}
	if err := term.PrintString("link"); err != nil {
		t.Fatal(err)
	}
	if err := screen.EndHyperlink(); err != nil {
		t.Fatal(err)
	}
	if err := screen.StartHyperlink("https://example.org", ""); err != nil {
		t.Fatal(err)
	}
	if err := screen.EndHyperlink(); err != nil {
		t.Fatal(err)
	}
	got, err := term.PlainString()
	if err != nil || got != "link" {
		t.Errorf("PlainString() = %q, %v; want %q", got, err, "link")
	}
}

func TestSearchNeedle(t *testing.T) {
	term, _ := newStreamPair(t, 20, 3)
	screen, err := term.ActiveScreen()
	if err != nil {
		t.Fatal(err)
	}
	search, err := screen.NewSearch("needle")
	if err != nil {
		t.Fatal(err)
	}
	defer search.Close()
	if got, err := search.Needle(); err != nil || got != "needle" {
		t.Errorf("Needle() = %q, %v; want %q", got, err, "needle")
	}
}

func TestSetKittyGraphicsSizeLimit(t *testing.T) {
	term, _ := newStreamPair(t, 20, 3)
	if err := term.SetKittyGraphicsSizeLimit(1 << 20); err != nil {
		t.Fatal(err)
	}
}
