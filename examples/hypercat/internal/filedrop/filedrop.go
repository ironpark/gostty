// Package filedrop prepares dropped paths for terminal MIME transfers.
package filedrop

import (
	"io/fs"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
)

// Representation is one way this window can describe a set of dropped
// paths. A program takes the drop by asking for one of them by name, so what
// is staged and what it is told about have to be the same list -- which is
// why it is a list.
type Representation struct {
	MIME string
	Body func(paths []string) []byte
}

// What this window can make, in the order a program that asked for more than
// one of them gets it.
var representations = []Representation{
	{"text/uri-list", func(paths []string) []byte { return []byte(uriList(paths)) }},
	{"text/plain", func(paths []string) []byte { return []byte(strings.Join(paths, "\n")) }},
}

// Offered is what this window can make and the program will
// take: the intersection, in this window's order of preference.
func Offered(registered []string) []Representation {
	var offered []Representation
	for _, representation := range representations {
		if slices.Contains(registered, representation.MIME) {
			offered = append(offered, representation)
		}
	}
	return offered
}

// Paths lists what was dropped, at the root of the virtual filesystem
// Ebitengine hands over. Directories come along: what to do with one is the
// program's business, and a shell handed a directory path is not confused.
func Paths(dropped fs.FS) []string {
	if dropped == nil {
		return nil
	}
	entries, err := fs.ReadDir(dropped, ".")
	if err != nil || len(entries) == 0 {
		return nil
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		// The names are real paths on the desktop platforms, which is what
		// both the shell and the URI list want.
		paths = append(paths, entry.Name())
	}
	return paths
}

// uriList is the paths as text/uri-list, which is CRLF separated file URIs.
func uriList(paths []string) string {
	var b strings.Builder
	for _, path := range paths {
		abs, err := filepath.Abs(path)
		if err != nil {
			abs = path
		}
		b.WriteString((&url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}).String())
		b.WriteString("\r\n")
	}
	return b.String()
}
