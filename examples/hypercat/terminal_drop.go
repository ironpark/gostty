package main

import (
	"log"
	"strings"

	"github.com/ironpark/gostty"
	"github.com/ironpark/gostty/examples/hypercat/internal/filedrop"
)

// Drag and drop, in the sense the terminal knows about it: OSC 72, where a
// program running in the terminal registers the types it will take and is then
// told about a drag happening over the window it cannot see.
//
// The window system half is this program's -- Ebitengine says what was dropped
// and where -- and the protocol half is the terminal's. Which is the whole
// point: a program that has not registered gets the old behaviour, its paths
// typed at the prompt as if the user had pasted them, and one that has gets the
// bytes.

// onDrag is the terminal's side of the conversation: the program registering,
// answering a move with the operation it will take, and concluding a drop.
// None of it needs an answer here, which is why it can be a log line -- but a
// real emulator would set the pointer shape from `Accepted` and refuse the drop
// when the program declined it.
func (tab *terminal) onDrag(notice gostty.DragNotice, mimes string) {
	switch notice.Event {
	case gostty.DragEventRegistration:
		if mimes == "" {
			log.Print("drag: the program stopped accepting drops")
		} else {
			log.Printf("drag: the program accepts %s", mimes)
		}
	case gostty.DragEventAcceptance:
		if notice.Answered {
			log.Printf("drag: the program will %s", notice.Accepted)
		}
	default:
		log.Printf("drag: %s", notice.Event)
	}
}

// handleDrop takes what the window system dropped on this tab.
//
// Ebitengine reports the drop and nothing before it -- there is no hover to
// pass on -- so the move and the drop are announced together, which is the
// least the protocol accepts: a program is told where the pointer is before it
// is told to take what is under it.
func (tab *terminal) handleDrop() error {
	paths := filedrop.Paths(tab.input.Dropped)
	if len(paths) == 0 {
		return nil
	}

	active, err := tab.stream.DragActive()
	if err != nil {
		return err
	}
	if !active {
		// No program asked for drops, so this is the convention every terminal
		// has: the paths are typed at the prompt, quoted, for the shell to do
		// something with.
		text, err := tab.shell.QuotePaths(paths)
		if err != nil {
			log.Printf("drop: %v", err)
			return nil
		}
		return tab.pasteText(text)
	}

	// What the program registered for decides what is staged: offering a type
	// it did not ask for is noise, and staging one it did ask for but this
	// window cannot make is a promise that cannot be kept.
	registered, err := tab.stream.DragRegisteredMimes()
	if err != nil {
		return err
	}
	offered := filedrop.Offered(strings.Fields(registered))
	if len(offered) == 0 {
		log.Printf("drag: the program accepts %s, and this window has none of it", registered)
		return nil
	}

	px, py := tab.cursorPosition()
	col, row := tab.grid().cellAt(px, py)
	move := gostty.DragMove{
		CellX: uint32(col), CellY: uint32(row),
		PixelX: int32(px), PixelY: int32(py),
		// What this window is willing to do with the files. They are not being
		// taken out of anywhere, so a copy is all that is offered.
		Operations: gostty.DragOperations{Copy: true},
	}
	// The representations are staged before the drop, because the program does
	// not receive them: it is told a drop happened and asks for the one it
	// wants, which the terminal answers from what was staged here. The list it
	// is told about and the bytes staged for it come from the same iteration,
	// so the two cannot fall out of step.
	if err := tab.stream.DragClearItems(); err != nil {
		return err
	}
	mimes := make([]string, 0, len(offered))
	for _, representation := range offered {
		if err := tab.stream.DragAddItem(representation.MIME, representation.Body(paths)); err != nil {
			return err
		}
		mimes = append(mimes, representation.MIME)
	}
	if err := tab.stream.DragMove(move, strings.Join(mimes, " ")); err != nil {
		return err
	}
	if err := tab.stream.DragDrop(move); err != nil {
		return err
	}
	// The moves and the drop are written as replies, the same as a device
	// status report, so they go out with the rest of this frame's answers.
	return tab.stream.WriteReplies(tab.shell.Pty)
}
