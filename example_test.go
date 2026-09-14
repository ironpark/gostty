package gostty_test

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/ironpark/gostty"
)

// The shortest useful program: make a session, write VT data into it, read the
// screen back as text. A Session owns a terminal and the stream that parses
// bytes for it, so one Close covers both.
func Example() {
	term, err := gostty.New(80, 24)
	if err != nil {
		log.Fatal(err)
	}
	defer term.Close()

	fmt.Fprint(term, "hello\r\n\x1b[31mworld\x1b[0m")

	text, err := term.PlainText()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(strings.TrimRight(text, "\n"))
	// Output:
	// hello
	// world
}

// A terminal is a state machine, not a byte sink: escape sequences move a
// cursor around, and what you read back is the grid they left behind.
func Example_cursorMovement() {
	term := gostty.MustNew(20, 3)
	defer term.Close()

	// Write three lines, then jump back to row 1 column 1 and overwrite.
	fmt.Fprint(term, "one\r\ntwo\r\nthree")
	fmt.Fprint(term, "\x1b[1;1H")
	fmt.Fprint(term, "ONE")

	text, _ := term.PlainText()
	fmt.Println(strings.TrimRight(text, "\n"))
	// Output:
	// ONE
	// two
	// three
}

// Options configure the terminal and its stream at construction, so they hold
// from the first byte parsed rather than from a setter afterwards.
func ExampleNew_options() {
	term, err := gostty.New(80, 24,
		gostty.WithTerminalOptions(gostty.WithScrollbackMaxLines(1000)),
		gostty.WithStreamOptions(
			gostty.WithVersionReport("myapp", "1.0"),
			gostty.WithEnquiryResponse("ok"),
		),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer term.Close()

	// XTVERSION: the program asks what terminal it is talking to.
	fmt.Fprint(term, "\x1b[>0q")

	var reply strings.Builder
	if err := term.Stream().WriteReplies(&reply); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%q\n", reply.String())
	// Output:
	// "\x1bP>|myapp 1.0\x1b\\"
}

// Colors cross as RGB rather than as a packed integer, so which byte is which
// is in the type. RGB satisfies image/color.Color, so it can be handed to the
// standard drawing packages directly.
func ExampleRGB() {
	term := gostty.MustNew(20, 3)
	defer term.Close()

	// OSC 11: the program sets the default background.
	fmt.Fprint(term, "\x1b]11;rgb:1a/1b/26\x1b\\")

	bg, ok, err := term.Terminal().BackgroundColor()
	if err != nil || !ok {
		log.Fatal(err)
	}
	fmt.Println(bg)
	fmt.Printf("R=%d G=%d B=%d packed=%#06x\n", bg.R, bg.G, bg.B, bg.Uint32())

	parsed, err := gostty.ParseRGB("#ff8800")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(parsed == gostty.NewRGB(0xff, 0x88, 0x00))
	// Output:
	// #1a1b26
	// R=26 G=27 B=38 packed=0x1a1b26
	// true
}

// A Formatter keeps its options and its buffer, so a program formatting the
// screen repeatedly pays for neither again. Append is the allocation-free
// form: hand back the same slice each time.
func ExampleFormatter() {
	term := gostty.MustNew(20, 3)
	defer term.Close()

	f := term.Formatter(gostty.FormatOptions{
		Format:                 gostty.FormatterFormatPlain,
		KeepTrailingWhitespace: false,
	})

	buf := make([]byte, 0, 256)
	for _, line := range []string{"first", "second"} {
		fmt.Fprintf(term, "%s\r\n", line)

		var err error
		if buf, err = f.Append(buf[:0]); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("%q\n", strings.TrimRight(string(buf), "\n"))
	}
	// Output:
	// "first"
	// "first\nsecond"
}

// WriteTo streams a formatting pass straight into a writer, buffering nothing
// on the way. That makes it the cheapest form when the destination is a file
// or a socket rather than memory.
func ExampleFormatter_WriteTo() {
	term := gostty.MustNew(20, 2)
	defer term.Close()

	fmt.Fprint(term, "\x1b[1;31mred\x1b[0m and plain")

	f := term.Formatter(gostty.FormatOptions{Format: gostty.FormatterFormatPlain})
	n, err := f.WriteTo(os.Stdout)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("(%d bytes)\n", n)
	// Output:
	// red and plain(13 bytes)
}

// A program on the far end of a pty asks the terminal for things: it sets a
// title, reports progress, rings the bell. Those arrive as events to drain
// after feeding, rather than as callbacks during it.
func ExampleSession_events() {
	term := gostty.MustNew(80, 24)
	defer term.Close()

	fmt.Fprint(term, "\x1b]0;build\x1b\\")  // OSC 0: set the title.
	fmt.Fprint(term, "\x1b]9;4;1;40\x1b\\") // OSC 9;4: progress at 40%.
	fmt.Fprint(term, "\a")                  // BEL.

	for event, err := range term.Stream().EventValues() {
		if err != nil {
			log.Fatal(err)
		}
		switch event.Kind {
		case gostty.StreamEventTitleChanged:
			fmt.Printf("title: %s\n", event.Title)
		case gostty.StreamEventProgressReport:
			fmt.Printf("progress: %s %d%%\n", event.ProgressState, event.Progress)
		default:
			fmt.Printf("event: %s\n", event.Kind)
		}
	}
	// Output:
	// title: build
	// progress: set 40%
	// event: bell
}

// Some sequences are questions. The terminal composes the answer; the
// embedder is responsible for writing it back to the pty.
func ExampleStream_WriteReplies() {
	term := gostty.MustNew(80, 24)
	defer term.Close()

	// DSR 6: "where is the cursor?" after moving it to row 5, column 10.
	fmt.Fprint(term, "\x1b[5;10H\x1b[6n")

	stream := term.Stream()
	if has, err := stream.HasReplies(); err != nil || !has {
		log.Fatal("expected a reply", err)
	}
	var toPty strings.Builder
	if err := stream.WriteReplies(&toPty); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%q\n", toPty.String())
	// Output:
	// "\x1b[5;10R"
}

// What a renderer reads each frame: one Update crosses into native code, and
// everything after it is served from the snapshot it took.
func ExampleRenderState() {
	term := gostty.MustNew(10, 2)
	defer term.Close()

	// SGR 32 names palette slot 2, not a literal green: what comes back is
	// already resolved against the terminal's palette.
	fmt.Fprint(term, "\x1b[1;32mhi\x1b[0m")

	state, err := gostty.NewRenderState()
	if err != nil {
		log.Fatal(err)
	}
	defer state.Close()
	if err := state.Update(term.Terminal()); err != nil {
		log.Fatal(err)
	}

	count, err := state.CellCount()
	if err != nil {
		log.Fatal(err)
	}
	cells := make([]gostty.RenderCell, count)
	if _, err := state.Cells(cells); err != nil {
		log.Fatal(err)
	}

	for _, cell := range cells[:2] {
		fmt.Printf("%c fg=%v bold=%v\n", cell.Codepoint, cell.Fg, cell.Flags.Bold)
	}

	cursor, _ := state.Cursor()
	fmt.Printf("cursor at %d,%d\n", cursor.X, cursor.Y)
	// Output:
	// h fg=#b5bd68 bold=true
	// i fg=#b5bd68 bold=true
	// cursor at 2,0
}

// Scrollback outlives the visible grid, and search runs over all of it.
func ExampleSearch() {
	term := gostty.MustNew(20, 2)
	defer term.Close()

	if err := term.Terminal().SetScrollbackMaxBytes(1 << 20); err != nil {
		log.Fatal(err)
	}
	for _, line := range []string{"alpha", "beta", "gamma needle", "delta"} {
		fmt.Fprintf(term, "%s\r\n", line)
	}

	search, err := term.Terminal().NewSearch("needle")
	if err != nil {
		log.Fatal(err)
	}
	defer search.Close()
	if err := search.All(); err != nil {
		log.Fatal(err)
	}

	count, err := search.MatchCount()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%d match\n", count)
	// Output:
	// 1 match
}

// A snapshot serializes the whole terminal -- grid, scrollback, modes, and any
// escape sequence left half-parsed -- so a session can be put down and picked
// back up. Nothing about the resumed terminal remembers it was ever stopped.
func ExampleSnapshot() {
	// The stream keeps a replay-safe suffix only if it is asked to; without a
	// limit an unfinished sequence is simply dropped.
	term := gostty.MustNew(20, 3,
		gostty.WithStreamOptions(gostty.WithContinuationMaxBytes(64)),
	)
	fmt.Fprint(term, "before\r\n")
	// A truncated sequence: the parser is mid-escape when we stop.
	fmt.Fprint(term, "\x1b[1;3")

	var saved strings.Builder
	if err := term.Stream().WriteSnapshot(&saved); err != nil {
		log.Fatal(err)
	}
	if err := term.Close(); err != nil {
		log.Fatal(err)
	}

	snapshot, err := gostty.DecodeSnapshot(strings.NewReader(saved.String()), 64)
	if err != nil {
		log.Fatal(err)
	}
	defer snapshot.Close()

	restoredTerm, err := snapshot.NewTerminal()
	if err != nil {
		log.Fatal(err)
	}
	defer restoredTerm.Close()
	continuation, err := snapshot.Continuation()
	if err != nil {
		log.Fatal(err)
	}

	// Resume: replay the unfinished sequence, then carry on.
	stream, err := restoredTerm.NewStream(0)
	if err != nil {
		log.Fatal(err)
	}
	defer stream.Close()
	stream.Write(continuation)
	fmt.Fprint(stream, "1mafter\x1b[0m")

	text, _ := restoredTerm.PlainText()
	fmt.Println(strings.TrimRight(text, "\n"))
	// Output:
	// before
	// after
}
