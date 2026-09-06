package gostty

import (
	"errors"
	"testing"
)

func TestCodepointWidth(t *testing.T) {
	for _, tc := range []struct {
		cp   rune
		want uint8
	}{
		{'A', 1},
		{0x00, 0},
		{0xAC00, 2},  // 가
		{0x1F600, 2}, // 😀
	} {
		got, err := CodepointWidth(tc.cp)
		if err != nil {
			t.Errorf("CodepointWidth(%#x): %v", tc.cp, err)
			continue
		}
		if got != tc.want {
			t.Errorf("CodepointWidth(%#x) = %d, want %d", tc.cp, got, tc.want)
		}
	}
}

// The parameter is a rune; values past the Unicode range are rejected in Go
// before the native call.
func TestCodepointWidthAboveUnicode(t *testing.T) {
	got, err := CodepointWidth(0x110000)
	if !errors.Is(err, ErrOutOfRange) {
		t.Fatalf("CodepointWidth(0x110000) = %d, %v; want ErrOutOfRange", got, err)
	}
	var rangeErr *RangeError
	if !errors.As(err, &rangeErr) {
		t.Fatalf("error is not *RangeError: %v", err)
	}
	if rangeErr.Parameter != "cp" || rangeErr.Type != "codepoint" {
		t.Errorf("RangeError = %+v; want Parameter cp, Type codepoint", rangeErr)
	}
}
