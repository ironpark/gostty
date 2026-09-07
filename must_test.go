package gostty

import (
	"errors"
	"testing"
)

// A Must variant is the checked call with the error branch removed, so on a
// live handle the two agree on every result.
func TestMustVariantsMatchTheCheckedCall(t *testing.T) {
	term := newTerm(t, 10, 3)

	checked, err := term.CursorIsAtPrompt()
	if err != nil {
		t.Fatalf("CursorIsAtPrompt: %v", err)
	}
	if got := term.MustCursorIsAtPrompt(); got != checked {
		t.Errorf("MustCursorIsAtPrompt() = %v, want %v", got, checked)
	}

	// The void ones have no result to compare; that they return at all is the
	// claim, since the checked call would have handed back an error instead.
	term.MustCarriageReturn()
	term.MustBackspace()
	term.MustCursorDown(1)
	term.MustFullReset()

	screen, err := term.ActiveScreen()
	if err != nil {
		t.Fatalf("ActiveScreen: %v", err)
	}
	bottom, err := screen.ViewportIsBottom()
	if err != nil {
		t.Fatalf("ViewportIsBottom: %v", err)
	}
	if got := screen.MustViewportIsBottom(); got != bottom {
		t.Errorf("MustViewportIsBottom() = %v, want %v", got, bottom)
	}
}

// The variants are declared only on methods whose Zig result carries no error,
// so the one way they fail is a handle the caller already closed. That is the
// bug the panic is meant to surface, and it arrives as the typed error the
// checked call would have returned.
func TestMustVariantPanicsWithTypedErrorOnClosedHandle(t *testing.T) {
	term, err := NewTerminal(10, 3)
	if err != nil {
		t.Fatalf("NewTerminal: %v", err)
	}
	if err := term.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// What the checked call reports once the handle is gone.
	_, checkedErr := term.CursorIsAtPrompt()
	var handleErr *HandleError
	if !errors.As(checkedErr, &handleErr) {
		t.Fatalf("CursorIsAtPrompt after Close = %v, want *HandleError", checkedErr)
	}

	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatal("MustCursorIsAtPrompt on a closed terminal did not panic")
		}
		panicked, ok := recovered.(error)
		if !ok {
			t.Fatalf("panic value is %T, want error", recovered)
		}
		if !errors.As(panicked, &handleErr) {
			t.Errorf("panic value is %v, want *HandleError", panicked)
		}
	}()
	term.MustCursorIsAtPrompt()
}
