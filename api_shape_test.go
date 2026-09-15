package gostty

import (
	"bytes"
	"fmt"
	"image/color"
	"strings"
	"testing"
)

// New is the short way in: one handle, one Close, and an io.Writer for VT data.
func TestNewSessionWritesAndFormats(t *testing.T) {
	session, err := New(80, 24)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer session.Close()

	if _, err := fmt.Fprintf(session, "Hello, \x1b[1;32mworld\x1b[0m!\r\n"); err != nil {
		t.Fatalf("Fprintf: %v", err)
	}
	got, err := session.PlainText()
	if err != nil {
		t.Fatalf("PlainText: %v", err)
	}
	if want := "Hello, world!"; strings.TrimSpace(got) != want {
		t.Errorf("PlainText() = %q, want %q", got, want)
	}
	// The pieces stay reachable; the session only owns them.
	if session.Terminal() == nil || session.Stream() == nil {
		t.Error("New() left the session without a terminal or a stream")
	}
}

func TestMustNewPanicsOnBadSize(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("MustNew(0, 0) did not panic")
		}
	}()
	_ = MustNew(0, 0)
}

// RGB says which byte is which, rather than leaving a caller to shift an
// integer apart, and it is a color.Color so it can be drawn with directly.
func TestRGB(t *testing.T) {
	c := RGB{0x11, 0x22, 0x33}
	if c.R() != 0x11 || c.G() != 0x22 || c.B() != 0x33 {
		t.Fatalf("channel accessors disagree with RGB byte order: %v", c)
	}
	if got := NewRGB(0x11, 0x22, 0x33); got != c {
		t.Fatalf("NewRGB = %v, want %v", got, c)
	}
	for _, sample := range []RGB{{}, {0xff, 0x80, 0x01}} {
		r, g, b, a := sample.RGBA()
		if r != uint32(sample[0])*257 || g != uint32(sample[1])*257 || b != uint32(sample[2])*257 || a != 0xffff {
			t.Errorf("%v.RGBA() = %x %x %x %x", sample, r, g, b, a)
		}
	}
	if got := c.Uint32(); got != 0x112233 {
		t.Errorf("Uint32() = %#06x, want 0x112233", got)
	}
	if got := RGBFromUint32(0x112233); got != c {
		t.Errorf("RGBFromUint32(0x112233) = %v, want %v", got, c)
	}
	if got := c.String(); got != "#112233" {
		t.Errorf("String() = %q, want %q", got, "#112233")
	}

	// A terminal colour can be handed to image/color unconverted.
	var asColor color.Color = c
	if got := color.RGBAModel.Convert(asColor).(color.RGBA); got != (color.RGBA{R: 0x11, G: 0x22, B: 0x33, A: 0xff}) {
		t.Errorf("through color.RGBAModel = %v, want opaque 112233", got)
	}

	for _, text := range []string{"#112233", "112233", "#112233", "#AABBCC"} {
		if _, err := ParseRGB(text); err != nil {
			t.Errorf("ParseRGB(%q): %v", text, err)
		}
	}
	for _, text := range []string{"", "#fff", "#11223", "#1122334", "#gg1122"} {
		if got, err := ParseRGB(text); err == nil {
			t.Errorf("ParseRGB(%q) = %v, want an error", text, got)
		}
	}
	if got, err := ParseRGB("#aAbBcC"); err != nil || got != NewRGB(0xaa, 0xbb, 0xcc) {
		t.Errorf("ParseRGB(%q) = %v, %v; want #aabbcc", "#aAbBcC", got, err)
	}
}

// Round-trip a colour through the terminal in the type the API now uses.
func TestRGBThroughTerminal(t *testing.T) {
	term, stream := newStreamPair(t, 20, 3)
	if err := term.SetDefaultBackgroundColor(NewRGB(0x10, 0x20, 0x30)); err != nil {
		t.Fatalf("SetDefaultBackgroundColor: %v", err)
	}
	bg, ok, err := term.BackgroundColor()
	if err != nil || !ok {
		t.Fatalf("BackgroundColor() = ok %v, %v", ok, err)
	}
	if bg != NewRGB(0x10, 0x20, 0x30) {
		t.Errorf("BackgroundColor() = %v, want #102030", bg)
	}

	// OSC 11 from the program, read back through the same type.
	feed(t, stream, "\x1b]11;rgb:aa/bb/cc\x1b\\")
	if bg, _, _ := term.BackgroundColor(); bg != NewRGB(0xaa, 0xbb, 0xcc) {
		t.Errorf("BackgroundColor() after OSC 11 = %v, want #aabbcc", bg)
	}
}

// A Formatter holds its options and its buffer, so a renderer formatting every
// frame pays for neither again.
func TestFormatter(t *testing.T) {
	term, stream := newStreamPair(t, 20, 3)
	feed(t, stream, "one\r\ntwo\r\n")

	f := term.Formatter(FormatOptions{Format: FormatterFormatPlain})
	first, err := f.String()
	if err != nil {
		t.Fatalf("Formatter.String: %v", err)
	}
	if want := "one\ntwo"; strings.TrimSpace(first) != want {
		t.Errorf("Formatter.String() = %q, want %q", first, want)
	}

	// Bytes reuses the formatter's own buffer; the contents still match.
	raw, err := f.Bytes()
	if err != nil {
		t.Fatalf("Formatter.Bytes: %v", err)
	}
	if string(raw) != first {
		t.Errorf("Bytes() = %q, String() = %q; want them equal", raw, first)
	}

	// WriteTo reports what it wrote and buffers nothing on the way.
	var sink bytes.Buffer
	n, err := f.WriteTo(&sink)
	if err != nil {
		t.Fatalf("Formatter.WriteTo: %v", err)
	}
	if int(n) != sink.Len() || sink.String() != first {
		t.Errorf("WriteTo() wrote %d bytes %q, want %d bytes %q", n, sink.String(), len(first), first)
	}

	// Append is the allocation-free form: the same slice comes back extended.
	dst := make([]byte, 0, 64)
	dst = append(dst, "prefix:"...)
	dst, err = f.Append(dst)
	if err != nil {
		t.Fatalf("Formatter.Append: %v", err)
	}
	if want := "prefix:" + first; string(dst) != want {
		t.Errorf("Append() = %q, want %q", dst, want)
	}

	// The options are the formatter's, and changing them changes the output.
	f.SetOptions(FormatOptions{Format: FormatterFormatVt})
	vt, err := f.String()
	if err != nil {
		t.Fatalf("Formatter.String after SetOptions: %v", err)
	}
	if vt == first {
		t.Error("SetOptions(VT) produced the same output as plain")
	}
	if f.Options().Format != FormatterFormatVt {
		t.Errorf("Options().Format = %v, want VT", f.Options().Format)
	}
}

// A screen formatter stays with its screen; a terminal formatter follows the
// active one. Both reach scrollback.
func TestFormatterTargets(t *testing.T) {
	term, stream := newStreamPair(t, 10, 2)
	if err := term.SetScrollbackMaxBytes(1 << 20); err != nil {
		t.Fatalf("SetScrollbackMaxBytes: %v", err)
	}
	for _, line := range []string{"one", "two", "three", "four"} {
		feed(t, stream, line+"\r\n")
	}

	screen, err := term.ActiveScreen()
	if err != nil {
		t.Fatalf("ActiveScreen: %v", err)
	}
	whole, err := screen.Formatter(FormatOptions{Format: FormatterFormatPlain}).String()
	if err != nil {
		t.Fatalf("Screen formatter: %v", err)
	}
	if !strings.Contains(whole, "one") {
		t.Errorf("screen formatter = %q, want it to reach scrollback", whole)
	}

	// The terminal's formatter follows the active screen; on the primary
	// screen it produces the same text as that screen's own formatter.
	active, err := term.Formatter(FormatOptions{Format: FormatterFormatPlain}).String()
	if err != nil {
		t.Fatalf("Terminal formatter: %v", err)
	}
	if active != whole {
		t.Errorf("terminal formatter = %q, screen formatter = %q; want them equal on the active screen", active, whole)
	}

	// Switching screens changes what the terminal's formatter sees and leaves
	// the screen's formatter where it was.
	if _, _, err := term.SwitchScreen(ScreenKeyAlternate); err != nil {
		t.Fatalf("SwitchScreen: %v", err)
	}
	alternate, err := term.Formatter(FormatOptions{Format: FormatterFormatPlain}).String()
	if err != nil {
		t.Fatalf("Terminal formatter on the alternate screen: %v", err)
	}
	if strings.Contains(alternate, "one") {
		t.Errorf("terminal formatter after switching = %q, want the empty alternate screen", alternate)
	}
	again, err := screen.Formatter(FormatOptions{Format: FormatterFormatPlain}).String()
	if err != nil {
		t.Fatalf("Screen formatter after switching: %v", err)
	}
	if again != whole {
		t.Errorf("screen formatter after switching = %q, want the primary screen %q", again, whole)
	}
}

// A Formatter owns no native resource, so it has no Close; a call after its
// target is gone reports the same error a direct call would.
func TestFormatterAfterClose(t *testing.T) {
	term, err := NewTerminal(10, 2)
	if err != nil {
		t.Fatalf("NewTerminal: %v", err)
	}
	f := term.Formatter(FormatOptions{Format: FormatterFormatPlain})
	if err := term.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := f.String(); err == nil {
		t.Error("Formatter.String() after Close returned no error")
	}
}
