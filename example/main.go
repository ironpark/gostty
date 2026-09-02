// Command example feeds a few escape sequences through libghostty-vt and prints
// the resulting screen.
package main

import (
	"bytes"
	"fmt"
	"log"

	"github.com/ironpark/gostty"
	"github.com/ironpark/gostty/input"
)

func main() {
	term, err := gostty.NewTerminal(40, 5)
	if err != nil {
		log.Fatal(err)
	}
	defer term.Close()

	stream, err := term.NewStream(0)
	if err != nil {
		log.Fatal(err)
	}
	// The stream must go first: its handler reaches through the terminal.
	defer stream.Close()

	if err := stream.Feed([]byte("\x1b[31mred\x1b[0m\r\n\x1b[3;5Hindented\x1b[4 q")); err != nil {
		log.Fatal(err)
	}

	screen, err := term.PlainString()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("screen:\n%s\n", screen)

	x, _ := term.CursorX()
	y, _ := term.CursorY()
	style, _ := term.CursorStyle()
	fmt.Printf("cursor: (%d,%d) style=%s\n", x, y, style)

	w, _ := gostty.CodepointWidth('가')
	fmt.Printf("width of 가: %d\n", w)

	// Input goes the other way: encoding follows the terminal's own modes.
	ev, err := input.NewKeyEvent()
	if err != nil {
		log.Fatal(err)
	}
	defer ev.Close()
	if err := ev.SetKey(input.KeyArrowUp); err != nil {
		log.Fatal(err)
	}
	var keys bytes.Buffer
	if err := input.EncodeKey(&keys, term, ev); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("arrow up encodes as: %q\n", keys.String())
}
