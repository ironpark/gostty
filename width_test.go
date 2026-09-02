package gostty

import (
	"errors"
	"testing"
)

func TestCodepointWidth(t *testing.T) {
	for _, tc := range []struct {
		cp   uint32
		want uint8
	}{
		{'A', 1},
		{0x00, 0},
		{0xAC00, 2},   // 가
		{0x1F600, 2},  // 😀
		{0x110000, 1}, // above Unicode but inside u21: ghostty's own answer
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

// The Zig parameter is u21, which zigo promotes to uint32 at the C boundary.
// Arguments past maxInt(u21) are rejected in Go before the native call.
func TestCodepointWidthAboveU21(t *testing.T) {
	got, err := CodepointWidth(0x200000)
	if !errors.Is(err, ErrOutOfRange) {
		t.Fatalf("CodepointWidth(0x200000) = %d, %v; want ErrOutOfRange", got, err)
	}
	var rangeErr *RangeError
	if !errors.As(err, &rangeErr) {
		t.Fatalf("error is not *RangeError: %v", err)
	}
	if rangeErr.Parameter != "p0" || rangeErr.Type != "u21" {
		t.Errorf("RangeError = %+v; want Parameter p0, Type u21", rangeErr)
	}
}
