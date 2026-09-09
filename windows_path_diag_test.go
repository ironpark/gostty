//go:build windows

package gostty

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Not a test of anything: a probe, so the runner can say which shape of path
// ghostty fails to open. `error.FileNotFound` for a file that is there could be
// the 8.3 short name the runner's TEMP is spelled with, the length, or the
// separator, and these tell those apart. Always passes; read the log.
func TestWindowsPathShapes(t *testing.T) {
	term, stream := newStreamPair(t, 20, 5)
	if err := term.SetKittyGraphicsLoadingLimits(true, "", false); err != nil {
		t.Fatalf("SetKittyGraphicsLoadingLimits: %v", err)
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(wd, "diag-workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(workspace) })

	tempDir := t.TempDir()
	tempPath := writeImageFile(t, tempDir, "image.rgb")

	cases := []struct {
		what string
		path string
	}{
		{"workspace, backslashes", writeImageFile(t, workspace, "image.rgb")},
		{"temp dir (8.3 short name), backslashes", tempPath},
		{"temp dir, forward slashes", filepath.ToSlash(tempPath)},
		{"temp dir, long name expanded", expandShort(t, tempPath)},
		{"workspace, extended-length prefix", `\\?\` + filepath.Join(workspace, "image.rgb")},
	}

	id := 100
	for _, c := range cases {
		id++
		_, statErr := os.Stat(strings.TrimPrefix(c.path, `\\?\`))
		transmitPath(t, stream, id, "f", c.path)
		t.Logf("%-40s go-stat=%v ghostty-loaded=%v path=%s",
			c.what, statErr == nil, hasImage(t, term, uint32(id)), c.path)
	}
	// `go test` without -v keeps a passing test's log to itself, and the
	// workflow does not pass -v. Failing is how the probe gets read.
	t.Fail()
}

// The long spelling of a path Windows handed back with an 8.3 component.
func expandShort(t *testing.T, path string) string {
	t.Helper()
	long, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Logf("EvalSymlinks(%s): %v", path, err)
		return path
	}
	return long
}
