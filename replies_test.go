package gostty

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

type replyWriterFunc func([]byte) (int, error)

func (f replyWriterFunc) Write(p []byte) (int, error) { return f(p) }

func TestWriteRepliesRetainsOutputOnFailure(t *testing.T) {
	writeErr := errors.New("pty unavailable")
	for _, tc := range []struct {
		name   string
		writer io.Writer
		want   error
	}{
		{"error", replyWriterFunc(func([]byte) (int, error) { return 0, writeErr }), writeErr},
		{"short write", replyWriterFunc(func([]byte) (int, error) { return 0, nil }), io.ErrShortWrite},
		{"nil writer", nil, ErrNilStream},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, stream := newStreamPair(t, 20, 3)
			feed(t, stream, "\x1b[5n")
			if err := stream.WriteReplies(tc.writer); !errors.Is(err, tc.want) {
				t.Fatalf("WriteReplies error = %v, want %v", err, tc.want)
			}
			if has, err := stream.HasReplies(); err != nil || !has {
				t.Fatalf("HasReplies after failed write = %v, %v; want true, nil", has, err)
			}
			var buf bytes.Buffer
			if err := stream.WriteReplies(&buf); err != nil {
				t.Fatalf("retry WriteReplies: %v", err)
			}
			if got, want := buf.String(), "\x1b[0n"; got != want {
				t.Fatalf("retried reply = %q, want %q", got, want)
			}
			if has, err := stream.HasReplies(); err != nil || has {
				t.Fatalf("HasReplies after successful write = %v, %v; want false, nil", has, err)
			}
		})
	}
}

func TestWriteRepliesLargeOutput(t *testing.T) {
	_, stream := newStreamPair(t, 20, 3)
	// Exceed the binding adapter's staging buffer with ordered query replies.
	const count = 10000
	feed(t, stream, strings.Repeat("\x1b[5n\x1b[6n", count))
	var buf bytes.Buffer
	if err := stream.WriteReplies(&buf); err != nil {
		t.Fatalf("WriteReplies: %v", err)
	}
	if want := strings.Repeat("\x1b[0n\x1b[1;1R", count); buf.String() != want {
		t.Fatalf("reply content mismatch: got %d bytes, want %d", buf.Len(), len(want))
	}
	if err := stream.WriteReplies(&buf); err != nil {
		t.Fatalf("WriteReplies after drain: %v", err)
	}
	if want := len("\x1b[0n\x1b[1;1R") * count; buf.Len() != want {
		t.Fatalf("replies repeated after drain: got %d bytes, want %d", buf.Len(), want)
	}
}

// replyString feeds bytes and returns whatever the terminal answered.
func replyString(t *testing.T, stream *Stream) string {
	t.Helper()
	var buf strings.Builder
	if err := stream.WriteReplies(&buf); err != nil {
		t.Fatalf("WriteReplies: %v", err)
	}
	return buf.String()
}

// CSI c and its two variants. Nothing has to be configured for these: the
// answers describe the parser, and a program that asks blocks until it gets
// one.
func TestDeviceAttributes(t *testing.T) {
	_, stream := newStreamPair(t, 20, 3)

	// DA1: VT220 conformance (62), ANSI color (22). No clipboard callback is
	// installed, so 52 is absent.
	feed(t, stream, "\x1b[c")
	if got, want := replyString(t, stream), "\x1b[?62;22c"; got != want {
		t.Errorf("DA1 = %q, want %q", got, want)
	}

	// DA2 and DA3 answer too.
	feed(t, stream, "\x1b[>c")
	if got := replyString(t, stream); !strings.HasPrefix(got, "\x1b[>") {
		t.Errorf("DA2 = %q, want a CSI > reply", got)
	}
	feed(t, stream, "\x1b[=c")
	if got := replyString(t, stream); !strings.HasPrefix(got, "\x1bP!|") {
		t.Errorf("DA3 = %q, want a DECRPTUI reply", got)
	}
}

// The one part of the DA1 reply that varies is whether OSC 52 is served, and
// the stream knows that from its own wiring rather than from a declaration.
func TestDeviceAttributesClipboardDerived(t *testing.T) {
	_, stream := newStreamPair(t, 20, 3)
	if err := stream.OnClipboardReadRequest(func() {
		_ = stream.DenyClipboard(ClipboardDenialDenied)
	}); err != nil {
		t.Fatalf("OnClipboardReadRequest: %v", err)
	}
	feed(t, stream, "\x1b[c")
	if got, want := replyString(t, stream), "\x1b[?62;22;52c"; got != want {
		t.Errorf("DA1 with a clipboard callback = %q, want %q", got, want)
	}
}

// XTVERSION always answers; without SetVersionReport it names the parser,
// which is no use as an application identity.
func TestVersionReport(t *testing.T) {
	_, stream := newStreamPair(t, 20, 3)

	feed(t, stream, "\x1b[>0q")
	if got, want := replyString(t, stream), "\x1bP>|libghostty\x1b\\"; got != want {
		t.Errorf("XTVERSION default = %q, want %q", got, want)
	}

	if err := stream.SetVersionReport("hypercat", "0.1.0"); err != nil {
		t.Fatalf("SetVersionReport: %v", err)
	}
	feed(t, stream, "\x1b[>0q")
	if got, want := replyString(t, stream), "\x1bP>|hypercat 0.1.0\x1b\\"; got != want {
		t.Errorf("XTVERSION = %q, want %q", got, want)
	}

	// A name with no version is reported bare rather than with a trailing
	// space.
	if err := stream.SetVersionReport("hypercat", ""); err != nil {
		t.Fatalf("SetVersionReport: %v", err)
	}
	feed(t, stream, "\x1b[>0q")
	if got, want := replyString(t, stream), "\x1bP>|hypercat\x1b\\"; got != want {
		t.Errorf("XTVERSION with no version = %q, want %q", got, want)
	}
}

// ENQ answers nothing until something is set: an answerback string is echoed
// to any program that sends one control byte.
func TestEnquiryResponse(t *testing.T) {
	_, stream := newStreamPair(t, 20, 3)

	feed(t, stream, "\x05")
	if got := replyString(t, stream); got != "" {
		t.Errorf("ENQ with no answerback = %q, want empty", got)
	}

	if err := stream.SetEnquiryResponse("hypercat"); err != nil {
		t.Fatalf("SetEnquiryResponse: %v", err)
	}
	feed(t, stream, "\x05")
	if got, want := replyString(t, stream), "hypercat"; got != want {
		t.Errorf("ENQ = %q, want %q", got, want)
	}
}

// The color scheme serves both halves of the protocol: the query, and the
// unsolicited report a program subscribes to with mode 2031.
func TestColorScheme(t *testing.T) {
	_, stream := newStreamPair(t, 20, 3)

	// Unanswered until the embedder says what the desktop is doing.
	feed(t, stream, "\x1b[?996n")
	if got := replyString(t, stream); got != "" {
		t.Errorf("CSI ? 996 n before any scheme = %q, want empty", got)
	}

	if err := stream.ColorSchemeChanged(ColorSchemeDark); err != nil {
		t.Fatalf("ColorSchemeChanged: %v", err)
	}
	// Mode 2031 is off, so setting it wrote nothing on its own.
	if got := replyString(t, stream); got != "" {
		t.Errorf("ColorSchemeChanged with mode 2031 off wrote %q, want nothing", got)
	}
	feed(t, stream, "\x1b[?996n")
	if got, want := replyString(t, stream), "\x1b[?997;1n"; got != want {
		t.Errorf("CSI ? 996 n = %q, want %q (dark)", got, want)
	}

	// With mode 2031 on, a change tells the program without being asked.
	feed(t, stream, "\x1b[?2031h")
	_ = replyString(t, stream)
	if err := stream.ColorSchemeChanged(ColorSchemeLight); err != nil {
		t.Fatalf("ColorSchemeChanged: %v", err)
	}
	if got, want := replyString(t, stream), "\x1b[?997;2n"; got != want {
		t.Errorf("mode 2031 report = %q, want %q (light)", got, want)
	}

	// Setting the same scheme again is not a change and reports nothing.
	if err := stream.ColorSchemeChanged(ColorSchemeLight); err != nil {
		t.Fatalf("ColorSchemeChanged: %v", err)
	}
	if got := replyString(t, stream); got != "" {
		t.Errorf("re-reporting the same scheme wrote %q, want nothing", got)
	}

	// Clearing goes back to leaving the query alone.
	if err := stream.ClearColorScheme(); err != nil {
		t.Fatalf("ClearColorScheme: %v", err)
	}
	feed(t, stream, "\x1b[?996n")
	if got := replyString(t, stream); got != "" {
		t.Errorf("CSI ? 996 n after ClearColorScheme = %q, want empty", got)
	}
}

// An APC this library does not implement is dropped silently until capture is
// turned on, which is how you find out a program is speaking a protocol you
// never wired up.
func TestUnknownSequence(t *testing.T) {
	_, stream := newStreamPair(t, 20, 3)

	drain := func() []string {
		t.Helper()
		var out []string
		for event, err := range stream.Events() {
			if err != nil {
				t.Fatalf("Events: %v", err)
			}
			if event != StreamEventUnknownSequence {
				continue
			}
			data, err := stream.EventSequence()
			if err != nil {
				t.Fatalf("EventSequence: %v", err)
			}
			out = append(out, string(data))
		}
		return out
	}

	// "Z" is not a protocol ghostty implements. Off by default.
	feed(t, stream, "\x1b_Zhello\x1b\\")
	if got := drain(); len(got) != 0 {
		t.Errorf("captured %q with capture off, want nothing", got)
	}

	if err := stream.SetUnknownMaxBytes(64); err != nil {
		t.Fatalf("SetUnknownMaxBytes: %v", err)
	}
	feed(t, stream, "\x1b_Zhello\x1b\\")
	got := drain()
	if len(got) != 1 || got[0] != "Zhello" {
		t.Errorf("captured %q, want [\"Zhello\"]", got)
	}

	// And it can be turned back off.
	if err := stream.SetUnknownMaxBytes(0); err != nil {
		t.Fatalf("SetUnknownMaxBytes: %v", err)
	}
	feed(t, stream, "\x1b_Zhello\x1b\\")
	if got := drain(); len(got) != 0 {
		t.Errorf("captured %q after turning capture off, want nothing", got)
	}
}
