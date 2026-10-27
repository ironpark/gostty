//go:build !gostty_noembed

package gostty

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ironpark/gostty/internal/raw"
)

// The native library is called through purego, without cgo, so a program
// builds with CGO_ENABLED=0 and cross-compiles without a C toolchain. The
// shared library for the platform being built is embedded in the program
// (library_<goos>_<goarch>.go) and loaded when this package initializes:
// written once into the user's cache directory, by its digest, and opened
// from there.
//
// ZIGO_GOSTTY_LIBRARY_PATH or ZIGO_LIBRARY_PATH, when set, names a library
// to load instead. Building with the gostty_noembed tag leaves the embedded
// copy out; the program then calls LoadLibrary itself before using the
// package.
//
// Importing the root package, or one that imports it such as input, loads
// the library; a program that imports only sys calls LoadLibrary.

var libraryErr = loadEmbeddedLibrary()

// LoadError is why the library could not be loaded when the package
// initialized, or nil once it is loaded. Every call into the package panics
// while it is not.
func LoadError() error {
	if raw.LibraryLoaded() {
		return nil
	}
	return libraryErr
}

func loadEmbeddedLibrary() error {
	for _, name := range []string{"ZIGO_GOSTTY_LIBRARY_PATH", "ZIGO_LIBRARY_PATH"} {
		if os.Getenv(name) != "" {
			return LoadLibrary("")
		}
	}
	if len(embeddedLibrary) == 0 {
		return errors.New("gostty: no native library is embedded for this platform")
	}
	path, err := extractLibrary(embeddedLibrary)
	if err != nil {
		return fmt.Errorf("gostty: unpacking the native library: %w", err)
	}
	return LoadLibrary(path)
}

// extractLibrary writes lib into a directory of the cache named by its
// digest, unless an earlier run already did, and returns its path.
func extractLibrary(lib []byte) (string, error) {
	sum := sha256.Sum256(lib)
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	dir := filepath.Join(base, "gostty", hex.EncodeToString(sum[:12]))
	path := filepath.Join(dir, raw.DefaultLibraryName)
	if info, err := os.Stat(path); err == nil && info.Size() == int64(len(lib)) {
		return path, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	// Written beside the target and renamed over it, so a second process
	// unpacking at the same time never opens a half-written file.
	tmp, err := os.CreateTemp(dir, raw.DefaultLibraryName+".*")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(lib); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Chmod(0o755); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		// Windows will not rename over a library another process has open,
		// which is then the same file.
		if _, statErr := os.Stat(path); statErr == nil {
			return path, nil
		}
		return "", err
	}
	return path, nil
}
