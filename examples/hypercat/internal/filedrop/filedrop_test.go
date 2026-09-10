package filedrop

import (
	"strings"
	"testing"
	"testing/fstest"
)

// The staged representation a program asks for is text/uri-list, which is CRLF
// separated file URIs rather than the paths as they were dropped.
func TestURIListIsFileURIs(t *testing.T) {
	got := uriList([]string{"/tmp/a b.txt"})
	if !strings.HasSuffix(got, "\r\n") {
		t.Errorf("uriList() = %q, want it to end in CRLF", got)
	}
	if !strings.Contains(got, "file://") || !strings.Contains(got, "a%20b.txt") {
		t.Errorf("uriList() = %q, want an escaped file URI", got)
	}
}

func TestDroppedPathsReadsTheDropRoot(t *testing.T) {
	if got := Paths(nil); got != nil {
		t.Errorf("Paths(nil) = %v, want nothing", got)
	}
	if got := Paths(fstest.MapFS{}); len(got) != 0 {
		t.Errorf("Paths(empty) = %v, want nothing", got)
	}
	dropped := fstest.MapFS{"one.txt": {}, "two.txt": {}}
	if got := Paths(dropped); len(got) != 2 {
		t.Errorf("Paths() = %v, want both entries", got)
	}
}

// What is staged for a drop is the intersection of what this window can make
// with what the program registered for: a type it did not ask for is noise, and
// one it asked for that this window cannot make is a promise it cannot keep.
func TestOfferedRepresentationsIntersect(t *testing.T) {
	both := Offered([]string{"text/plain", "text/uri-list"})
	if len(both) != 2 || both[0].MIME != "text/uri-list" {
		t.Errorf("offered %v, want both in this window's order", mimesOf(both))
	}
	if one := Offered([]string{"text/plain"}); len(one) != 1 || one[0].MIME != "text/plain" {
		t.Errorf("offered %v, want only text/plain", mimesOf(one))
	}
	if none := Offered([]string{"image/png"}); len(none) != 0 {
		t.Errorf("offered %v for a type this window cannot make", mimesOf(none))
	}
	// The bytes come from the same table that named the type, so a drop cannot
	// advertise one thing and stage another.
	for _, representation := range both {
		if len(representation.Body([]string{"/tmp/a"})) == 0 {
			t.Errorf("%s staged nothing", representation.MIME)
		}
	}
}

func mimesOf(representations []Representation) []string {
	names := make([]string, 0, len(representations))
	for _, r := range representations {
		names = append(names, r.MIME)
	}
	return names
}
