package gostty

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
)

func TestEventValuesCaptureAndRetainPayloads(t *testing.T) {
	_, s := newStreamPair(t, 20, 3)
	if err := s.SetUnknownMaxBytes(64); err != nil {
		t.Fatal(err)
	}
	feed(t, s, "\x1b]2;first\x07\x1b]7;file:///first\x07\x1b]2;second\x07\x1b]7;file:///second\x07"+
		"\x1b]777;notify;notice;body\x07\x1b]9;4;1;42\x07\x1b_unknown\x1b\\")
	var got []Event
	for event, err := range s.EventValues() {
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, event)
	}
	if len(got) != 7 {
		t.Fatalf("events = %+v", got)
	}
	if got[0].Kind != StreamEventTitleChanged || got[0].Title != "first" || got[2].Title != "second" {
		t.Fatalf("titles were not captured at emission: %+v", got)
	}
	if got[1].Pwd != "file:///first" || got[3].Pwd != "file:///second" {
		t.Fatalf("directories: %+v", got)
	}
	if got[4].Title != "notice" || got[4].Body != "body" {
		t.Fatalf("notification: %+v", got[4])
	}
	if got[5].Progress != 42 || !got[5].HasProgress || got[5].ProgressState != ProgressStateSet {
		t.Fatalf("progress: %+v", got[5])
	}
	if string(got[6].Sequence) != "unknown" {
		t.Fatalf("sequence: %q", got[6].Sequence)
	}
	feed(t, s, "\x1b]2;third\x07")
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if got[0].Title != "first" || got[4].Body != "body" || string(got[6].Sequence) != "unknown" {
		t.Fatal("retained payload changed")
	}
	yielded := 0
	for _, err := range s.EventValues() {
		yielded++
		if !errors.Is(err, ErrInvalidHandle) {
			t.Fatalf("closed stream: %v", err)
		}
	}
	if yielded != 1 {
		t.Fatalf("error yielded %d times", yielded)
	}
}

func TestEventValuesShareQueueAndStopEarly(t *testing.T) {
	_, s := newStreamPair(t, 10, 2)
	feed(t, s, "\a\x1b]2;next\x07\a")
	for event, err := range s.EventValues() {
		if err != nil || event.Kind != StreamEventBell {
			t.Fatalf("first = %+v, %v", event, err)
		}
		break
	}
	next, ok, err := s.NextEventValue()
	if err != nil || !ok || next.Kind != StreamEventTitleChanged || next.Title != "next" {
		t.Fatalf("next = %+v, %v, %v", next, ok, err)
	}
	event, ok, err := s.NextEventValue()
	if err != nil || !ok || event.Kind != StreamEventBell {
		t.Fatalf("last = %+v, %v, %v", event, ok, err)
	}
	if _, ok, err := s.NextEventValue(); ok || err != nil {
		t.Fatalf("empty = %v, %v", ok, err)
	}
}

func TestFeedUntilGroundBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, prefix, suffix string
		consumed             uint
		reached              bool
	}{
		{"already ground", "", "untouched", 0, true},
		{"CSI", "\x1b[31", "mX", 1, true},
		{"incomplete", "\x1b[", "31", 2, false},
		{"empty incomplete", "\x1b[", "", 0, false},
		{"OSC", "\x1b]2;title", "\x07X", 1, true},
		{"UTF8", "\xe2", "\x82\xacX", 2, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			term, s := newStreamPair(t, 20, 2)
			feed(t, s, tc.prefix)
			result, err := s.FeedUntilGround([]byte(tc.suffix))
			if err != nil || result.Consumed != tc.consumed || result.Reached != tc.reached {
				t.Fatalf("result = %+v, %v", result, err)
			}
			if ground, err := s.AtGround(); err != nil || ground != tc.reached {
				t.Fatalf("ground = %v, %v", ground, err)
			}
			text, err := term.PlainString()
			if err != nil || strings.Contains(text, "X") || strings.Contains(text, "untouched") {
				t.Fatalf("unconsumed text applied: %q, %v", text, err)
			}
		})
	}
}

func TestRenderMetadataSnapshot(t *testing.T) {
	term, s := newStreamPair(t, 10, 3)
	state, err := NewRenderState()
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	feed(t, s, "\x1b[2;4H\x1b[5 q\x1b]10;#112233\x07\x1b]11;#445566\x07\x1b]12;#778899\x07")
	if err := state.Update(term); err != nil {
		t.Fatal(err)
	}
	cursor, err := state.Cursor()
	if err != nil {
		t.Fatal(err)
	}
	if cursor.X != 3 || cursor.Y != 1 || !cursor.ViewportHasValue || !cursor.Visible || !cursor.Blinking || cursor.Style != CursorStyleBar {
		t.Fatalf("cursor = %+v", cursor)
	}
	colors, err := state.Colors()
	if err != nil {
		t.Fatal(err)
	}
	if colors.Foreground.Uint32() != 0x112233 || colors.Background.Uint32() != 0x445566 || colors.Cursor.Uint32() != 0x778899 || !colors.CursorHasValue {
		t.Fatalf("colors = %+v", colors)
	}
	feed(t, s, "\x1b[1;1H\x1b[?25l\x1b]11;#000000\x07")
	if beforeUpdate, err := state.Cursor(); err != nil || beforeUpdate != cursor {
		t.Fatalf("snapshot changed without update: %+v, %v", beforeUpdate, err)
	}
	if err := state.Update(term); err != nil {
		t.Fatal(err)
	}
	after, err := state.Cursor()
	if err != nil || after.Visible {
		t.Fatalf("updated cursor = %+v, %v", after, err)
	}
	if colors.Background.Uint32() != 0x445566 || cursor.X != 3 {
		t.Fatal("retained metadata changed")
	}
}

func TestConfigurationConstructors(t *testing.T) {
	zero := uint(0)
	black := RGB{}
	noBlink := false
	term, err := NewTerminalWithConfig(TerminalConfig{Cols: 10, Rows: 2, ScrollbackMaxBytes: &zero, ScrollbackMaxLines: &zero, BackgroundColor: &black, CursorBlink: &noBlink, ModeDefaults: []ModeDefault{{Mode: ModeCursorVisible, Enabled: false}}})
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	s, err := term.NewStreamWithConfig(StreamConfig{ContinuationMaxBytes: 128, UnknownMaxBytes: 64, Version: &VersionReport{Name: "testapp", Version: "1.0"}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	feed(t, s, "\x1b[>0q")
	var replies bytes.Buffer
	if err := s.WriteReplies(&replies); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(replies.String(), "testapp 1.0") {
		t.Fatalf("identity = %q", replies.String())
	}
	feed(t, s, "1\r\n2\r\n3\r\n4")
	screen, err := term.ActiveScreen()
	if err != nil {
		t.Fatal(err)
	}
	bar, err := screen.Scrollbar()
	if err != nil || bar.Total != bar.Len || bar.Offset != 0 {
		t.Fatalf("zero scrollback = %+v, %v", bar, err)
	}
	state, err := NewRenderState()
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	if err := state.Update(term); err != nil {
		t.Fatal(err)
	}
	cursor, err := state.Cursor()
	if err != nil || cursor.Visible || cursor.Blinking {
		t.Fatalf("explicit false = %+v, %v", cursor, err)
	}
	colors, err := state.Colors()
	if err != nil || colors.Background != (RGB{}) {
		t.Fatalf("explicit black = %+v, %v", colors, err)
	}
	for _, cfg := range []TerminalConfig{{}, {Cols: 1}, {Rows: 1}} {
		if got, err := NewTerminalWithConfig(cfg); got != nil || !errors.Is(err, ErrOutOfRange) {
			t.Fatalf("invalid dimensions = %v, %v", got, err)
		}
	}
	if got, err := (*Terminal)(nil).NewStreamWithConfig(StreamConfig{}); got != nil || !errors.Is(err, ErrInvalidHandle) {
		t.Fatalf("nil parent = %v, %v", got, err)
	}
}

func TestClipboardMIMEReply(t *testing.T) {
	_, s := newStreamPair(t, 10, 2)
	binary := []byte{0, 1, 255, 10}
	if err := s.OnClipboardReadRequest(func(req *ClipboardRequest) {
		if err := req.Reply([]ClipboardContent{{MIME: "text/plain", Data: []byte("hello")}, {MIME: "application/octet-stream", Data: binary}}, false); err != nil {
			t.Error(err)
		}
		// A second response to the same request must not emit another reply.
		if err := req.ReplyText("duplicate", false); err != nil {
			t.Error(err)
		}
	}); err != nil {
		t.Fatal(err)
	}
	mimes := base64.StdEncoding.EncodeToString([]byte("text/plain application/octet-stream"))
	feed(t, s, "\x1b]5522;type=read:id=multi;"+mimes+"\x1b\\")
	var output bytes.Buffer
	if err := s.WriteReplies(&output); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	for _, want := range []string{"status=OK", "status=DONE", "aGVsbG8=", base64.StdEncoding.EncodeToString(binary)} {
		if !strings.Contains(got, want) {
			t.Fatalf("reply missing %q: %q", want, got)
		}
	}
	if strings.Count(got, "status=DONE") != 1 {
		t.Fatalf("duplicate reply: %q", got)
	}
	if err := s.OnClipboardReadRequest(func(req *ClipboardRequest) {
		if err := req.AddContent("text/plain", []byte("not authorized")); err != nil {
			t.Error(err)
		}
	}); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	feed(t, s, "\x1b]5522;type=read:id=denied;"+mimes+"\x1b\\")
	if err := s.WriteReplies(&output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "status=EPERM") || strings.Contains(output.String(), "status=DATA") {
		t.Fatalf("staging authorized read: %q", output.String())
	}
}

func TestBuildInfo(t *testing.T) {
	info := GetBuildInfo()
	if len(info.GhosttyRevision) != 40 || info.ZigoVersion == "" || info.Optimize == "" {
		t.Fatalf("incomplete metadata: %+v", info)
	}
	if info != GetBuildInfo() {
		t.Fatal("build metadata is not stable")
	}
	data, err := os.ReadFile("build-info.json")
	if err != nil {
		t.Fatal(err)
	}
	var artifact struct {
		GhosttyRevision string `json:"ghostty_revision"`
		ZigoVersion     string `json:"zigo_version"`
		Optimize        string `json:"optimize"`
		SIMD            bool   `json:"simd"`
		KittyGraphics   bool   `json:"kitty_graphics"`
		TmuxControlMode bool   `json:"tmux_control_mode"`
	}
	if err := json.Unmarshal(data, &artifact); err != nil {
		t.Fatal(err)
	}
	if BuildInfo(artifact) != info {
		t.Fatalf("artifact %+v differs from Go build information %+v", artifact, info)
	}
}

func TestConfigurationFailureReleasesHandles(t *testing.T) {
	if term, err := NewTerminalWithConfig(TerminalConfig{Cols: 10, Rows: 2, ModeDefaults: []ModeDefault{{Mode: Mode(65535)}}}); term != nil || !errors.Is(err, ErrOutOfRange) {
		t.Fatalf("invalid mode = %v, %v", term, err)
	}
	term, err := NewTerminal(10, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	invalid := ColorScheme(255)
	if stream, err := term.NewStreamWithConfig(StreamConfig{ColorScheme: &invalid}); stream != nil || !errors.Is(err, ErrOutOfRange) {
		t.Fatalf("invalid scheme = %v, %v", stream, err)
	}
	if err := term.Close(); err != nil {
		t.Fatalf("failed constructor retained child: %v", err)
	}
}

func TestStreamConfigCallbackLifetime(t *testing.T) {
	before := zigoActiveCallbackHandleCount()
	term, err := NewTerminalWithConfig(TerminalConfig{Cols: 10, Rows: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	s, err := term.NewStreamWithConfig(StreamConfig{ClipboardRead: func(req *ClipboardRequest) {
		if err := req.Reply(nil, false); err != nil {
			t.Error(err)
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got := zigoActiveCallbackHandleCount(); got != before+1 {
		t.Fatalf("registered callbacks = %d, want %d", got, before+1)
	}
	feed(t, s, "\x1b]52;c;?\x07")
	var output bytes.Buffer
	if err := s.WriteReplies(&output); err != nil {
		t.Fatal(err)
	}
	if output.String() != "\x1b]52;c;\x07" {
		t.Fatalf("empty success reply = %q", output.String())
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if got := zigoActiveCallbackHandleCount(); got != before {
		t.Fatalf("leaked callbacks: %d, want %d", got, before)
	}
}

func TestStreamConfigOmittedCallbacksKeepDefaults(t *testing.T) {
	term, err := NewTerminal(10, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	before := zigoActiveCallbackHandleCount()
	stream, err := term.NewStreamWithConfig(StreamConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if got := zigoActiveCallbackHandleCount(); got != before {
		t.Fatalf("nil callbacks registered: %d, want %d", got, before)
	}
	feed(t, stream, "\x1b]52;c;?\x07\x1b[c")
	var output bytes.Buffer
	if err := stream.WriteReplies(&output); err != nil {
		t.Fatal(err)
	}
	// A denied OSC 52 read returns empty text; DA1 must not advertise clipboard support.
	if output.String() != "\x1b]52;c;\x07\x1b[?62;22c" {
		t.Fatalf("unconfigured clipboard changed protocol behavior: %q", output.String())
	}
}

func BenchmarkEventNotificationRead(b *testing.B) {
	term, err := NewTerminal(10, 2)
	if err != nil {
		b.Fatal(err)
	}
	defer term.Close()
	s, err := term.NewStream(0)
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close()
	data := []byte("\x1b]777;notify;title;body\x07")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := s.Feed(data); err != nil {
			b.Fatal(err)
		}
		event, ok, err := s.NextEventValue()
		if err != nil || !ok || event.Title != "title" || event.Body != "body" {
			b.Fatal(event, ok, err)
		}
	}
}

func TestNilClipboardRegistrationPreservesHandler(t *testing.T) {
	for _, read := range []bool{false, true} {
		name := "write"
		if read {
			name = "read"
		}
		t.Run(name, func(t *testing.T) {
			_, stream := newStreamPair(t, 10, 2)
			register := stream.OnClipboardWriteRequest
			sequence := "\x1b]52;c;aGVsbG8=\x07"
			if read {
				register = stream.OnClipboardReadRequest
				sequence = "\x1b]52;c;?\x07"
			}
			before := zigoActiveCallbackHandleCount()
			checkNil := func(wantHandles int64) {
				t.Helper()
				err := register(nil)
				var callbackErr *CallbackError
				if !errors.Is(err, ErrNilCallback) || !errors.As(err, &callbackErr) {
					t.Fatalf("nil registration = %T: %v", err, err)
				}
				if got := zigoActiveCallbackHandleCount(); got != wantHandles {
					t.Fatalf("callback handles = %d, want %d", got, wantHandles)
				}
			}
			checkNil(before)
			calls := 0
			if err := register(func(*ClipboardRequest) { calls++ }); err != nil {
				t.Fatal(err)
			}
			checkNil(before + 1)
			feed(t, stream, sequence)
			if calls != 1 {
				t.Fatalf("existing handler calls = %d", calls)
			}
			if err := register(func(*ClipboardRequest) { calls += 10 }); err != nil {
				t.Fatal(err)
			}
			feed(t, stream, sequence)
			if calls != 11 {
				t.Fatalf("replacement handler calls = %d", calls)
			}
			if err := stream.Close(); err != nil {
				t.Fatal(err)
			}
			if got := zigoActiveCallbackHandleCount(); got != before {
				t.Fatalf("leaked handles: %d", got-before)
			}
		})
	}
}

func TestFunctionalOptionsAndConvenience(t *testing.T) {
	// The background color is not one ghostty's Options can carry across the
	// ABI, so it comes from TerminalConfig rather than a constructor option.
	bg := RGBFromUint32(0x123456)
	term, err := NewTerminalWithConfig(
		TerminalConfig{Cols: 80, Rows: 24, BackgroundColor: &bg},
		WithScrollbackMaxLines(500),
		WithDefaultCursorBlink(true),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()

	if term.Cols() != 80 || term.Rows() != 24 {
		t.Fatalf("unexpected dimensions: %dx%d", term.Cols(), term.Rows())
	}

	stream, err := term.NewStreamWithOptions(
		WithContinuationMaxBytes(128),
		WithVersionReport("testapp", "1.0"),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()

	// Test Stream.WriteString (io.StringWriter)
	n, err := stream.WriteString("Hello, world!\r\n")
	if err != nil {
		t.Fatal(err)
	}
	if n != len("Hello, world!\r\n") {
		t.Fatalf("WriteString wrote %d bytes, want %d", n, len("Hello, world!\r\n"))
	}

	// Test FormatString and PlainText on Terminal
	plain, err := term.PlainText()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plain, "Hello, world!") {
		t.Fatalf("PlainText = %q, want containing %q", plain, "Hello, world!")
	}

	// Test FormatString on Screen
	screen, err := term.ActiveScreen()
	if err != nil {
		t.Fatal(err)
	}
	screenPlain, err := screen.PlainText()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(screenPlain, "Hello, world!") {
		t.Fatalf("screen PlainText = %q, want containing %q", screenPlain, "Hello, world!")
	}
}

func TestSession(t *testing.T) {
	sess, err := New(80, 24,
		WithTerminalOptions(WithScrollbackMaxLines(100)),
		WithStreamOptions(WithVersionReport("sessapp", "2.0")),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	// Test io.Writer via fmt.Fprintf
	fmt.Fprintf(sess, "fmt write test: %d\r\n", 42)
	written, err := sess.WriteString("line 1\r\n")
	if err != nil || written != len("line 1\r\n") {
		t.Fatalf("WriteString = %d, %v", written, err)
	}

	// Feed VT data (bold, colored)
	_, _ = sess.Write([]byte("\033[1;32mBold Green\033[0m\r\n"))

	// Test PlainText
	text, err := sess.PlainText()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "fmt write test: 42") || !strings.Contains(text, "line 1") || !strings.Contains(text, "Bold Green") {
		t.Fatalf("Session PlainText = %q", text)
	}

	// Test FormatString (HTML)
	html, err := sess.FormatString(FormatOptions{Format: FormatterFormatHtml})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, "Bold Green") {
		t.Fatalf("Session HTML = %q", html)
	}

	// Test MustNew
	mustSess := MustNew(40, 10)
	defer mustSess.Close()
	if mustSess.Terminal().Cols() != 40 || mustSess.Terminal().Rows() != 10 {
		t.Fatalf("unexpected MustNew dimensions")
	}
}

func TestSessionStreamDoesNotAllocate(t *testing.T) {
	sess := MustNew(10, 2)
	defer sess.Close()
	want := sess.Stream()
	if got := testing.AllocsPerRun(1000, func() {
		if sess.Stream() != want {
			t.Fatal("Stream returned a different primary stream")
		}
	}); got != 0 {
		t.Fatalf("Stream allocated %v times per call", got)
	}
}

// The session's lifecycle is generated, so what is tested here is the contract
// the generator promises and the hand-written adapters lean on: more than one
// stream per session, Close in reverse adoption order, Close being idempotent,
// and adoption after Close closing rather than leaking.
// The constructor options and Stream.WriteString are both generated now, so
// what is checked here is that the generated surface carries the contract the
// hand-written code used to: ghostty's own defaults when an option is omitted,
// the option winning when it is given, and both io interfaces on one stream.
func TestGeneratedTerminalOptions(t *testing.T) {
	// Omitting every option leaves ghostty's defaults, which for the cursor
	// style is block.
	plain, err := NewTerminal(10, 3)
	if err != nil {
		t.Fatal(err)
	}
	defer plain.Close()
	if got := plain.CursorStyle(); got != CursorStyleBlock {
		t.Fatalf("default CursorStyle = %v, want %v", got, CursorStyleBlock)
	}

	term, err := NewTerminal(10, 3,
		WithTerminalDefaultCursorStyle(CursorStyleBar),
		WithScrollbackMaxLines(7),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	if got := term.CursorStyle(); got != CursorStyleBar {
		t.Fatalf("CursorStyle = %v, want %v", got, CursorStyleBar)
	}

	// The value-taking wrapper and the generated pointer form are the same
	// option, so they have to agree.
	lines := uint(7)
	if _, err := NewTerminal(10, 3, WithTerminalMaxScrollbackLines(&lines)); err != nil {
		t.Fatal(err)
	}

	// MustNewTerminal is the panicking form of the generated constructor.
	must := MustNewTerminal(4, 2)
	defer must.Close()
	if must.Cols() != 4 || must.Rows() != 2 {
		t.Fatalf("MustNewTerminal dimensions = %dx%d", must.Cols(), must.Rows())
	}
}

func TestStreamImplementsBothWriters(t *testing.T) {
	sess, err := New(40, 5)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	stream := sess.Stream()

	var _ io.Writer = stream
	var _ io.StringWriter = stream

	// io.WriteString picks WriteString over Write when the target has it.
	n, err := io.WriteString(stream, "abc\r\n")
	if err != nil || n != 5 {
		t.Fatalf("io.WriteString = %d, %v", n, err)
	}
	if _, err := stream.Write([]byte("def\r\n")); err != nil {
		t.Fatal(err)
	}
	text, err := sess.PlainText()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "abc") || !strings.Contains(text, "def") {
		t.Fatalf("PlainText = %q, want both writes", text)
	}
}

func TestSessionAdoptsManyStreams(t *testing.T) {
	term, err := NewTerminal(20, 5)
	if err != nil {
		t.Fatal(err)
	}
	sess := NewSession(term)
	first, err := term.NewStream(0)
	if err != nil {
		t.Fatal(err)
	}
	second, err := term.NewStream(0)
	if err != nil {
		t.Fatal(err)
	}
	sess.AddStream(first).AddStream(nil, second)

	if got := sess.Streams(); len(got) != 2 || got[0] != first || got[1] != second {
		t.Fatalf("Streams() = %v, want [first second]", got)
	}
	if sess.Stream() != first {
		t.Fatalf("Stream() is not the first stream adopted")
	}
	if sess.Terminal() != term {
		t.Fatalf("Terminal() is not the adopted terminal")
	}

	// A terminal refuses to close while a stream is open, so a Close that
	// succeeds is itself the evidence the order was streams-then-terminal.
	if err := sess.Close(); err != nil {
		t.Fatalf("Close = %v", err)
	}
	if err := sess.Close(); err != nil {
		t.Fatalf("second Close = %v, want the first result", err)
	}

	// Nothing survives the session, and the adapters report a closed stream
	// rather than panicking on the empty slice.
	if _, err := sess.Write([]byte("x")); !errors.Is(err, ErrInvalidHandle) {
		t.Fatalf("Write after Close = %v, want ErrInvalidHandle", err)
	}
	if _, err := sess.WriteString("x"); !errors.Is(err, ErrInvalidHandle) {
		t.Fatalf("WriteString after Close = %v, want ErrInvalidHandle", err)
	}
}

// All four child kinds go into one session, so Close is the single place the
// ordering has to be right. A terminal refuses to close while any child is
// open, so a Close that succeeds is the evidence.
func TestSessionAdoptsEveryChildKind(t *testing.T) {
	term, err := NewTerminal(20, 5)
	if err != nil {
		t.Fatal(err)
	}
	sess := NewSession(term)

	stream, err := term.NewStream(0)
	if err != nil {
		t.Fatal(err)
	}
	search, err := term.NewSearch("needle")
	if err != nil {
		t.Fatal(err)
	}
	gesture, err := term.NewGesture()
	if err != nil {
		t.Fatal(err)
	}
	ref, err := term.NewGridRef(PointTagViewport, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	sess.AddStream(stream).AddSearch(search).AddGesture(gesture).AddGridRef(ref)

	if len(sess.Streams()) != 1 || len(sess.Searches()) != 1 ||
		len(sess.Gestures()) != 1 || len(sess.GridRefs()) != 1 {
		t.Fatalf("not every child was adopted")
	}
	if err := sess.Close(); err != nil {
		t.Fatalf("Close = %v, want the terminal to close after all four children", err)
	}
	if _, err := stream.Write([]byte("x")); !errors.Is(err, ErrInvalidHandle) {
		t.Fatalf("stream survived Close: %v", err)
	}
}

func TestSessionAdoptionAfterCloseDoesNotLeak(t *testing.T) {
	term, err := NewTerminal(20, 5)
	if err != nil {
		t.Fatal(err)
	}
	// A second terminal outlives the session so the late stream has a parent
	// that is still open; the session is the only thing that can close it.
	other, err := NewTerminal(20, 5)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	late, err := other.NewStream(0)
	if err != nil {
		t.Fatal(err)
	}

	sess := NewSession(term)
	if err := sess.Close(); err != nil {
		t.Fatal(err)
	}
	sess.AddStream(late)
	if got := sess.Streams(); len(got) != 0 {
		t.Fatalf("Streams() after Close = %v, want empty", got)
	}
	// `other` closes cleanly in the deferred call only if `late` is already
	// closed; an open child makes Terminal.Close fail with HandleInUseError.
	if _, err := late.Write([]byte("x")); !errors.Is(err, ErrInvalidHandle) {
		t.Fatalf("late stream Write = %v, want ErrInvalidHandle", err)
	}
}
