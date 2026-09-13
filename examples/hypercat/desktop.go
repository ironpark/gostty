package main

import (
	"context"
	"fmt"
	"image"
	"io"
	"io/fs"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/ironpark/gostty"
	"github.com/ironpark/gostty/examples/hypercat/keys"
	sysclip "golang.design/x/clipboard"
)

// The terminal's conversation with the desktop: the clipboard (OSC 52),
// hyperlinks it opens (OSC 8), files dropped on it (OSC 72), and the files
// it writes.

// Clipboard ---------------------------------------------------------------------

// clipboard is the window's clipboard, shared by every tab: the system one
// when there is one, and otherwise a process-local buffer, which at least
// lets OSC 52 and paste agree. A headless box has no system clipboard.
type clipboard struct {
	local  []byte
	system bool
}

func (c *clipboard) init() {
	if err := sysclip.Init(); err != nil {
		log.Printf("no system clipboard, staying in-process: %v", err)
		return
	}
	c.system = true
}

// hold keeps bytes for this process alone. A program writing OSC 52 asked the
// terminal to remember something; putting it on the user's system clipboard
// unasked is not the terminal's to decide.
func (c *clipboard) hold(data []byte) { c.local = append(c.local[:0], data...) }

// copy is the user's own copy, which does reach the system clipboard.
func (c *clipboard) copy(text string) error {
	c.hold([]byte(text))
	if !c.system {
		return nil
	}
	_, err := sysclip.Write(context.Background(), sysclip.FmtText, c.local)
	return err
}

func (c *clipboard) paste() []byte {
	if c.system {
		if text, err := sysclip.Read(context.Background(), sysclip.FmtText); err == nil && len(text) > 0 {
			return text
		}
	}
	return c.local
}

// OSC 52 requests are answered inside the callback, while Feed runs.
func (tab *terminal) writeClipboard(req *gostty.ClipboardRequest) {
	if n, err := req.ContentCount(); err == nil && n > 0 {
		if data, err := req.ContentData(0); err == nil {
			tab.clipboard.hold(data)
		}
	}
	_ = req.Allow(false)
}

// readClipboard shares the clipboard with the program at once. A production
// emulator should ask the user before answering an OSC 52 read.
func (tab *terminal) readClipboard(req *gostty.ClipboardRequest) {
	_ = req.ReplyText(string(tab.clipboard.paste()), false)
}

// Hyperlinks --------------------------------------------------------------------
//
// OSC 8: a program marks a run of cells as a link and gives the URI once, so
// the URI comes from the terminal rather than from a pattern over the text.
// The link a cell belongs to is not in RenderCell; RenderState.HyperlinkAt is
// asked one cell at a time, which is why only the hovered row is scanned.

// hoveredLink is the link under the pointer as of the last refresh.
type hoveredLink struct {
	uri        string
	row        int
	start, end int // the half-open run of columns the link covers in that row
}

func (l hoveredLink) valid() bool { return l.uri != "" && l.end > l.start }

func (tab *terminal) refreshLink() error {
	px, py := tab.cursorPosition()
	pointer := image.Pt(px, py)
	if !tab.reports.focused { // nothing is under a pointer the window is not shown
		pointer = image.Pt(-1, -1)
	}
	return tab.frame.readLink(tab.state, tab.grid(), pointer)
}

// readLink finds the link under a pointer position, negative when the
// pointer is off the grid. The hovered cell is asked every frame; its run is
// only walked when it is a link the last frame did not already have. A link
// that soft-wraps is underlined a row at a time; what opens is the whole URI.
func (f *frame) readLink(state *gostty.RenderState, g grid, pointer image.Point) error {
	previous := f.link
	f.link = hoveredLink{}
	if pointer.X < 0 || pointer.Y < 0 || g.cols == 0 || g.rows == 0 {
		return nil
	}
	col, row := g.cellAt(pointer.X, pointer.Y)
	uri, ok, err := state.HyperlinkAt(uint16(col), uint16(row))
	if err != nil || !ok || uri == "" {
		return err
	}
	// The same link in the same row still covering this cell: the run cannot
	// have changed unless the terminal rewrote the row.
	if previous.uri == uri && previous.row == row &&
		col >= previous.start && col < previous.end && !f.redraw.rewritten(row) {
		f.link = previous
		return nil
	}
	sameLink := func(x int) (bool, error) {
		at, ok, err := state.HyperlinkAt(uint16(x), uint16(row))
		return ok && at == uri, err
	}
	link := hoveredLink{uri: uri, row: row, start: col, end: col + 1}
	for x := col - 1; x >= 0; x-- {
		if same, err := sameLink(x); err != nil || !same {
			if err != nil {
				return err
			}
			break
		}
		link.start = x
	}
	for x := col + 1; x < g.cols; x++ {
		if same, err := sameLink(x); err != nil || !same {
			if err != nil {
				return err
			}
			break
		}
		link.end = x + 1
	}
	f.link = link
	return nil
}

// openLink opens the hovered link if the shortcut modifier is held. A plain
// click keeps starting a selection.
func (tab *terminal) openLink(m keys.Mods) bool {
	if !m.Shortcut() || !tab.frame.link.valid() {
		return false
	}
	openURL(tab.frame.link.uri)
	return true
}

// A URI in a link came out of the pty, so only the schemes a terminal is for
// are handed to the platform opener.
var openableSchemes = map[string]bool{"http": true, "https": true, "mailto": true, "file": true}

func openable(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && openableSchemes[parsed.Scheme]
}

// openURL hands a link to the platform. Failures are the platform's to
// report: a terminal that died because a link did not open would be worse.
func openURL(raw string) {
	if !openable(raw) {
		return
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", raw)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", raw)
	default:
		cmd = exec.Command("xdg-open", raw)
	}
	if err := cmd.Start(); err == nil {
		go func() { _ = cmd.Wait() }() // reap the opener rather than leave a zombie
	}
}

// Drag and drop -------------------------------------------------------------------
//
// OSC 72: a program registers the MIME types it will take and is then told
// about a drop it cannot see. A program that has not registered gets the old
// behaviour, its paths typed at the prompt; one that has gets the bytes.

// onDrag is the terminal's side of the conversation. None of it needs an
// answer here, so it is logged; a real emulator would set the pointer shape
// from Accepted and refuse a drop the program declined.
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

// handleDrop takes what the window system dropped on this tab. Ebitengine
// reports the drop and no hover before it, so the move and the drop are
// announced together, which is the least the protocol accepts.
func (tab *terminal) handleDrop() error {
	paths := dropPaths(tab.input.Dropped)
	if len(paths) == 0 {
		return nil
	}
	active, err := tab.stream.DragActive()
	if err != nil {
		return err
	}
	if !active {
		// No program asked, so the paths are typed at the prompt, quoted for
		// the shell. The trailing space leaves the cursor ready for more.
		text, err := tab.shell.QuotePaths(paths)
		if err != nil {
			log.Printf("drop: %v", err)
			return nil
		}
		return tab.pasteText(text + " ")
	}

	// Only what the program registered for and this window can make is
	// staged: anything else is noise or a promise that cannot be kept.
	registered, err := tab.stream.DragRegisteredMimes()
	if err != nil {
		return err
	}
	offered := offeredRepresentations(strings.Fields(registered))
	if len(offered) == 0 {
		log.Printf("drag: the program accepts %s, and this window has none of it", registered)
		return nil
	}
	px, py := tab.cursorPosition()
	col, row := tab.grid().cellAt(px, py)
	move := gostty.DragMove{
		CellX: uint32(col), CellY: uint32(row), PixelX: int32(px), PixelY: int32(py),
		Operations: gostty.DragOperations{Copy: true}, // the files are not moved anywhere
	}
	// The program does not receive the bytes: it is told a drop happened and
	// asks for the representation it wants, which the terminal answers from
	// what is staged here.
	if err := tab.stream.DragClearItems(); err != nil {
		return err
	}
	mimes := make([]string, 0, len(offered))
	for _, r := range offered {
		if err := tab.stream.DragAddItem(r.mime, r.body(paths)); err != nil {
			return err
		}
		mimes = append(mimes, r.mime)
	}
	if err := tab.stream.DragMove(move, strings.Join(mimes, " ")); err != nil {
		return err
	}
	if err := tab.stream.DragDrop(move); err != nil {
		return err
	}
	// The move and the drop are written as replies, like a status report.
	return tab.stream.WriteReplies(tab.shell.Pty)
}

// representation is one way this window can describe dropped paths. The list
// a program is told about and the bytes staged for it come from the same
// table, so the two cannot fall out of step.
type representation struct {
	mime string
	body func(paths []string) []byte
}

var representations = []representation{
	{"text/uri-list", func(paths []string) []byte { return []byte(uriList(paths)) }},
	{"text/plain", func(paths []string) []byte { return []byte(strings.Join(paths, "\n")) }},
}

// offeredRepresentations is the intersection of what this window can make
// and what the program registered for, in this window's order of preference.
func offeredRepresentations(registered []string) []representation {
	var offered []representation
	for _, r := range representations {
		if slices.Contains(registered, r.mime) {
			offered = append(offered, r)
		}
	}
	return offered
}

// dropPaths lists what was dropped, at the root of the virtual filesystem
// Ebitengine hands over. The names are real paths on the desktop platforms.
func dropPaths(dropped fs.FS) []string {
	if dropped == nil {
		return nil
	}
	entries, err := fs.ReadDir(dropped, ".")
	if err != nil || len(entries) == 0 {
		return nil
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		paths = append(paths, entry.Name())
	}
	return paths
}

// uriList is the paths as text/uri-list: CRLF-separated file URIs.
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

// Files -----------------------------------------------------------------------

// saveTemp writes a uniquely named file matching pattern in dir (the system
// temporary directory when empty). It returns the path only after writing
// and closing succeed, and removes incomplete output on failure.
func saveTemp(dir, pattern string, write func(io.Writer) error) (string, error) {
	file, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", fmt.Errorf("create export: %w", err)
	}
	name := file.Name()
	saved := false
	defer func() {
		if !saved {
			_ = file.Close()
			_ = os.Remove(name)
		}
	}()
	if err := write(file); err != nil {
		return "", fmt.Errorf("write export: %w", err)
	}
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("close export: %w", err)
	}
	saved = true
	return name, nil
}
