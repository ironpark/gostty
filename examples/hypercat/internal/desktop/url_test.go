package desktop

import "testing"

// A URI out of a pty is not to be handed to the platform opener whatever it
// says: only the schemes a terminal is for are opened.
func TestOnlyKnownSchemesAreOpenable(t *testing.T) {
	for _, test := range []struct {
		uri  string
		want bool
	}{
		{"https://example.com", true},
		{"http://example.com", true},
		{"mailto:someone@example.com", true},
		{"file:///tmp/notes.txt", true},
		{"javascript:alert(1)", false},
		{"vnd.dangerous://run", false},
		{"not a uri at all", false},
	} {
		if got := openable(test.uri); got != test.want {
			t.Errorf("openable(%q) = %v, want %v", test.uri, got, test.want)
		}
	}
}
