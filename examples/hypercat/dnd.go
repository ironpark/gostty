package main

import (
	"io/fs"
	"log"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/ironpark/gostty"
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
func (tab *terminalTab) onDrag(notice gostty.DragNotice, mimes string) {
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
func (tab *terminalTab) handleDrop() error {
	paths := droppedPaths(ebiten.DroppedFiles())
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
		return tab.pasteText(shellQuote(paths))
	}

	// What the program registered for, which is what a real emulator would
	// stage rather than offering the two types this one always has.
	if mimes, err := tab.stream.DragRegisteredMimes(); err == nil {
		log.Printf("drag: dropping %d path(s) for %s", len(paths), mimes)
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
	// wants, which the terminal answers from what was staged here.
	if err := tab.stream.DragClearItems(); err != nil {
		return err
	}
	if err := tab.stream.DragAddItem("text/uri-list", []byte(uriList(paths))); err != nil {
		return err
	}
	if err := tab.stream.DragAddItem("text/plain", []byte(strings.Join(paths, "\n"))); err != nil {
		return err
	}
	if err := tab.stream.DragMove(move, "text/uri-list text/plain"); err != nil {
		return err
	}
	if err := tab.stream.DragDrop(move); err != nil {
		return err
	}
	// The moves and the drop are written as replies, the same as a device
	// status report, so they go out with the rest of this frame's answers.
	return tab.stream.WriteReplies(tab.shell.Pty)
}

// droppedPaths lists what was dropped, at the root of the virtual filesystem
// Ebitengine hands over. Directories come along: what to do with one is the
// program's business, and a shell handed a directory path is not confused.
func droppedPaths(dropped fs.FS) []string {
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

// shellQuote is the paths as a line a shell will read as filenames, which is
// what a drop with no program listening turns into.
func shellQuote(paths []string) string {
	quoted := make([]string, 0, len(paths))
	for _, path := range paths {
		quoted = append(quoted, "'"+strings.ReplaceAll(path, "'", `'\''`)+"'")
	}
	return strings.Join(quoted, " ") + " "
}
