package gostty

import "testing"

func newTestOSCParser(t *testing.T) *OSCParser {
	t.Helper()
	p, err := NewOSCParser()
	if err != nil {
		t.Fatalf("NewOSCParser: %v", err)
	}
	t.Cleanup(func() { p.Close() })
	return p
}

// parseOSC feeds one complete OSC payload -- the bytes between ESC ] and the
// terminator -- and reports what the parser made of it.
func parseOSC(t *testing.T, p *OSCParser, payload string) OSCCommand {
	t.Helper()
	if err := p.Reset(); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if err := p.Feed([]byte(payload)); err != nil {
		t.Fatalf("Feed(%q): %v", payload, err)
	}
	ok, err := p.End(OSCTerminatorSt)
	if err != nil {
		t.Fatalf("End(%q): %v", payload, err)
	}
	if !ok {
		t.Fatalf("End(%q) reported no command", payload)
	}
	kind, err := p.Command()
	if err != nil {
		t.Fatalf("Command(%q): %v", payload, err)
	}
	return kind
}

func TestOSCParserWindowTitle(t *testing.T) {
	p := newTestOSCParser(t)
	if kind := parseOSC(t, p, "0;hello world"); kind != OSCCommandChangeWindowTitle {
		t.Fatalf("kind = %v, want ChangeWindowTitle", kind)
	}
	title, err := p.WindowTitle()
	if err != nil {
		t.Fatalf("WindowTitle: %v", err)
	}
	if title != "hello world" {
		t.Fatalf("WindowTitle = %q, want %q", title, "hello world")
	}
	// Every accessor answers for its own command only.
	if icon, _ := p.Icon(); icon != "" {
		t.Fatalf("Icon = %q, want empty for a title sequence", icon)
	}
}

func TestOSCParserIcon(t *testing.T) {
	p := newTestOSCParser(t)
	if kind := parseOSC(t, p, "1;icon-name"); kind != OSCCommandChangeWindowIcon {
		t.Fatalf("kind = %v, want ChangeWindowIcon", kind)
	}
	if icon, _ := p.Icon(); icon != "icon-name" {
		t.Fatalf("Icon = %q, want %q", icon, "icon-name")
	}
}

func TestOSCParserPwd(t *testing.T) {
	p := newTestOSCParser(t)
	const url = "file://host/home/user"
	if kind := parseOSC(t, p, "7;"+url); kind != OSCCommandReportPwd {
		t.Fatalf("kind = %v, want ReportPwd", kind)
	}
	pwd, err := p.Pwd()
	if err != nil {
		t.Fatalf("Pwd: %v", err)
	}
	if pwd != url {
		t.Fatalf("Pwd = %q, want %q", pwd, url)
	}
}

func TestOSCParserHyperlink(t *testing.T) {
	p := newTestOSCParser(t)
	if kind := parseOSC(t, p, "8;;https://example.com/"); kind != OSCCommandHyperlinkStart {
		t.Fatalf("kind = %v, want HyperlinkStart", kind)
	}
	if uri, _ := p.HyperlinkUri(); uri != "https://example.com/" {
		t.Fatalf("HyperlinkUri = %q, want the URI", uri)
	}
	if id, _ := p.HyperlinkID(); id != "" {
		t.Fatalf("HyperlinkID = %q, want empty for a link with no id", id)
	}

	if kind := parseOSC(t, p, "8;id=42;https://example.com/two"); kind != OSCCommandHyperlinkStart {
		t.Fatalf("kind = %v, want HyperlinkStart", kind)
	}
	if uri, _ := p.HyperlinkUri(); uri != "https://example.com/two" {
		t.Fatalf("HyperlinkUri = %q, want the URI", uri)
	}
	if id, _ := p.HyperlinkID(); id != "42" {
		t.Fatalf("HyperlinkID = %q, want %q", id, "42")
	}

	// An empty URI ends the link rather than starting one.
	if kind := parseOSC(t, p, "8;;"); kind != OSCCommandHyperlinkEnd {
		t.Fatalf("kind = %v, want HyperlinkEnd", kind)
	}
}

func TestOSCParserProgressReport(t *testing.T) {
	p := newTestOSCParser(t)
	if kind := parseOSC(t, p, "9;4;1;50"); kind != OSCCommandConemuProgressReport {
		t.Fatalf("kind = %v, want ConemuProgressReport", kind)
	}
	state, err := p.ProgressState()
	if err != nil {
		t.Fatalf("ProgressState: %v", err)
	}
	if state != ProgressStateSet {
		t.Fatalf("ProgressState = %v, want Set", state)
	}
	value, err := p.ProgressValue()
	if err != nil {
		t.Fatalf("ProgressValue: %v", err)
	}
	if value != 50 {
		t.Fatalf("ProgressValue = %d, want 50", value)
	}

	// A report with no percentage is -1 rather than zero, which is a real
	// progress value.
	if kind := parseOSC(t, p, "9;4;3"); kind != OSCCommandConemuProgressReport {
		t.Fatalf("kind = %v, want ConemuProgressReport", kind)
	}
	if value, _ := p.ProgressValue(); value != -1 {
		t.Fatalf("ProgressValue = %d, want -1", value)
	}
}

func TestOSCParserDesktopNotification(t *testing.T) {
	p := newTestOSCParser(t)
	if kind := parseOSC(t, p, "777;notify;build;done"); kind != OSCCommandShowDesktopNotification {
		t.Fatalf("kind = %v, want ShowDesktopNotification", kind)
	}
	if title, _ := p.NotificationTitle(); title != "build" {
		t.Fatalf("NotificationTitle = %q, want %q", title, "build")
	}
	if body, _ := p.NotificationBody(); body != "done" {
		t.Fatalf("NotificationBody = %q, want %q", body, "done")
	}
}

func TestOSCParserClipboard(t *testing.T) {
	p := newTestOSCParser(t)
	if kind := parseOSC(t, p, "52;c;aGVsbG8="); kind != OSCCommandClipboardContents {
		t.Fatalf("kind = %v, want ClipboardContents", kind)
	}
	sel, err := p.ClipboardSelection()
	if err != nil {
		t.Fatalf("ClipboardSelection: %v", err)
	}
	if sel != 'c' {
		t.Fatalf("ClipboardSelection = %q, want 'c'", rune(sel))
	}
	if data, _ := p.ClipboardData(); data != "aGVsbG8=" {
		t.Fatalf("ClipboardData = %q, want the base64 payload", data)
	}
}

func TestOSCParserSemanticPrompt(t *testing.T) {
	p := newTestOSCParser(t)
	if kind := parseOSC(t, p, "133;A"); kind != OSCCommandSemanticPrompt {
		t.Fatalf("kind = %v, want SemanticPrompt", kind)
	}
	action, err := p.SemanticPromptAction()
	if err != nil {
		t.Fatalf("SemanticPromptAction: %v", err)
	}
	if action != SemanticPromptActionFreshLineNewPrompt {
		t.Fatalf("SemanticPromptAction = %v, want FreshLineNewPrompt", action)
	}
}

func TestOSCParserMouseShape(t *testing.T) {
	p := newTestOSCParser(t)
	if kind := parseOSC(t, p, "22;pointer"); kind != OSCCommandMouseShape {
		t.Fatalf("kind = %v, want MouseShape", kind)
	}
	if shape, _ := p.MouseShape(); shape != "pointer" {
		t.Fatalf("MouseShape = %q, want %q", shape, "pointer")
	}
}

// A sequence can arrive split across reads, and the parser has to stitch it
// back together.
func TestOSCParserSplitFeed(t *testing.T) {
	p := newTestOSCParser(t)
	if err := p.Reset(); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	for _, chunk := range []string{"0;spl", "it ti", "tle"} {
		if err := p.Feed([]byte(chunk)); err != nil {
			t.Fatalf("Feed(%q): %v", chunk, err)
		}
	}
	if ok, err := p.End(OSCTerminatorBel); err != nil || !ok {
		t.Fatalf("End = %v, %v; want true, nil", ok, err)
	}
	if title, _ := p.WindowTitle(); title != "split title" {
		t.Fatalf("WindowTitle = %q, want %q", title, "split title")
	}
}

// Anything the parser does not recognise leaves End reporting no command
// rather than handing back a stale one.
func TestOSCParserGarbage(t *testing.T) {
	p := newTestOSCParser(t)
	if kind := parseOSC(t, p, "0;stale"); kind != OSCCommandChangeWindowTitle {
		t.Fatalf("kind = %v, want ChangeWindowTitle", kind)
	}
	if err := p.Reset(); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if err := p.Feed([]byte("not-an-osc")); err != nil {
		t.Fatalf("Feed: %v", err)
	}
	ok, err := p.End(OSCTerminatorSt)
	if err != nil {
		t.Fatalf("End: %v", err)
	}
	if ok {
		t.Fatal("End reported a command for garbage input")
	}
	if kind, _ := p.Command(); kind != OSCCommandInvalid {
		t.Fatalf("Command = %v, want Invalid", kind)
	}
	if title, _ := p.WindowTitle(); title != "" {
		t.Fatalf("WindowTitle = %q, want empty", title)
	}
}

func parseSGR(t *testing.T, params []uint16, colonMask uint32) []SgrAttribute {
	t.Helper()
	n, err := SgrAttributeCount(params, colonMask)
	if err != nil {
		t.Fatalf("SgrAttributeCount(%v): %v", params, err)
	}
	out := make([]SgrAttribute, n)
	written, err := SgrAttributes(params, colonMask, out)
	if err != nil {
		t.Fatalf("SgrAttributes(%v): %v", params, err)
	}
	if written != n {
		t.Fatalf("SgrAttributes wrote %d attributes, want the %d SgrAttributeCount promised", written, n)
	}
	return out
}

func expectSGR(t *testing.T, got []SgrAttribute, want ...SgrAttribute) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("parsed %d attributes (%v), want %d (%v)", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("attribute %d = %v, want %v", i, got[i], want[i])
		}
	}
}

// An empty parameter list is the SGR reset.
func TestSGREmptyIsUnset(t *testing.T) {
	expectSGR(t, parseSGR(t, nil, 0), SgrAttribute{Tag: SgrAttributeTagUnset})
}

func TestSGRSimpleAttributes(t *testing.T) {
	// CSI 1;3;4;7 m
	expectSGR(t, parseSGR(t, []uint16{1, 3, 4, 7}, 0),
		SgrAttribute{Tag: SgrAttributeTagBold},
		SgrAttribute{Tag: SgrAttributeTagItalic},
		SgrAttribute{Tag: SgrAttributeTagUnderline, Value: uint32(UnderlineSingle)},
		SgrAttribute{Tag: SgrAttributeTagInverse},
	)
}

func TestSGRNamedAndPaletteColors(t *testing.T) {
	// CSI 31;42;91;5 m -- the last is blink, not a colour.
	expectSGR(t, parseSGR(t, []uint16{31, 42, 91, 5}, 0),
		SgrAttribute{Tag: SgrAttributeTagNamedFg, Value: uint32(ColorNameRed)},
		SgrAttribute{Tag: SgrAttributeTagNamedBg, Value: uint32(ColorNameGreen)},
		SgrAttribute{Tag: SgrAttributeTagBrightNamedFg, Value: uint32(ColorNameBrightRed)},
		SgrAttribute{Tag: SgrAttributeTagBlink},
	)

	// CSI 38;5;200 m and CSI 48;5;17 m
	expectSGR(t, parseSGR(t, []uint16{38, 5, 200}, 0),
		SgrAttribute{Tag: SgrAttributeTagColor256Fg, Value: 200})
	expectSGR(t, parseSGR(t, []uint16{48, 5, 17}, 0),
		SgrAttribute{Tag: SgrAttributeTagColor256Bg, Value: 17})
}

// 4;3 is two attributes; 4:3 is one curly underline. The colon mask is what
// tells them apart.
func TestSGRColonUnderlineStyle(t *testing.T) {
	expectSGR(t, parseSGR(t, []uint16{4, 3}, 0),
		SgrAttribute{Tag: SgrAttributeTagUnderline, Value: uint32(UnderlineSingle)},
		SgrAttribute{Tag: SgrAttributeTagItalic},
	)

	// Parameter 0 is followed by a colon.
	expectSGR(t, parseSGR(t, []uint16{4, 3}, 1<<0),
		SgrAttribute{Tag: SgrAttributeTagUnderline, Value: uint32(UnderlineCurly)})
	expectSGR(t, parseSGR(t, []uint16{4, 0}, 1<<0),
		SgrAttribute{Tag: SgrAttributeTagUnderline, Value: uint32(UnderlineNone)})
	expectSGR(t, parseSGR(t, []uint16{4, 5}, 1<<0),
		SgrAttribute{Tag: SgrAttributeTagUnderline, Value: uint32(UnderlineDashed)})
}

func TestSGRTruecolor(t *testing.T) {
	const orange = uint32(0xFF8800)

	// Semicolon form: CSI 38;2;255;136;0 m
	expectSGR(t, parseSGR(t, []uint16{38, 2, 255, 136, 0}, 0),
		SgrAttribute{Tag: SgrAttributeTagDirectColorFg, Value: orange})
	expectSGR(t, parseSGR(t, []uint16{48, 2, 255, 136, 0}, 0),
		SgrAttribute{Tag: SgrAttributeTagDirectColorBg, Value: orange})

	// Colon form with the colour-space id: CSI 38:2::255:136:0 m. Every
	// parameter but the last is followed by a colon.
	expectSGR(t, parseSGR(t, []uint16{38, 2, 0, 255, 136, 0}, 0b011111),
		SgrAttribute{Tag: SgrAttributeTagDirectColorFg, Value: orange})

	// Underline colour: CSI 58:2::255:136:0 m
	expectSGR(t, parseSGR(t, []uint16{58, 2, 0, 255, 136, 0}, 0b011111),
		SgrAttribute{Tag: SgrAttributeTagUnderlineColorRgb, Value: orange})
}

func TestSGRResets(t *testing.T) {
	expectSGR(t, parseSGR(t, []uint16{0, 22, 39, 49}, 0),
		SgrAttribute{Tag: SgrAttributeTagUnset},
		SgrAttribute{Tag: SgrAttributeTagResetBold},
		SgrAttribute{Tag: SgrAttributeTagResetFg},
		SgrAttribute{Tag: SgrAttributeTagResetBg},
	)
}

// A code ghostty does not implement is reported rather than dropped, so the
// attributes still line up with the sequence as written.
func TestSGRUnknown(t *testing.T) {
	expectSGR(t, parseSGR(t, []uint16{1, 1234, 3}, 0),
		SgrAttribute{Tag: SgrAttributeTagBold},
		SgrAttribute{Tag: SgrAttributeTagUnknown},
		SgrAttribute{Tag: SgrAttributeTagItalic},
	)
}

// The tags are Attribute's own, in Attribute's order, so the constructor named
// after a tag rebuilds the attribute the parser saw.
func TestSGRRebuildsAttribute(t *testing.T) {
	got := parseSGR(t, []uint16{38, 5, 200}, 0)
	attr := AttributeColor256Fg(uint8(got[0].Value))
	if uint8(attr.Tag()) != uint8(got[0].Tag) {
		t.Fatalf("Attribute tag = %v, SGR tag = %v", attr.Tag(), got[0].Tag)
	}

	term := newTerm(t, 10, 2)
	if err := term.SetAttribute(attr); err != nil {
		t.Fatalf("SetAttribute: %v", err)
	}
}

// ghostty's CSI parser records at most 24 parameters, and the colon bitset is
// exactly that wide, so a longer list is refused rather than truncated.
func TestSGRTooManyParams(t *testing.T) {
	if _, err := SgrAttributeCount(make([]uint16, 25), 0); err == nil {
		t.Fatal("SgrAttributeCount accepted 25 parameters")
	}
	if _, err := SgrAttributeCount(make([]uint16, 24), 0); err != nil {
		t.Fatalf("SgrAttributeCount(24 params): %v", err)
	}
}

// A destination shorter than the sequence is an error rather than a silent
// truncation, so a caller cannot mistake a partial list for the whole one.
func TestSGRDestinationTooShort(t *testing.T) {
	if _, err := SgrAttributes([]uint16{1, 3, 4}, 0, make([]SgrAttribute, 2)); err == nil {
		t.Fatal("SgrAttributes accepted a destination shorter than the parameter list")
	}
}
