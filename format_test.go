package gostty

import (
	"bytes"
	"strings"
	"testing"
)

// formatString runs Terminal.Format into a string.
func formatString(term *Terminal, opts FormatOptions) (string, error) {
	var buf bytes.Buffer
	err := term.Format(opts, &buf)
	return buf.String(), err
}

// Format emits the screen as plain text, VT or HTML; the zero options are
// plain, trimmed, with styling where the format carries it.
func TestFormat(t *testing.T) {
	term, stream := newStreamPair(t, 20, 3)
	feed(t, stream, "plain \x1b[1mbold\x1b[0m   \r\nnext")

	var buf bytes.Buffer
	if err := term.Format(FormatOptions{}, &buf); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); got != "plain bold\nnext" {
		t.Errorf("plain = %q", got)
	}

	buf.Reset()
	if err := term.Format(FormatOptions{KeepTrailingWhitespace: true}, &buf); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); !strings.HasPrefix(got, "plain bold   ") {
		t.Errorf("untrimmed = %q", got)
	}

	buf.Reset()
	if err := term.Format(FormatOptions{Format: FormatterFormatVt}, &buf); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); !strings.Contains(got, "\x1b[1m") || !strings.Contains(got, "\r\n") {
		t.Errorf("vt = %q; want SGR bold and CRLF", got)
	}

	buf.Reset()
	if err := term.Format(FormatOptions{Format: FormatterFormatHtml}, &buf); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); !strings.Contains(got, "bold") || !strings.Contains(got, "<") {
		t.Errorf("html = %q", got)
	}

	screen, err := term.ActiveScreen()
	if err != nil {
		t.Fatal(err)
	}
	buf.Reset()
	if err := screen.Format(FormatOptions{}, &buf); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); got != "plain bold\nnext" {
		t.Errorf("Screen.Format = %q", got)
	}
	buf.Reset()
	ok, err := screen.FormatSelection(FormatOptions{}, Selection{StartX: 6, StartY: 0, EndX: 9, EndY: 0}, &buf)
	if err != nil || !ok || buf.String() != "bold" {
		t.Errorf("FormatSelection = %q, %v, %v", buf.String(), ok, err)
	}
	if ok, _ := screen.FormatSelection(FormatOptions{}, Selection{StartY: 99, EndY: 99}, &buf); ok {
		t.Error("FormatSelection outside the screen reported ok")
	}
	if f, err := ParseFormatterFormat("html"); err != nil || f != FormatterFormatHtml {
		t.Errorf("ParseFormatterFormat = %v, %v", f, err)
	}
}

// Graphemes returns the whole cluster, which RenderCell.Codepoint truncates.
func TestRenderGraphemes(t *testing.T) {
	term, stream := newStreamPair(t, 10, 2)
	// Mode 2027 keeps a ZWJ sequence (man + ZWJ + laptop) in one cell; without
	// it ghostty lays the parts out as separate wide cells.
	feed(t, stream, "\x1b[?2027ha\U0001F468‍\U0001F4BB")
	state, err := NewRenderState()
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	if err := state.Update(term); err != nil {
		t.Fatal(err)
	}
	dst := make([]rune, 8)
	n, err := state.Graphemes(0, 0, dst)
	if err != nil || n != 1 || dst[0] != 'a' {
		t.Errorf("Graphemes(0,0) = %v %q, %v", n, dst[:n], err)
	}
	n, err = state.Graphemes(1, 0, dst)
	if err != nil || string(dst[:n]) != "\U0001F468‍\U0001F4BB" {
		t.Errorf("Graphemes(1,0) = %v %q, %v", n, dst[:n], err)
	}
	if n, err := state.Graphemes(9, 0, dst); err != nil || n != 0 {
		t.Errorf("Graphemes(empty) = %v, %v", n, err)
	}
	if _, err := state.Graphemes(1, 0, dst[:1]); err == nil {
		t.Error("Graphemes with a short buffer returned no error")
	}
}

// HyperlinkAt reads the OSC 8 link under a viewport cell.
func TestRenderHyperlinkAt(t *testing.T) {
	term, stream := newStreamPair(t, 20, 2)
	feed(t, stream, "\x1b]8;;https://example.com\x1b\\link\x1b]8;;\x1b\\ text")
	state, err := NewRenderState()
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	if err := state.Update(term); err != nil {
		t.Fatal(err)
	}
	uri, ok, err := state.HyperlinkAt(2, 0)
	if err != nil || !ok || uri != "https://example.com" {
		t.Errorf("HyperlinkAt(2,0) = %q, %v, %v", uri, ok, err)
	}
	if _, ok, err := state.HyperlinkAt(6, 0); err != nil || ok {
		t.Errorf("HyperlinkAt(6,0) = ok %v, %v; want none", ok, err)
	}
	if _, ok, err := state.HyperlinkAt(50, 50); err != nil || ok {
		t.Errorf("HyperlinkAt off grid = ok %v, %v", ok, err)
	}
}
