# **G**~~h~~**o**stty

Go bindings for [libghostty-vt](https://github.com/ghostty-org/ghostty), the terminal emulation core of [Ghostty](https://ghostty.org).

Parse terminal output, inspect cells and scrollback, and encode input for terminal
applications. gostty provides the terminal state and protocol handling; your
application supplies the PTY, window, and renderer.

![HyperCat Term demo](assets/demo.gif)

Try [HyperCat Term](examples/hypercat/README.md), the included GUI terminal emulator
with Kitty graphics and animated cats.

## Contents

- [Installation](#installation)
- [Quick start](#quick-start)
- [API guide](#api-guide)
- [Layout](#layout)
- [Example](#example)
- [Building](#building)
- [Configuration and frame metadata](#configuration-and-frame-metadata)
- [Concurrency](#concurrency)
- [Lifetimes](#lifetimes)

## Installation

Requires **Go 1.25+**, **cgo enabled**, and a **C compiler**. Native archives are
included for macOS, Linux, and Windows on amd64 and arm64.

From your Go module:

```sh
go get github.com/ironpark/gostty
```

Zig is only needed to regenerate bindings or rebuild native libraries; see
[Building](#building).

## Quick start

Save this as `main.go` in your module and run `go run .`:

```go
package main

import (
    "fmt"
    "log"

    "github.com/ironpark/gostty"
)

func main() {
    if err := run(); err != nil {
        log.Fatal(err)
    }
}

func run() error {
    term, err := gostty.NewTerminal(80, 24)
    if err != nil {
        return err
    }
    defer term.Close()

    stream, err := term.NewStream(0)
    if err != nil {
        return err
    }
    defer stream.Close() // Runs before term.Close(): children must close first.

    // Streams implement io.Writer and parse VT escape sequences.
    if _, err := fmt.Fprint(stream, "hello\r\n\x1b[31mworld\x1b[0m"); err != nil {
        return err
    }

    screen, err := term.PlainString()
    if err != nil {
        return err
    }
    fmt.Println(screen)
    return nil
}
```

The plain-text result contains `hello` and `world` on separate lines, with color
formatting removed. Use `RenderState` to read styled cells for a renderer.

When connecting a PTY, feed its output into the stream and use
`stream.WriteReplies(pty)` to send terminal responses back. Serialize access to
the terminal and its children; see [Concurrency](#concurrency) and
[Lifetimes](#lifetimes).

## API guide

### Events and terminal state

Anything the program asked _of you_ rather than of the screen — a bell, a
desktop notification, a progress report — queues up during the feed and is
drained afterwards.

```go
for event, err := range stream.EventValues() {
    if err != nil {
        break
    }
    if event.Kind == gostty.StreamEventDesktopNotification {
        notify(event.Title, event.Body)
    }
}
```

Colors are `0xRRGGBB` everywhere: `RenderCell`, the palette (`PaletteColors`),
and the dynamic background, foreground and cursor colors. DEC and ANSI modes
are readable and settable by name (`ModeEnabled`, `SetMode`), and a selection
is a value in screen coordinates (`Screen.Selection`, `Search.Matches`) that a
renderer converts with `Screen.ViewportTop`.

A terminal can be saved and restored: `Stream.WriteSnapshot` writes ghostty's
binary snapshot, unfinished escape sequence included, and `DecodeSnapshot`
reads one back into a `Snapshot` whose `RestoreInto` replaces an existing
terminal's contents. The format puts the active state first so a terminal is
drawable before its scrollback has been read, which `SnapshotDecoder` exposes:
`Ready` then `RestoreInto` gives a terminal to draw, and `Next` prepends the
scrollback a page at a time. Both satisfy `SnapshotSource`, so a function that
only has to restore -- `RestoreInto`, `Continuation`, `Close` -- takes the
interface and leaves the choice to its caller.

Enums that a consumer names in text -- keys, mouse buttons, cursor styles,
progress states -- implement `encoding.TextMarshaler` and `TextUnmarshaler` and have a
`Parse<Enum>` function, so a keybinding like `{"key":"enter","mod":"ctrl"}`
decodes straight into `input.Key` and `input.MouseButton`.

Input goes the other way: build a key event and encode it for whatever the
program on the pty currently expects. Key, mouse, focus, and paste encoding live
in `github.com/ironpark/gostty/input`, which is completely decoupled from the
terminal state.

```go
ev := input.KeyEvent{Key: input.KeyArrowUp}

var buf bytes.Buffer
input.EncodeKey(&buf, term, ev, "") // "\x1b[A", or "\x1bOA" under DECCKM
```

### System hooks

Two things libghostty-vt cannot do on its own in a library build live in
`github.com/ironpark/gostty/sys`: decoding PNG bytes, which a Kitty graphics
`f=100` transmission needs and is refused without, and drawing secure entropy.
Both are process-global and answered from inside the callback, the way a
clipboard request is:

```go
sys.OnPngDecodeRequest(func(data []byte) {
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return // no reply fails the transmission
	}
	rgba := toNRGBA(img)
	sys.ReplyPngImage(uint32(rgba.Rect.Dx()), uint32(rgba.Rect.Dy()), rgba.Pix)
})
```

With a decoder installed the image reaches `KittyImage` already decoded, as
`KittyFormatRgba`.

### Kitty graphics

`KittyImages` is the same kind of per-frame snapshot as `RenderState`, for
images rather than cells: one `Update` and one `Placements` hands over a flat
list of "this image, this part of it, at this cell, this many pixels wide",
already resolved against the viewport and sorted back to front.

Each placement carries a `Layer` — the protocol's `z` split into the three
bands a renderer draws in — so the frame is three passes over one slice:

```go
images.Update(term)
images.Placements(dst)

draw(dst, gostty.KittyLayerBelowBg)
drawCellBackgrounds()
draw(dst, gostty.KittyLayerBelowText)
drawText()
draw(dst, gostty.KittyLayerAboveText)
```

A placement with `Virtual` set is positioned by the cells that reference it
through unicode placeholders rather than by the cursor, so its `ViewportCol`
and `ViewportRow` mean nothing; a renderer that does not scan cells for
placeholders should skip those.

### Tracked cells

Every coordinate above names a cell by where it is now, and stops naming it
as soon as output pushes it into the scrollback. A `GridRef` names the cell
itself: ghostty's tracked pin, kept up to date by the page storage as pages
are added, reflowed and pruned. It is what a renderer keeps for the cell under
the mouse between frames, or an embedder keeps for a mark it wants to scroll
back to. Positions are asked for in whichever coordinate system is useful:
`PointTagActive`, `PointTagViewport`, `PointTagScreen` or `PointTagHistory`.

```go
ref, _ := term.NewGridRef(gostty.PointTagViewport, mouseX, mouseY)
defer ref.Close() // before term, like Search and Gesture

// ... output scrolls the row into the scrollback ...

if pt, ok, _ := ref.Point(gostty.PointTagScreen); ok {
	cell, _, _ := ref.Cell()          // a RenderCell, colors resolved
	uri, isLink, _ := ref.HyperlinkUri()
	scrollTo(pt.Y)
}
```

A full reset discards every page, and a scrollback limit prunes the oldest,
after which `HasValue` is false and the reads report nothing until `Set`
points the reference somewhere new. For a read that happens once, such as
what is under a click, `Terminal.CellAt` and `Terminal.HyperlinkAt` answer
the same questions without tracking anything.

### Clipboard

Clipboard write requests cannot wait for a drain: the program blocks until they
are answered, so they arrive as a callback running while `Feed` is still on the
stack, with the request to answer.

```go
stream.OnClipboardWriteRequest(func(req *gostty.ClipboardRequest) {
	n, _ := req.ContentCount()
	for i := uint(0); i < n; i++ {
		data, _ := req.ContentData(i)
		setSystemClipboard(data)
	}
	req.Allow(false)
})
```

A request that is not answered before the callback returns is denied, as is
any request arriving on a stream with no handler installed. Answering a read
allows the running program to read the user's clipboard, so ensure user consent
before calling `ReplyText`.

### Identity and the desktop

A program that asks what terminal it is talking to blocks until it is answered.
`CSI c`, `CSI > c`, `CSI = c` and XTVERSION are answered by every stream with no
setup: the conformance level and device type describe what the parser
implements, and the one part that varies — whether OSC 52 is served — the stream
already knows from whether a clipboard callback was installed. There is nothing
to configure before they are right.

Two things are worth saying anyway:

```go
// Otherwise XTVERSION names the parser, "libghostty", which is no use as an
// application identity. Two arguments because the reply is parsed as
// "name version".
stream.SetVersionReport("hypercat", "0.1.0")

// Answers CSI ? 996 n, and reports the change to a program that subscribed
// with mode 2031 -- both halves, because doing one without the other is a
// silent bug. The report leaves with the next WriteReplies.
stream.ColorSchemeChanged(gostty.ColorSchemeDark)
```

ENQ (`0x05`) answers nothing unless `SetEnquiryResponse` gives it something. An
answerback string is echoed to any program that sends one control byte, so it
leaks whatever it holds; terminals leave it empty and so does this.

### Drag and drop

Kitty's OSC 72 is the one part of the binding where the embedder is not
draining what a program did but driving a conversation with it. A program
registers to accept drops, the terminal tells it where a native drag is and
what could be handed over, and on drop the terminal holds the data until the
program has read it.

```go
stream.OnDrag(func(n gostty.DragNotice, mimes string) {
	switch n.Event {
	case gostty.DragEventRegistration:
		// mimes is empty once the program has unregistered.
		window.AcceptDrops(strings.Fields(mimes))
	case gostty.DragEventAcceptance:
		window.SetDragFeedback(n.Answered, n.Accepted)
	}
})

// The native half. Every call is a no-op while no program is registered,
// which is the normal state.
stream.DragMove(gostty.DragMove{CellX: 3, CellY: 1, Operations: ops}, "text/uri-list text/plain")
stream.DragAddItem("text/uri-list", uris)   // staged, in the order offered
stream.DragDrop(gostty.DragMove{ /* ... */ })
```

The event and the program's answer are captured when the effect fires and
passed as arguments, so a second event in the same feed cannot overwrite the
first one's answer.

### Parsers without a terminal

A program that only wants to read one kind of sequence has no terminal to feed.
`OSCParser` and the SGR functions are ghostty's two parsers on their own: bytes
in, what the sequence said out.

```go
p, _ := gostty.NewOSCParser()
defer p.Close()

p.Feed([]byte("8;id=42;https://example.com/")) // between ESC ] and the terminator
if ok, _ := p.End(gostty.OSCTerminatorSt); ok {
	kind, _ := p.Command() // OSCCommandHyperlinkStart
	uri, _ := p.HyperlinkUri()
	id, _ := p.HyperlinkID()
}
```

Every string payload has its own accessor -- `WindowTitle`, `Pwd`,
`NotificationBody`, `ClipboardData`, `MouseShape` and the rest -- and `Command`
covers the whole OSC set, so a command with no accessor is still identifiable.
The strings stay valid until the next `Feed`, `End` or `Reset`.

SGR is a parameter list rather than a byte stream: the parameters of a
`CSI ... m` with the `m` dropped, plus a mask saying which of them were followed
by a colon rather than a semicolon, since `4;3` is underline then italic while
`4:3` is a curly underline.

```go
params := []uint16{38, 2, 0, 255, 136, 0} // CSI 38:2::255:136:0 m
n, _ := gostty.SgrAttributeCount(params, 0b011111)
for i := uint(0); i < n; i++ {
	attr, _ := gostty.SgrAttributeAt(params, 0b011111, i)
	if rgb, ok := attr.AsDirectColorFg(); ok {
		// rgb is 0xFF8800
	}
	term.SetAttribute(attr)
}
```

An attribute comes back as the same `Attribute` that `SetAttribute` takes, so
what the parser saw applies as it is. It is indexed rather than filled into a
slice because a tagged union cannot be a slice element across the boundary.

### Unimplemented sequences

`SetUnknownMaxBytes` captures the sequences this library does not implement —
only APC today — and reports them as `StreamEventUnknownSequence` with the
content on `EventSequence`. Off by default, since the capture buffer is per
stream and most programs never send one. It is how a program speaking a
graphics protocol you never wired up says so, instead of drawing nothing.

## Layout

```text
.
├── gostty_*_gen.go       # Public functions, handles, enums, structs, errors
├── zigo_must_gen.go      # Panic wrappers behind the Must* variants
├── *_test.go             # The module's only hand-written Go files
├── input/                # Key, mouse, focus, and paste encoding package
├── sys/                  # Process-global hooks: PNG decoder, secure entropy
├── internal/
│   ├── lifecycle/        # Handle and error contract shared by cgo and purego
│   └── raw/              # Unsupported internal cgo call layer
├── libs/                 # Prebuilt native archives, one <goos>_<goarch>/ per platform
├── zig/
│   ├── build.zig         # Ghostty dependency, zigo wiring, platform matrix
│   ├── src/
│   │   ├── bindings.zig  # API assembly; domain declarations in bindings/
│   │   └── root.zig      # Declarations Ghostty does not provide
│   └── zigo/
│       ├── semantic.json     # ABI description consumed by abi-diff
│       └── errors.lock.json  # Stable numeric codes for Zig errors
├── examples/
│   └── hypercat/         # GUI terminal emulator; its own module, see go.work
├── go.work               # Builds the example against this checkout
└── Makefile              # Toolchain entry point; `make help` lists targets
```

`zig/src/bindings/` groups handles, methods, enums and value types by domain:
terminal, stream, rendering, input and system. `policy.zig` holds shared byte,
ownership and enum policies; `bindings.zig` assembles the public API.

The public Go bindings are generated from the Zig declarations. Everything named
`*_gen.go`, along with
`internal/lifecycle/` and `zig/zigo/`, is produced by `make generate` and should
not be edited manually. Unexported generator helpers (`newTerminal`, `newStream`,
`newKeyEvent`, etc.) share the package namespace as well.

### What `root.zig` is for

Most of libghostty-vt is bound directly: `Terminal`, `Screen`, `RenderState`
and `Snapshot` are ghostty's own types and their methods become Go methods with
nothing in between, including their `init`/`deinit` lifecycles,
while `.fields` and `.flatten` in `bindings.zig` cover plain field reads and
`Terminal.init`. A field accessor reaches optionals and slices as well as
scalars, so the whole of `OSCParser`'s payload surface is paths rather than
fifteen bodies that would each return one field. What is left in `root.zig` is the shapes that cannot cross a C
ABI directly:

- an `std.Io` value, because ghostty ships `TinyIo` as a type rather than a
  ready-made declaration;
- `Stream`, which owns the event queue and the clipboard callbacks that
  ghostty's handler reaches back for through `@fieldParentPtr`;
- wrappers for calls that return a value holding page pins — `SelectWord`,
  `SelectLine`, `SelectOutput` — which apply the selection instead of handing it
  back;
- `GridRef`, a tracked pin held beside the screen it was tracked by, so that
  a reset that replaces the screens is noticed before the pin is touched;
- `Search`, ghostty's terminal-wide searcher with the terminal held beside it:
  nearly every call on it takes the terminal back, including its own `deinit`,
  and its matches are page pins that come out as screen coordinates;
- flat mirrors for the two things with no C representation: `Attribute`,
  ghostty's SGR union, and `RenderCell`, one viewport cell with its colors
  already resolved.

## Example

`examples/hypercat/` is a full GUI terminal emulator — shell on a pty, rendered in an
Ebitengine window — built on these bindings. See [examples/hypercat/README.md](examples/hypercat/README.md).

```bash
make example
```

## Building

Requires Zig 0.16, Go 1.25+, and a C compiler. `make` drives both toolchains;
`make help` lists every target.

```sh
git clone https://github.com/ironpark/gostty.git
cd gostty
make generate
```

Zig fetches the pinned Ghostty and [zigo](https://github.com/ironpark/zigo)
dependencies from `zig/build.zig.zon`; a sibling zigo checkout is not required.
The first build needs network access to download dependencies.

```bash
make generate   # regenerate Go and build the native archives for every platform
make build      # same as generate
make test       # build, then run the Go tests
make example    # run the GUI terminal emulator
make bench      # measure the boundary and the parser
make coverage   # report which public Zig declarations are bound
```

The native libraries are built into `libs/<goos>_<goarch>/` using `ReleaseSafe`
optimization by default. The platform matrix is declared once in `zig/build.zig`
and passed to zigo as `targets`, so a single `zig build go` generates the Go tree
and builds every platform's archives; `internal/raw/zigo_link_inputs_gen.go`
carries one `#cgo <goos>,<goarch>` line per platform. Prebuilt archives are
tracked in git for macOS, Linux, and Windows on amd64 and arm64, so consumers
only need Go and a C compiler to build the package. Zig is required when
changing the bindings or rebuilding the native libraries. After updating zigo or
the Zig sources, run `make generate` to refresh all six platforms, then
`make check` to check that the generated bindings match the sources.

## Configuration and frame metadata

`NewTerminalWithConfig(TerminalConfig{Cols: 80, Rows: 24, ...})` adds optional
scrollback limits, default colors, cursor blinking and mode defaults. Nil fields
keep native defaults; a pointer to zero or false explicitly sets that value.
`term.NewStreamWithConfig(StreamConfig{...})` configures continuation tracking,
unknown-sequence capture, application identity, color scheme and clipboard
handlers. Failed creation closes the partially configured handle.

`RenderState.Cursor()` and `Colors()` each return a copied value with one native
call. Cursor coordinates are meaningful only when `ViewportHasValue` is true;
`Visible` separately reflects the terminal's cursor visibility mode. The cursor
color is meaningful only when `CursorHasValue` is true. Existing individual
getters and cell/row batch APIs remain available.

`Stream.EventValues()` returns owned payloads safe to retain after the next feed
or stream close. Title and working-directory events contain their values at the
time of the change, even if a single feed changes them multiple times. This
iterator shares consumption with `Events()` and `NextEvent()`.

Clipboard reads can answer multiple MIME representations atomically with
`req.Reply([]ClipboardContent{{MIME: "text/plain", Data: text}}, false)`.
`AddContent` and `ReplyContents` expose staging separately; staged data alone is
never permission to read the clipboard, and is discarded when the callback
returns. `ReplyText` remains the plain-text convenience method.

`Stream.AtGround()` reports whether the parser is between codepoints and VT
sequences. `FeedUntilGround(data)` returns `FeedBoundary{Consumed, Reached}`:
if already at ground it consumes zero; otherwise it consumes only through the
first boundary, or all input with `Reached == false` if none is reached. Feed the
remaining bytes separately after inserting any out-of-band VT data.

`GetBuildInfo()` reports the Ghostty source revision, zigo version and optimization
mode, SIMD, Kitty graphics and tmux feature flags used when generating the bundled
archives. It describes the build, not
runtime hooks such as an installed PNG decoder.

## Concurrency

Serialize all operations on a terminal and its streams, screens, tracked
references, searches and gestures, including getters and Close. The internal
mutex protects handle lifetime; it does not serialize native terminal operations.
Configuration constructors and event-value reads also require this exclusive
access for the duration of the whole call.

Callbacks run synchronously on the calling thread. During a clipboard callback,
read or answer the supplied request; do not recursively Feed, resize, reset or
close the same terminal or its children. Drag callbacks should capture the
notice and schedule terminal mutations after the active call returns. Generated
"reentrancy: allowed" describes the binding transport needed for request replies,
not permission to recursively mutate terminal state. Process-global sys hooks
must be installed before terminal use; they may invoke only their reply APIs
while handling a request.

`RenderState.Update` needs exclusive terminal access and exclusive access to the
render state. After it returns, the render snapshot can be read independently of
terminal mutations. Serialize reads with Update, Clean and Close on that render
state. Copied Cursor, Colors and cell values can be retained across updates.

## Lifetimes

- Every handle has an idempotent `Close`. Calls after `Close` return
  `ErrInvalidHandle` rather than touching freed memory.
- **A `Stream` must be closed before the `Terminal` it feeds.** ghostty's
  handler reaches through the terminal for its allocator when it tears down.
  The binding declares the stream a child of its terminal, so closing the
  terminal first returns `ErrHandleInUse` instead of corrupting memory.

## License

MIT for this repository's own code. libghostty-vt is MIT, and Ghostty's own terms
apply to it.
