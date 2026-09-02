package gostty

import (
	"strings"
	"testing"
)

func attrs(t *testing.T, term *Terminal) string {
	t.Helper()
	buf := make([]byte, 64)
	n, err := term.PrintAttributesInto(buf)
	if err != nil {
		t.Fatalf("PrintAttributesInto: %v", err)
	}
	return string(buf[:n])
}

// SetAttribute changes the cursor's pen, which PrintAttributesInto reports
// back as a DECRPSS body.
func TestSetAttributeReadback(t *testing.T) {
	term := newTerm(t, 20, 3)

	if got, want := attrs(t, term), "0"; got != want {
		t.Fatalf("default attributes = %q, want %q", got, want)
	}

	if err := term.SetAttribute(AttributeBold()); err != nil {
		t.Fatalf("SetAttribute(bold): %v", err)
	}
	if got := attrs(t, term); !strings.Contains(got, "1") {
		t.Errorf("after bold, attributes = %q, want it to report 1", got)
	}

	if err := term.SetAttribute(AttributeItalic()); err != nil {
		t.Fatalf("SetAttribute(italic): %v", err)
	}
	if got := attrs(t, term); !strings.Contains(got, "3") {
		t.Errorf("after italic, attributes = %q, want it to report 3", got)
	}

	if err := term.SetAttribute(AttributeResetBold()); err != nil {
		t.Fatalf("SetAttribute(reset_bold): %v", err)
	}
	got := attrs(t, term)
	if strings.Contains(got, "1") {
		t.Errorf("after reset_bold, attributes = %q, want no 1", got)
	}
	if !strings.Contains(got, "3") {
		t.Errorf("after reset_bold, attributes = %q, want italic to survive", got)
	}

	if err := term.SetAttribute(AttributeUnset()); err != nil {
		t.Fatalf("SetAttribute(unset): %v", err)
	}
	if got, want := attrs(t, term), "0"; got != want {
		t.Errorf("after unset, attributes = %q, want %q", got, want)
	}
}

// The union carries its payload across the boundary; Tag reads it back.
func TestAttributeTag(t *testing.T) {
	for _, tc := range []struct {
		attr Attribute
		want AttributeTag
	}{
		{AttributeBold(), AttributeTagBold},
		{AttributeDirectColorFg(0xFF8000), AttributeTagDirectColorFg},
		{AttributeUnderline(UnderlineCurly), AttributeTagUnderline},
		{AttributeNamedBg(ColorNameRed), AttributeTagNamedBg},
	} {
		if got := tc.attr.Tag(); got != tc.want {
			t.Errorf("Tag() = %v, want %v", got, tc.want)
		}
	}
}

// A direct color set through the union reaches the cell the same way the
// equivalent escape sequence does.
func TestSetAttributeDirectColor(t *testing.T) {
	viaAPI := newTerm(t, 20, 3)
	if err := viaAPI.SetAttribute(AttributeDirectColorFg(0xFF0000)); err != nil {
		t.Fatalf("SetAttribute: %v", err)
	}
	fromAPI := attrs(t, viaAPI)

	viaStream, stream := newStreamPair(t, 20, 3)
	feed(t, stream, "\x1b[38;2;255;0;0m")
	fromStream := attrs(t, viaStream)

	if fromAPI != fromStream {
		t.Errorf("SetAttribute gave %q, the equivalent SGR gave %q", fromAPI, fromStream)
	}
}

func TestSetAttributeUnderlineStyle(t *testing.T) {
	term := newTerm(t, 20, 3)
	if err := term.SetAttribute(AttributeUnderline(UnderlineDouble)); err != nil {
		t.Fatalf("SetAttribute: %v", err)
	}
	// ghostty reports underline styles in the colon sub-parameter form.
	if got := attrs(t, term); !strings.Contains(got, "4:2") {
		t.Errorf("attributes = %q, want double underline (4:2)", got)
	}
}

// The OSC parser itself is not bound, but the state it feeds is: title and pwd
// arrive through the stream and are read off the terminal.
func TestOSCReachesTerminalState(t *testing.T) {
	term, stream := newStreamPair(t, 40, 4)
	feed(t, stream, "\x1b]0;my title\x07")
	feed(t, stream, "\x1b]7;file:///tmp/work\x1b\\")

	title, ok, err := term.GetTitle()
	if err != nil || !ok || string(title) != "my title" {
		t.Errorf("GetTitle() = %q, %v, %v; want \"my title\", true, nil", title, ok, err)
	}
	pwd, ok, err := term.GetPwd()
	if err != nil || !ok || string(pwd) != "file:///tmp/work" {
		t.Errorf("GetPwd() = %q, %v, %v; want the reported URL, true, nil", pwd, ok, err)
	}
}
