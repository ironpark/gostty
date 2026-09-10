package desktop

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestSaveHTMLDoesNotOverwritePreviousExports(t *testing.T) {
	dir := t.TempDir()
	names := make(map[string]bool)
	for _, body := range []string{"first", "second", "third"} {
		name, err := SaveHTML(dir, func(out io.Writer) error { _, err := io.WriteString(out, body); return err })
		if err != nil {
			t.Fatal(err)
		}
		if names[name] {
			t.Fatalf("reused filename %q", name)
		}
		names[name] = true
		if filepath.Ext(name) != ".html" {
			t.Fatalf("export filename = %q", name)
		}
		got, err := os.ReadFile(name)
		if err != nil || string(got) != body {
			t.Fatalf("export = %q, %v; want %q", got, err, body)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 3 {
		t.Fatalf("exports = %v, %v", entries, err)
	}
}

func TestSaveHTMLRemovesIncompleteOutput(t *testing.T) {
	dir := t.TempDir()
	failed := errors.New("formatter failed")
	name, err := SaveHTML(dir, func(out io.Writer) error {
		if _, err := io.WriteString(out, "partial"); err != nil {
			t.Fatal(err)
		}
		return failed
	})
	if name != "" || !errors.Is(err, failed) {
		t.Fatalf("SaveHTML = %q, %v", name, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("partial exports remain: %v, %v", entries, err)
	}
}

func TestSaveHTMLReportsCloseFailure(t *testing.T) {
	dir := t.TempDir()
	name, err := SaveHTML(dir, func(out io.Writer) error {
		// Force finalization to fail after the formatter has returned successfully.
		return out.(io.Closer).Close()
	})
	if name != "" || err == nil {
		t.Fatalf("SaveHTML = %q, %v", name, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("failed export remains: %v, %v", entries, err)
	}
}

func TestSaveHTMLCreationFailureDoesNotCallFormatter(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing")
	name, err := SaveHTML(dir, func(io.Writer) error { t.Fatal("formatter called without a file"); return nil })
	if name != "" || err == nil {
		t.Fatalf("SaveHTML = %q, %v", name, err)
	}
}
