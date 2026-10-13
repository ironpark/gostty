package gostty

import (
	"errors"
	"testing"
)

func TestPanicsOnLiveHandle(t *testing.T) {
	term := newTerm(t, 10, 3)

	_ = term.CursorIsAtPrompt()
	term.CarriageReturn()
	term.Backspace()
	term.CursorDown(1)
	term.FullReset()
	_ = term.Cols()
	_ = term.Rows()
	_ = term.CursorX()
	_ = term.CursorY()
}

func TestPanicsWithTypedErrorOnClosedHandle(t *testing.T) {
	term, err := NewTerminal(10, 3)
	if err != nil {
		t.Fatalf("NewTerminal: %v", err)
	}
	if err := term.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatal("CursorIsAtPrompt on a closed terminal did not panic")
		}
		panicked, ok := recovered.(error)
		if !ok {
			t.Fatalf("panic value is %T, want error", recovered)
		}
		var handleErr *HandleError
		if !errors.As(panicked, &handleErr) {
			t.Errorf("panic value is %v, want *HandleError", panicked)
		}
	}()
	term.CursorIsAtPrompt()
}
