// Command vtdump renders terminal output that was captured to a file.
//
// A program's stdout is not what you saw on screen: it is the instructions
// that produced it. Progress bars rewrite their line, `clear` erases the
// scrollback, an editor paints over itself. Reading that back with `cat`
// replays the instructions; reading it with `less -R` shows some of them.
// vtdump runs them through a real terminal and prints the grid they left.
//
//	go run ./examples/vtdump < session.log
//	some-build-tool 2>&1 | go run ./examples/vtdump -format html > build.html
//
// It is also the smallest complete example of the binding: one session, bytes
// in, a formatter out, and the events the program raised along the way.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/ironpark/gostty"
)

func main() {
	if err := run(os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "vtdump:", err)
		os.Exit(1)
	}
}

func run(stdin io.Reader, stdout, stderr io.Writer) error {
	cols := flag.Uint("cols", 80, "terminal width in cells")
	rows := flag.Uint("rows", 24, "terminal height in cells")
	scrollback := flag.Uint("scrollback", 10000, "scrollback lines to keep; pruned at page boundaries, so not an exact cap")
	format := flag.String("format", "plain", "output format: plain, vt, or html")
	keepBlanks := flag.Bool("keep-trailing-space", false, "keep trailing whitespace on each line")
	showEvents := flag.Bool("events", false, "also report titles, bells and progress on stderr")
	flag.Parse()

	opts, err := formatOptions(*format, *keepBlanks)
	if err != nil {
		return err
	}

	// One session owns the terminal and the stream that parses for it, so
	// there is one Close no matter how the function leaves.
	term, err := gostty.New(uint16(*cols), uint16(*rows),
		gostty.WithTerminalOptions(gostty.WithScrollbackMaxLines(*scrollback)),
		// Unknown sequences are kept only if a limit says how much to keep,
		// and only -events ever looks at them.
		gostty.WithStreamOptions(gostty.WithUnknownMaxBytes(unknownLimit(*showEvents))),
	)
	if err != nil {
		return err
	}
	defer term.Close()

	// A Session is an io.Writer, so feeding it is just a copy. Escape
	// sequences split across reads are fine: the parser keeps its state.
	if _, err := io.Copy(term, bufio.NewReader(stdin)); err != nil {
		return fmt.Errorf("reading stdin: %w", err)
	}

	if *showEvents {
		if err := reportEvents(term, stderr); err != nil {
			return err
		}
	}

	// Format through the terminal rather than through its screen: both reach
	// the scrollback, but only the terminal carries the palette and the
	// default colors that the styled formats need.
	//
	// WriteTo streams the pass straight out instead of building the whole
	// screen in memory first, which matters for a long capture.
	out := bufio.NewWriter(stdout)
	defer out.Flush()
	if _, err := term.Formatter(opts).WriteTo(out); err != nil {
		return err
	}
	// The formatter emits no trailing newline; a command-line tool should.
	if opts.Format != gostty.FormatterFormatHtml {
		if err := out.WriteByte('\n'); err != nil {
			return err
		}
	}
	return nil
}

func formatOptions(format string, keepBlanks bool) (gostty.FormatOptions, error) {
	opts := gostty.FormatOptions{KeepTrailingWhitespace: keepBlanks}
	switch format {
	case "plain":
		opts.Format = gostty.FormatterFormatPlain
	case "vt":
		// Reproduces colors and styles, so the output replays into a terminal.
		opts.Format = gostty.FormatterFormatVt
	case "html":
		// Palette indices become CSS variables unless they are resolved here.
		opts.Format = gostty.FormatterFormatHtml
		opts.ResolvePalette = true
	default:
		return opts, fmt.Errorf("unknown -format %q: want plain, vt or html", format)
	}
	return opts, nil
}

func unknownLimit(showEvents bool) uint {
	if showEvents {
		return 64
	}
	return 0
}

// Events are drained after feeding rather than delivered during it, so the
// parser never calls back into code that might feed it again.
func reportEvents(term *gostty.Session, stderr io.Writer) error {
	for event, err := range term.Stream().EventValues() {
		if err != nil {
			return err
		}
		switch event.Kind {
		case gostty.StreamEventTitleChanged:
			fmt.Fprintf(stderr, "title: %s\n", event.Title)
		case gostty.StreamEventPwdChanged:
			fmt.Fprintf(stderr, "pwd: %s\n", event.Pwd)
		case gostty.StreamEventDesktopNotification:
			fmt.Fprintf(stderr, "notification: %s: %s\n", event.Title, event.Body)
		case gostty.StreamEventProgressReport:
			fmt.Fprintf(stderr, "progress: %s %d%%\n", event.ProgressState, event.Progress)
		case gostty.StreamEventUnknownSequence:
			fmt.Fprintf(stderr, "unknown sequence: %q\n", event.Sequence)
		default:
			fmt.Fprintf(stderr, "%s\n", event.Kind)
		}
	}
	return nil
}
