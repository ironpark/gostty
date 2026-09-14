package input_test

import (
	"fmt"
	"log"
	"strings"

	"github.com/ironpark/gostty"
	"github.com/ironpark/gostty/input"
)

// Encoding is mode-aware: the same key press produces different bytes
// depending on what the program running on the pty has turned on. That is why
// the terminal is a parameter -- it is the thing that knows the modes.
func ExampleEncodeKey() {
	term := gostty.MustNew(80, 24)
	defer term.Close()

	press := func(event input.KeyEvent, text string) {
		var out strings.Builder
		if err := input.EncodeKey(&out, term.Terminal(), event, text); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("%q\n", out.String())
	}

	// A plain letter is itself.
	press(input.KeyEvent{Key: input.KeyKeyA}, "a")
	// Ctrl+C is the control byte, not the letter.
	press(input.KeyEvent{Key: input.KeyKeyC, Mods: input.KeyMods{Ctrl: true}}, "")
	// The up arrow in the default cursor-key mode.
	press(input.KeyEvent{Key: input.KeyArrowUp}, "")

	// DECCKM: the program switches to application cursor keys, and the very
	// same press now encodes differently.
	fmt.Fprint(term, "\x1b[?1h")
	press(input.KeyEvent{Key: input.KeyArrowUp}, "")
	// Output:
	// "a"
	// "\x03"
	// "\x1b[A"
	// "\x1bOA"
}

// A mouse event encodes to nothing at all unless the program asked for mouse
// reporting, so a GUI can hand every event over and let the terminal decide.
func ExampleEncodeMouse() {
	term := gostty.MustNew(80, 24)
	defer term.Close()

	click := input.MouseEvent{
		Action:    input.MouseActionPress,
		Button:    input.MouseButtonLeft,
		HasButton: true,
		X:         25,
		Y:         45,
	}
	// The grid's pixel geometry, so the terminal can turn a pixel position
	// into a cell. Here a 10x20 cell with no padding, so (25, 45) is the cell
	// at column 3, row 3 counting from one.
	size := input.RenderSize{
		ScreenWidth:  800,
		ScreenHeight: 480,
		CellWidth:    10,
		CellHeight:   20,
	}

	encode := func() string {
		var out strings.Builder
		if err := input.EncodeMouse(&out, term.Terminal(), click, size, true); err != nil {
			log.Fatal(err)
		}
		return out.String()
	}

	fmt.Printf("before tracking: %q\n", encode())

	// Modes 1000 (button tracking) and 1006 (SGR encoding).
	fmt.Fprint(term, "\x1b[?1000h\x1b[?1006h")
	fmt.Printf("after tracking:  %q\n", encode())
	// Output:
	// before tracking: ""
	// after tracking:  "\x1b[<0;3;3M"
}

// A paste is framed only if the program turned on bracketed paste, and the
// contents are checked first: control bytes in a paste are how a pasted line
// becomes a command the user never typed.
func ExampleEncodePaste() {
	term := gostty.MustNew(80, 24)
	defer term.Close()

	text := []byte("hello")
	fmt.Println(input.IsSafePaste(text))
	fmt.Println(input.IsSafePaste([]byte("rm -rf /\nyes")))

	var out strings.Builder
	if err := input.EncodePaste(&out, term.Terminal(), text); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("plain:     %q\n", out.String())

	// Mode 2004: bracketed paste.
	fmt.Fprint(term, "\x1b[?2004h")
	out.Reset()
	if err := input.EncodePaste(&out, term.Terminal(), text); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("bracketed: %q\n", out.String())
	// Output:
	// true
	// false
	// plain:     "hello"
	// bracketed: "\x1b[200~hello\x1b[201~"
}

// Key carries what a keymap needs to decide about a press without a table of
// its own, and reads and writes the W3C names a browser or a config file uses.
func ExampleKey() {
	key, ok := input.KeyFromW3C("ArrowLeft")
	fmt.Println(key, ok)

	a, _ := input.KeyFromASCII('a')
	codepoint, hasCodepoint := a.Codepoint()
	fmt.Printf("%s: printable=%v codepoint=%q(%v) w3c=%s\n",
		a, a.Printable(), codepoint, hasCodepoint, a.W3C())

	shift := input.KeyShiftLeft
	fmt.Printf("%s: modifier=%v shift=%v\n", shift, shift.Modifier(), shift.LeftOrRightShift())
	// Output:
	// arrow_left true
	// key_a: printable=true codepoint='a'(true) w3c=KeyA
	// shift_left: modifier=true shift=true
}
