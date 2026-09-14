package main

import (
	"flag"
	"os"
	"strings"
	"testing"
)

// render runs the command over in-memory streams, with the flags reset to
// their defaults plus whatever the case overrides.
func render(t *testing.T, input string, args ...string) (string, string) {
	t.Helper()
	flag.CommandLine = flag.NewFlagSet("vtdump", flag.ContinueOnError)
	os.Args = append([]string{"vtdump"}, args...)

	var stdout, stderr strings.Builder
	if err := run(strings.NewReader(input), &stdout, &stderr); err != nil {
		t.Fatalf("run(%q, %v): %v", input, args, err)
	}
	return stdout.String(), stderr.String()
}

// The point of the tool: the bytes are instructions, and what is printed is
// the grid they left, not the bytes themselves.
func TestRenderResolvesOverwrites(t *testing.T) {
	// A progress line that rewrites itself, the way a build tool does.
	input := "building...\rdone.      \r\nnext\r\n"
	out, _ := render(t, input)
	if want := "done.\nnext\n"; out != want {
		t.Errorf("run() = %q, want %q", out, want)
	}
}

func TestRenderCursorAddressing(t *testing.T) {
	out, _ := render(t, "one\r\ntwo\r\nthree\x1b[1;1HONE")
	if want := "ONE\ntwo\nthree\n"; out != want {
		t.Errorf("run() = %q, want %q", out, want)
	}
}

// Escape sequences split across reads must not break: the parser keeps its
// state between writes, and io.Copy chunks wherever it likes.
func TestRenderSplitSequence(t *testing.T) {
	long := strings.Repeat("x", 40000)
	out, _ := render(t, long+"\x1b[31mred\x1b[0m")
	if !strings.Contains(out, "red") {
		t.Errorf("run() lost text after a long input; got %d bytes", len(out))
	}
}

func TestRenderFormats(t *testing.T) {
	const input = "\x1b[31mred\x1b[0m plain\n"
	for _, tc := range []struct {
		format string
		want   string
	}{
		{"vt", "\x1b[38;5;1mred\x1b[0m plain"},
		{"html", "red"},
	} {
		out, _ := render(t, input, "-format", tc.format)
		if !strings.Contains(out, tc.want) {
			t.Errorf("-format %s = %q, want it to contain %q", tc.format, out, tc.want)
		}
	}
	// html resolves palette indices, which needs the terminal's palette
	// rather than the screen's -- the screen does not carry one.
	out, _ := render(t, input, "-format", "html")
	if !strings.Contains(out, "rgb(") {
		t.Errorf("-format html = %q, want resolved rgb() colors", out)
	}
}

func TestRenderRejectsUnknownFormat(t *testing.T) {
	flag.CommandLine = flag.NewFlagSet("vtdump", flag.ContinueOnError)
	os.Args = []string{"vtdump", "-format", "yaml"}
	if err := run(strings.NewReader(""), &strings.Builder{}, &strings.Builder{}); err == nil {
		t.Error("run() with -format yaml returned no error")
	}
}

// Events are what the program asked the terminal for, drained after feeding.
func TestRenderEvents(t *testing.T) {
	input := "\x1b]0;mybuild\x1b\\\x1b]9;4;1;70\x1b\\text\a"
	_, stderr := render(t, input, "-events")
	for _, want := range []string{"title: mybuild", "progress: set 70%", "bell"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr = %q, want it to contain %q", stderr, want)
		}
	}
}

// Rows that scroll off are still in the output, because the terminal formatter
// reaches the scrollback.
func TestRenderKeepsScrollback(t *testing.T) {
	var input strings.Builder
	for i := range 10 {
		input.WriteString(string(rune('a'+i)) + "\r\n")
	}
	out, _ := render(t, input.String(), "-rows", "3")
	if !strings.Contains(out, "a") || !strings.Contains(out, "j") {
		t.Errorf("run(-rows 3) = %q, want both the first and last line", out)
	}
}
