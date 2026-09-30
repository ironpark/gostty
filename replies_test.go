package gostty

import (
	"bytes"
	"errors"
	"io"
	"slices"
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
				t.Fatalf("WriteReplies error = %v, want %v, %v", err, tc.want, err)
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

func TestModeQueryReplies(t *testing.T) {
	for _, tc := range []struct {
		name, setup, query, want string
	}{
		{"ANSI set", "\x1b[4h", "\x1b[4$p", "\x1b[4;1$y"},
		{"ANSI reset", "\x1b[4l", "\x1b[4$p", "\x1b[4;2$y"},
		{"DEC namespace", "\x1b[4h\x1b[?4l", "\x1b[?4$p", "\x1b[?4;2$y"},
		{"unknown ANSI", "", "\x1b[9999$p", "\x1b[9999;0$y"},
		{"large ANSI does not alias insert", "\x1b[4h", "\x1b[32772$p", "\x1b[32772;0$y"},
		{"large DEC does not alias wraparound", "\x1b[?7h", "\x1b[?32775$p", "\x1b[?32775;0$y"},
		{"maximum mode", "", "\x1b[?65535$p", "\x1b[?65535;0$y"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, stream := newStreamPair(t, 20, 3)
			feed(t, stream, tc.setup)
			for split := 0; split <= len(tc.query); split++ {
				feed(t, stream, tc.query[:split])
				if split < len(tc.query) {
					if got := replyString(t, stream); got != "" {
						t.Fatalf("split %d: premature reply %q", split, got)
					}
				}
				feed(t, stream, tc.query[split:])
				if got := replyString(t, stream); got != tc.want {
					t.Fatalf("split %d: reply = %q, want %q", split, got, tc.want)
				}
			}
		})
	}
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
	if err := stream.OnClipboardReadRequest(func(req *ClipboardRequest) {
		_ = req.Deny(ClipboardDenialDenied)
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

// An APC or OSC this library does not implement is dropped silently until
// capture is turned on, which is how you find out a program is speaking a protocol you
// never wired up.
func TestUnknownSequence(t *testing.T) {
	_, stream := newStreamPair(t, 20, 3)

	unknown := func() []string {
		t.Helper()
		var out []string
		for _, ev := range drain(t, stream) {
			if ev.kind == StreamEventUnknownSequence {
				out = append(out, ev.sequenceKind.String()+":"+ev.sequence)
			}
		}
		return out
	}

	// "Z" is not a protocol ghostty implements. Off by default.
	feed(t, stream, "\x1b_Zhello\x1b\\")
	if got := unknown(); len(got) != 0 {
		t.Errorf("captured %q with capture off, want nothing", got)
	}

	if err := stream.SetUnknownMaxBytes(64); err != nil {
		t.Fatalf("SetUnknownMaxBytes: %v", err)
	}
	// OSC 7400 is not one ghostty implements either; the number stays in the
	// content so the caller can tell which one it was.
	feed(t, stream, "\x1b_Zhello\x1b\\\x1b]7400;status=busy\a")
	got := unknown()
	want := []string{"apc:Zhello", "osc:7400;status=busy"}
	if !slices.Equal(got, want) {
		t.Errorf("captured %q, want %q", got, want)
	}

	// And it can be turned back off.
	if err := stream.SetUnknownMaxBytes(0); err != nil {
		t.Fatalf("SetUnknownMaxBytes: %v", err)
	}
	feed(t, stream, "\x1b_Zhello\x1b\\")
	if got := unknown(); len(got) != 0 {
		t.Errorf("captured %q after turning capture off, want nothing", got)
	}
}

// A program that turned on in-band size reports (mode 2048) is told about a
// resize through the stream, and only through the stream: the terminal alone
// has no way to reach the program.
func TestStreamResizeReportsInBandSize(t *testing.T) {
	term, stream := newStreamPair(t, 20, 3)
	feed(t, stream, "\x1b[?2048h")
	replyString(t, stream) // enabling the mode may report the current size

	if err := term.ResizeCells(30, 5, 8, 16); err != nil {
		t.Fatalf("Terminal.ResizeCells: %v", err)
	}
	if got := replyString(t, stream); got != "" {
		t.Errorf("reply after Terminal.ResizeCells = %q, want none", got)
	}

	if err := stream.ResizeCells(40, 6, 8, 16); err != nil {
		t.Fatalf("Stream.ResizeCells: %v", err)
	}
	if got, want := replyString(t, stream), "\x1b[48;6;40;96;320t"; got != want {
		t.Errorf("reply after Stream.ResizeCells = %q, want %q", got, want)
	}
	if cols := term.Cols(); cols != 40 {
		t.Errorf("Cols = %d, want 40", cols)
	}
}

// Synchronized output (mode 2026) is reported as a render hold, begun and
// ended in pairs, and a resize ends it.
func TestRenderHold(t *testing.T) {
	_, stream := newStreamPair(t, 20, 3)

	holds := func() []bool {
		t.Helper()
		var out []bool
		for _, ev := range drain(t, stream) {
			if ev.kind == StreamEventRenderHold {
				out = append(out, ev.held)
			}
		}
		return out
	}

	// Setting the mode twice begins one hold.
	feed(t, stream, "\x1b[?2026h\x1b[?2026hframe\x1b[?2026l\x1b[?2026h")
	if got, want := holds(), []bool{true, false, true}; !slices.Equal(got, want) {
		t.Errorf("holds = %v, want %v", got, want)
	}

	if err := stream.ResizeCells(30, 5, 8, 16); err != nil {
		t.Fatalf("ResizeCells: %v", err)
	}
	if got, want := holds(), []bool{false}; !slices.Equal(got, want) {
		t.Errorf("holds after resize = %v, want %v", got, want)
	}
}

// A session resizes through its stream, so the program hears about it.
func TestSessionResizeReportsInBandSize(t *testing.T) {
	sess, err := New(20, 3)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer sess.Close()
	if _, err := sess.WriteString("\x1b[?2048h"); err != nil {
		t.Fatalf("WriteString: %v", err)
	}
	replyString(t, sess.Stream())

	if err := sess.ResizeCells(40, 6, 8, 16); err != nil {
		t.Fatalf("ResizeCells: %v", err)
	}
	if got, want := replyString(t, sess.Stream()), "\x1b[48;6;40;96;320t"; got != want {
		t.Errorf("reply = %q, want %q", got, want)
	}
}
