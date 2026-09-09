package gostty

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// The xterm default for palette entry 1 is red; entry 196 is the top of the
// color cube's red axis. Both are checked so a bug that only moves the named
// colors is visible.
const (
	defaultPalette1   = 0xCC6666
	defaultPalette196 = 0xFF0000
)

func paletteColor(t *testing.T, term *Terminal, idx uint8) uint32 {
	t.Helper()
	got, err := term.PaletteColor(idx)
	if err != nil {
		t.Fatalf("PaletteColor(%d): %v", idx, err)
	}
	return got
}

func TestPaletteDefaults(t *testing.T) {
	term := newTerm(t, 20, 5)
	if got := paletteColor(t, term, 196); got != defaultPalette196 {
		t.Errorf("PaletteColor(196) = %#06x, want %#06x", got, defaultPalette196)
	}
	if got := paletteColor(t, term, 1); got != defaultPalette1 {
		t.Errorf("PaletteColor(1) = %#06x, want %#06x", got, defaultPalette1)
	}
}

func TestSetAndResetPaletteColor(t *testing.T) {
	term := newTerm(t, 20, 5)
	if err := term.SetPaletteColor(1, 0x123456); err != nil {
		t.Fatalf("SetPaletteColor: %v", err)
	}
	if got := paletteColor(t, term, 1); got != 0x123456 {
		t.Fatalf("after set, PaletteColor(1) = %#06x, want 0x123456", got)
	}
	// Only the entry that was set moves.
	if got := paletteColor(t, term, 2); got != paletteColor(t, term, 2) || got == 0x123456 {
		t.Errorf("PaletteColor(2) = %#06x, should be untouched", got)
	}
	if err := term.ResetPaletteColor(1); err != nil {
		t.Fatalf("ResetPaletteColor: %v", err)
	}
	if got := paletteColor(t, term, 1); got != defaultPalette1 {
		t.Errorf("after reset, PaletteColor(1) = %#06x, want %#06x", got, defaultPalette1)
	}
}

func TestResetPalette(t *testing.T) {
	term := newTerm(t, 20, 5)
	for idx := uint8(0); idx < 8; idx++ {
		if err := term.SetPaletteColor(idx, 0x010203); err != nil {
			t.Fatalf("SetPaletteColor(%d): %v", idx, err)
		}
	}
	if err := term.ResetPalette(); err != nil {
		t.Fatalf("ResetPalette: %v", err)
	}
	for idx := uint8(0); idx < 8; idx++ {
		if got := paletteColor(t, term, idx); got == 0x010203 {
			t.Errorf("PaletteColor(%d) still overridden", idx)
		}
	}
	if got := paletteColor(t, term, 1); got != defaultPalette1 {
		t.Errorf("PaletteColor(1) = %#06x, want %#06x", got, defaultPalette1)
	}
}

// Changing a default is what an embedder does when its theme changes: an entry
// the program has overridden keeps the override, and the new default is what a
// later reset lands on.
func TestSetDefaultPaletteColor(t *testing.T) {
	term := newTerm(t, 20, 5)
	if err := term.SetPaletteColor(1, 0xAAAAAA); err != nil {
		t.Fatalf("SetPaletteColor: %v", err)
	}
	if err := term.SetDefaultPaletteColor(1, 0x00FF00); err != nil {
		t.Fatalf("SetDefaultPaletteColor: %v", err)
	}
	if err := term.SetDefaultPaletteColor(2, 0x0000FF); err != nil {
		t.Fatalf("SetDefaultPaletteColor: %v", err)
	}
	if got := paletteColor(t, term, 1); got != 0xAAAAAA {
		t.Errorf("override lost: PaletteColor(1) = %#06x, want 0xaaaaaa", got)
	}
	// An entry with no override takes the new default immediately.
	if got := paletteColor(t, term, 2); got != 0x0000FF {
		t.Errorf("PaletteColor(2) = %#06x, want 0x0000ff", got)
	}
	if err := term.ResetPaletteColor(1); err != nil {
		t.Fatalf("ResetPaletteColor: %v", err)
	}
	if got := paletteColor(t, term, 1); got != 0x00FF00 {
		t.Errorf("reset landed on %#06x, want the new default 0x00ff00", got)
	}
}

func TestResetDefaultPalette(t *testing.T) {
	term := newTerm(t, 20, 5)
	if err := term.SetDefaultPaletteColor(1, 0x00FF00); err != nil {
		t.Fatalf("SetDefaultPaletteColor: %v", err)
	}
	if err := term.SetPaletteColor(2, 0xAAAAAA); err != nil {
		t.Fatalf("SetPaletteColor: %v", err)
	}
	if err := term.ResetDefaultPalette(); err != nil {
		t.Fatalf("ResetDefaultPalette: %v", err)
	}
	if got := paletteColor(t, term, 1); got != defaultPalette1 {
		t.Errorf("PaletteColor(1) = %#06x, want %#06x", got, defaultPalette1)
	}
	if got := paletteColor(t, term, 2); got != 0xAAAAAA {
		t.Errorf("override lost: PaletteColor(2) = %#06x, want 0xaaaaaa", got)
	}
}

// Wraparound (DECAWM, DEC private mode 7) is on by default, so a string longer
// than the row spills onto the next one. Turning it off pins every further
// character to the last column.
func wrapped(t *testing.T, term *Terminal) string {
	t.Helper()
	got, err := term.PlainString()
	if err != nil {
		t.Fatalf("PlainString: %v", err)
	}
	return got
}

func TestSetDefaultModeSurvivesReset(t *testing.T) {
	term, stream := newStreamPair(t, 4, 3)
	if err := term.SetDefaultMode(ModeWraparound, false); err != nil {
		t.Fatalf("SetDefaultMode: %v", err)
	}
	if err := term.PrintString("ABCDEF"); err != nil {
		t.Fatalf("PrintString: %v", err)
	}
	if got := wrapped(t, term); got != "ABCF" {
		t.Fatalf("with wraparound defaulted off, got %q, want \"ABCF\"", got)
	}

	// A program turning it back on wins until something resets the modes.
	feed(t, stream, "\x1b[H\x1b[2J\x1b[?7h")
	if err := term.PrintString("ABCDEF"); err != nil {
		t.Fatalf("PrintString: %v", err)
	}
	if got := wrapped(t, term); got != "ABCD\nEF" {
		t.Fatalf("with wraparound re-enabled, got %q, want \"ABCD\\nEF\"", got)
	}

	// And a reset returns to the default the embedder chose, not to ghostty's.
	if err := term.ResetModes(); err != nil {
		t.Fatalf("ResetModes: %v", err)
	}
	feed(t, stream, "\x1b[H\x1b[2J")
	if err := term.PrintString("ABCDEF"); err != nil {
		t.Fatalf("PrintString: %v", err)
	}
	if got := wrapped(t, term); got != "ABCF" {
		t.Errorf("after ResetModes, got %q, want \"ABCF\"", got)
	}
}

func TestSaveAndRestoreMode(t *testing.T) {
	term, stream := newStreamPair(t, 4, 3)

	// XTSAVE the mode while it is off, then let the program turn it on.
	feed(t, stream, "\x1b[?7l")
	if err := term.SaveMode(ModeWraparound); err != nil {
		t.Fatalf("SaveMode: %v", err)
	}
	feed(t, stream, "\x1b[?7h")
	if err := term.PrintString("ABCDEF"); err != nil {
		t.Fatalf("PrintString: %v", err)
	}
	if got := wrapped(t, term); got != "ABCD\nEF" {
		t.Fatalf("before restore, got %q, want \"ABCD\\nEF\"", got)
	}

	restored, err := term.RestoreMode(ModeWraparound)
	if err != nil {
		t.Fatalf("RestoreMode: %v", err)
	}
	if restored {
		t.Errorf("RestoreMode returned true, want the saved value false")
	}
	feed(t, stream, "\x1b[H\x1b[2J")
	if err := term.PrintString("ABCDEF"); err != nil {
		t.Fatalf("PrintString: %v", err)
	}
	if got := wrapped(t, term); got != "ABCF" {
		t.Errorf("after restore, got %q, want \"ABCF\"", got)
	}
}

// ResetModes also empties the XTSAVE slots, so a restore afterwards reports the
// slot's initial state rather than what was saved. Origin mode is the one
// checked here because its initial state is off, which the saved value is not.
func TestResetModesClearsSavedModes(t *testing.T) {
	term, stream := newStreamPair(t, 4, 3)
	feed(t, stream, "\x1b[?6h")
	if err := term.SaveMode(ModeOrigin); err != nil {
		t.Fatalf("SaveMode: %v", err)
	}
	if restored, err := term.RestoreMode(ModeOrigin); err != nil || !restored {
		t.Fatalf("RestoreMode() = %v, %v; want true, nil", restored, err)
	}

	if err := term.SaveMode(ModeOrigin); err != nil {
		t.Fatalf("SaveMode: %v", err)
	}
	if err := term.ResetModes(); err != nil {
		t.Fatalf("ResetModes: %v", err)
	}
	restored, err := term.RestoreMode(ModeOrigin)
	if err != nil {
		t.Fatalf("RestoreMode: %v", err)
	}
	if restored {
		t.Errorf("RestoreMode returned true, want false after ResetModes")
	}
}

func cursorX(t *testing.T, term *Terminal) uint16 {
	t.Helper()
	x, err := term.CursorX()
	if err != nil {
		t.Fatalf("CursorX: %v", err)
	}
	return x
}

// Tabstops start every 8 columns, so a tab from column 0 lands on 8.
func TestTabstopDefaults(t *testing.T) {
	term := newTerm(t, 40, 3)
	if err := term.HorizontalTab(); err != nil {
		t.Fatalf("HorizontalTab: %v", err)
	}
	if got := cursorX(t, term); got != 8 {
		t.Errorf("cursor at %d after tab, want 8", got)
	}
}

func TestSetAndUnsetTabstop(t *testing.T) {
	term := newTerm(t, 40, 3)
	if err := term.SetTabstop(3); err != nil {
		t.Fatalf("SetTabstop: %v", err)
	}
	if err := term.HorizontalTab(); err != nil {
		t.Fatalf("HorizontalTab: %v", err)
	}
	if got := cursorX(t, term); got != 3 {
		t.Fatalf("cursor at %d, want the new tabstop at 3", got)
	}
	if err := term.HorizontalTab(); err != nil {
		t.Fatalf("HorizontalTab: %v", err)
	}
	if got := cursorX(t, term); got != 8 {
		t.Fatalf("cursor at %d, want the default tabstop at 8", got)
	}

	// Removing the column 8 stop makes the next tab from 3 run to 16.
	if err := term.UnsetTabstop(8); err != nil {
		t.Fatalf("UnsetTabstop: %v", err)
	}
	if err := term.SetCursorPos(1, 4); err != nil {
		t.Fatalf("SetCursorPos: %v", err)
	}
	if err := term.HorizontalTab(); err != nil {
		t.Fatalf("HorizontalTab: %v", err)
	}
	if got := cursorX(t, term); got != 16 {
		t.Errorf("cursor at %d after unsetting 8, want 16", got)
	}
}

func TestResetTabstops(t *testing.T) {
	term := newTerm(t, 40, 3)
	if err := term.SetTabstop(3); err != nil {
		t.Fatalf("SetTabstop: %v", err)
	}
	if err := term.ResetTabstops(4); err != nil {
		t.Fatalf("ResetTabstops: %v", err)
	}
	// The column 3 stop is gone and stops now sit every four columns.
	if err := term.HorizontalTab(); err != nil {
		t.Fatalf("HorizontalTab: %v", err)
	}
	if got := cursorX(t, term); got != 4 {
		t.Fatalf("cursor at %d, want 4", got)
	}
	if err := term.HorizontalTab(); err != nil {
		t.Fatalf("HorizontalTab: %v", err)
	}
	if got := cursorX(t, term); got != 8 {
		t.Errorf("cursor at %d, want 8", got)
	}
}

// A 2x2 RGB image on disk, transmitted by path rather than inline. The path
// mediums are what `SetKittyGraphicsLoadingLimits` gates.
func writeImageFile(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	pixels := []byte{
		0xff, 0x00, 0x00, 0x00, 0xff, 0x00,
		0x00, 0x00, 0xff, 0xff, 0xff, 0xff,
	}
	if err := os.WriteFile(path, pixels, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

// `medium` is the kitty `t` key: `f` an arbitrary path, `t` a path the terminal
// is also allowed to delete afterwards.
func transmitPath(t *testing.T, s *Stream, id int, medium, path string) {
	t.Helper()
	feed(t, s, fmt.Sprintf(
		"\x1b_Ga=t,f=24,s=2,v=2,t=%s,i=%d;%s\x1b\\",
		medium, id, base64.StdEncoding.EncodeToString([]byte(path))))
}

func hasImage(t *testing.T, term *Terminal, id uint32) bool {
	t.Helper()
	_, ok, err := term.KittyImage(id)
	if err != nil {
		t.Fatalf("KittyImage(%d): %v", id, err)
	}
	return ok
}

// Both path mediums are dead on Windows: ghostty's own open of the path fails
// with `error.FileNotFound` for a file that is there, before it gets as far as
// the permission it is being asked about. That is below the binding -- nothing
// on the Go side touches the path, it travels through the pty as base64 -- so
// the tests skip rather than assert a medium the platform does not deliver.
func skipIfNoPathMediums(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("ghostty cannot open the transmitted path on Windows")
	}
}

// The default is direct transmission only, so a program cannot make the
// terminal read an arbitrary path until the embedder allows it.
func TestKittyGraphicsLoadingLimitsGateFileMedium(t *testing.T) {
	skipIfNoPathMediums(t)
	dir := t.TempDir()
	path := writeImageFile(t, dir, "image.rgb")

	term, stream := newStreamPair(t, 20, 5)
	transmitPath(t, stream, 1, "f", path)
	if hasImage(t, term, 1) {
		t.Fatalf("file medium loaded with the default limits")
	}

	if err := term.SetKittyGraphicsLoadingLimits(true, "", false); err != nil {
		t.Fatalf("SetKittyGraphicsLoadingLimits: %v", err)
	}
	transmitPath(t, stream, 2, "f", path)
	if !hasImage(t, term, 2) {
		t.Fatalf("file medium still refused after being allowed")
	}

	// And the gate closes again.
	if err := term.SetKittyGraphicsLoadingLimits(false, "", false); err != nil {
		t.Fatalf("SetKittyGraphicsLoadingLimits: %v", err)
	}
	transmitPath(t, stream, 3, "f", path)
	if hasImage(t, term, 3) {
		t.Errorf("file medium loaded after being disallowed again")
	}
}

// A directory for the temporary file medium, outside the system temp
// directory on purpose. ghostty always accepts `/tmp` and `/dev/shm` for that
// medium, whatever directory the embedder allowed, so a `t.TempDir()` on Linux
// is accepted by the rule under test and by the blanket one -- the test cannot
// tell which. A directory next to the package keeps only the rule under test
// in play, on every platform.
func allowedTempMedium(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp(".", "kitty-tmp-medium")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatalf("Abs(%s): %v", dir, err)
	}
	return abs
}

// The temporary file medium is a separate permission scoped to one directory,
// which the binding copies rather than borrows: the Go string backing it is
// gone by the time the image is transmitted.
func TestKittyGraphicsLoadingLimitsTempDir(t *testing.T) {
	skipIfNoPathMediums(t)
	dir := allowedTempMedium(t)
	term, stream := newStreamPair(t, 20, 5)

	transmitPath(t, stream, 1, "t", writeImageFile(t, dir, "tty-graphics-protocol-a.rgb"))
	if hasImage(t, term, 1) {
		t.Fatalf("temporary file medium loaded with the default limits")
	}

	if err := term.SetKittyGraphicsLoadingLimits(false, dir, false); err != nil {
		t.Fatalf("SetKittyGraphicsLoadingLimits: %v", err)
	}
	transmitPath(t, stream, 2, "t", writeImageFile(t, dir, "tty-graphics-protocol-b.rgb"))
	if !hasImage(t, term, 2) {
		t.Fatalf("temporary file medium refused inside the allowed directory")
	}

	// Setting the limits again frees the previous copy of the directory; the
	// medium must still work through the replacement.
	if err := term.SetKittyGraphicsLoadingLimits(false, dir, false); err != nil {
		t.Fatalf("SetKittyGraphicsLoadingLimits: %v", err)
	}
	transmitPath(t, stream, 3, "t", writeImageFile(t, dir, "tty-graphics-protocol-c.rgb"))
	if !hasImage(t, term, 3) {
		t.Fatalf("temporary file medium refused after the limits were replaced")
	}

	// Allowing a temporary directory is not the same as allowing any path.
	transmitPath(t, stream, 4, "f", writeImageFile(t, dir, "tty-graphics-protocol-d.rgb"))
	if hasImage(t, term, 4) {
		t.Errorf("the temporary directory permission also allowed the file medium")
	}

	// A file outside the allowed directory is still refused.
	transmitPath(t, stream, 5, "t", writeImageFile(t, allowedTempMedium(t), "tty-graphics-protocol-e.rgb"))
	if hasImage(t, term, 5) {
		t.Errorf("temporary file medium loaded from outside the allowed directory")
	}
}
