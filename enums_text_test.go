package gostty

import (
	"errors"
	"testing"
)

// The enums a consumer names in text round-trip through the generated
// parsers, and unknown text is reported as *EnumParseError.
func TestEnumTextRoundTrip(t *testing.T) {
	for _, style := range []CursorStyle{CursorStyleBar, CursorStyleBlock, CursorStyleUnderline} {
		text, err := style.MarshalText()
		if err != nil {
			t.Fatalf("MarshalText(%v): %v", style, err)
		}
		got, err := ParseCursorStyle(string(text))
		if err != nil || got != style {
			t.Errorf("ParseCursorStyle(%q) = %v, %v; want %v", text, got, err, style)
		}
	}
	if _, err := ParseCursorStyle("wedge"); err == nil {
		t.Error("ParseCursorStyle(\"wedge\") returned no error")
	} else {
		var parseErr *EnumParseError
		if !errors.As(err, &parseErr) {
			t.Errorf("ParseCursorStyle error is %T, want *EnumParseError", err)
		}
	}

	// Open enums also parse the `<Enum>(N)` spelling String() produces.
	unknown := EraseLine(7)
	got, err := ParseEraseLine(unknown.String())
	if err != nil || got != unknown {
		t.Errorf("ParseEraseLine(%q) = %v, %v; want %v", unknown.String(), got, err, unknown)
	}
}

// Events ranges over NextEvent until the queue is empty.
func TestStreamEvents(t *testing.T) {
	_, s := newStreamPair(t, 20, 3)
	if err := s.Feed([]byte("\x07\x1b]0;hi\x07")); err != nil {
		t.Fatal(err)
	}
	var got []StreamEvent
	for event, err := range s.Events() {
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, event)
	}
	if len(got) != 2 || got[0] != StreamEventBell {
		t.Errorf("Events() = %v, want bell then a title event", got)
	}
	for range s.Events() {
		t.Error("Events() yielded after the queue was drained")
	}
}
