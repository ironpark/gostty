# HyperCat Term

A small GUI terminal emulator built with [gostty](../../) and
[Ebitengine](https://ebitengine.org).

```sh
make example # run from the repository root
```

This example demonstrates:

- parsing PTY output with `Stream.Feed`
- rendering cells, grapheme clusters, and Kitty graphics from frame snapshots
- mode-aware keyboard, mouse, focus, and paste encoding, including the Kitty
  keyboard protocol's repeats, releases, and unshifted codepoints
- scrollback with a scrollbar, search, and clipboard integration
- themes that carry the ANSI palette, and scrollback exported through the
  formatter
- selection through `Gesture`: click counts, word and output granularity,
  rectangular drags, and autoscroll
- OSC 8 hyperlinks and OSC 72 drag and drop
- terminal replies and OSC side effects such as title, bell, and progress

```text
shell --pty--> Stream.Feed --> Terminal --> RenderState --> Ebitengine
shell <--pty-- input.EncodeKey <-- KeyEvent <-- keys.Reader <-- Ebitengine
```

## Reading the example

Follow the data through these files:

1. [`main.go`](main.go): register the PNG decoder, open the first tab, and run
   Ebitengine. [`tab.go`](tab.go) lists the resources owned by a tab.
2. [`terminal.go`](terminal.go): `start` creates the terminal, stream, render
   state, and shell. `readOutput` feeds PTY bytes with `Stream.Feed`, dispatches
   events, and sends `Stream.WriteReplies` back to the shell. `close` releases
   resources, including on startup failure.
3. [`app.go`](app.go): `Update` services every shell, routes input to the active
   tab, refreshes its snapshot, and updates the cat. `Draw` displays the snapshot.
   Background tabs continue parsing output and returning replies.
4. [`input.go`](input.go): `handleInput` filters host shortcuts, `sendKey` passes
   full key events to `input.EncodeKey`, and `pasteText` uses `input.EncodePaste`.
   [`report.go`](report.go) demonstrates mouse and focus encoding.
5. [`viewport.go`](viewport.go) → [`frame.go`](frame.go) → [`render.go`](render.go):
   `frame.read` captures cells, dirty rows, colors, cursor, and grapheme clusters
   from `RenderState`. The viewport adds search highlights, graphics, and
   overlays. `Draw` uses saved data; native reads stay in `Update`, where errors
   can be returned. Only terminal-rewritten rows need their clusters read again.
6. [`layout.go`](layout.go): resize the terminal and PTY together when the grid
   or font changes.

The core gostty calls stay in these files rather than behind a separate terminal
wrapper. Each `terminalTab` owns its terminal, stream, render state, and shell;
`terminalApp` shares fonts, theme, and clipboard across tabs. A tab returns UI
actions without holding a reference back to the app.

### Optional features and host plumbing

These files support the full demo but can be skipped on the first pass through
the feed → encode → snapshot flow:

| Concern | Files |
| --- | --- |
| Tab lifecycle, shortcuts, and tab bar | `tabs.go`, `ui/tabbar.go` |
| Search/settings panel input and drawing | `panels.go`, `ui/` |
| OSC title, bell, progress, and notifications | `events.go` |
| System clipboard and OSC 52 callbacks | `clipboard.go` |
| Selection, scrollback, and search | `mouse.go`, `viewport.go`, `search.go` |
| Hyperlinks, file drops, and Kitty graphics | `hyperlink.go`, `dnd.go`, `kitty.go` |
| Shared appearance | `settings.go` |
| Platform keyboard state and repeats | `keys/` |
| Font discovery, loading, and emoji | `fonts/` |
| PTY and shell process lifecycle | `shell/` |
| Animated cat and its connection to the cell grid | `companion.go`, `thecat/` |

`shell/` handles platform differences and stops, terminates, and reaps the shell
on close, even when its output queue is full.

Run the example's regression tests from the repository root:

```sh
go test ./examples/hypercat/...
```

## Controls

| Input | Action |
| --- | --- |
| Ctrl+Shift+T / Cmd+T | Open a new tab. |
| Ctrl+Shift+W / Cmd+W | Close the active tab. |
| Ctrl+Tab / Ctrl+Shift+Tab | Next / previous tab. |
| Ctrl+Shift+1–8 / Cmd+1–8 | Select a numbered tab. |
| Ctrl+Shift+9 / Cmd+9 | Select the last tab. |
| Tab bar | Click a tab to select it, × to close, or + to add; wheel to switch. |
| Wheel | Scroll; sends arrow keys on the alternate screen. |
| Shift+wheel | Scroll while the application owns the mouse. |
| Drag | Select; hold Alt for block selection; drag past the edge to autoscroll. |
| Double / triple click | Select the word / the command output around it. |
| Ctrl+Shift+click / Cmd+click | Open the OSC 8 link under the pointer. |
| Drop files | Hand them to a program that registered for drops (OSC 72), or type their paths. |
| Shift+drag | Select while the application owns the mouse. |
| Ctrl+Shift+C / Cmd+C | Copy. |
| Ctrl+Shift+V / Cmd+V | Paste. |
| Ctrl+Shift+A / Cmd+A | Select the whole scrollback. |
| Ctrl+Shift+S / Cmd+S | Save the selection, or the scrollback, as HTML. |
| Ctrl+Shift+arrows / Cmd+arrows | Move the end of the selection; Home, End, PageUp and PageDown too. |
| Ctrl+Shift+F / Cmd+F | Search. |
| Ctrl+Shift+, / Cmd+, | Open settings. |
| Click the cat | Interact with the cat and open settings. |

Each tab has its own shell, scrollback, selection, search, and cat. Background
tabs continue processing output and terminal replies. The font, theme, and cat
settings belong to the window, so a change made from any tab's settings panel
applies to every tab, including ones opened later. The clipboard is shared
across tabs. When a shell exits, its tab closes;
closing the last tab exits the window. If there are more tabs than fit in the
bar, it follows the active tab; use the wheel or keyboard shortcuts to reach
hidden tabs.

Everything else is sent to the shell. In search, use Enter or Shift+Enter to
move between matches and Escape to close.

## Kitty graphics

Run these commands inside HyperCat Term, Ghostty, or Kitty:

```sh
./kittydemo.py
./kittydemo.py --cells 20x10
./kittydemo.py --rgba
./kittydemo.py --png
./kittydemo.py --z -1
./kittydemo.py --query
```

The demo sends raw RGB and RGBA by default and a PNG with `--png`. Library
builds of libghostty-vt carry no PNG decoder, so the example installs Go's
`image/png` through `gostty/sys` at startup; without that the terminal refuses
`f=100` transmissions. Use `--reply` to request and inspect terminal replies.

## Fonts and the cat

The settings panel includes terminal, midnight, Catppuccin, and Solarized
themes. A theme carries the sixteen ANSI colours as well as the default
foreground and background, so text a program coloured by name follows it;
they are set as the terminal's palette defaults, which is also why changing a
theme rebuilds the render state -- a palette change dirties no row, and cell
colours are resolved as the state is read. The example discovers system fonts and prefers JetBrains Mono when
available.
Set `GOSTTY_FONT` and `GOSTTY_FONT_CJK` to override the narrow and wide font
paths. A bundled bitmap font is used as a final fallback. Font and size changes
are available in settings.

Colour emoji are drawn from the emoji font's own bitmaps (`sbix` on macOS,
`CBDT` elsewhere) rather than through the text renderer, which rasterises
outlines and would draw them blank. Only two-column cells take that path, which
is the rule the terminal laid the line out with. Set `GOSTTY_FONT_EMOJI` to
override the search. Windows keeps its text face, since Segoe UI Emoji is a
layered vector font rather than a bitmap one.

Emoji written as several codepoints -- flags, ZWJ sequences, skin tones -- are
one cell and one picture. `RenderCell.Codepoint` carries the base of the
cluster, so the rest is read back with `RenderState.Graphemes`, for the rows
about to be redrawn; the tab turns on grapheme clustering (mode 2027) as a
default so the terminal puts the whole cluster in one two-column cell rather
than counting codepoints. The same read is what puts a combining accent on the
letter it belongs to. The picture for a cluster is found by shaping it with the
emoji font, since the substitution that turns two regional indicators into a
flag lives in the font.

The cat uses `thecat.GridWorld` to treat the rendered cell grid as terrain, so it follows terminal output
and scrollback without maintaining a separate world model. It can be disabled
in settings. Hyper Cat mode adds infinite stamina, faster movement, a pulsing
aura, rainbow particles, and motion afterimages. The cat setting cycles through
`off`, `on`, and `hyper`.

Sprites are by **Jump Button**
([Bluesky](https://bsky.app/profile/jumpbutton.bsky.social),
[X](https://twitter.com/Jump_Button)); see `thecat/cat/Read_me.txt` for terms.

## Limitations

This is a binding demo, not a production terminal. It has no preedit display,
font fallback chain, ligatures, splits, or persistent settings. OSC 52
clipboard reads are accepted without confirmation, which is unsafe for a
general-purpose terminal.

The Kitty keyboard protocol is described as fully as Ebitengine allows.
Ebitengine reports a typed character and the key that produced it separately,
so they are paired again when exactly one of each arrives in a frame; an IME
committing a phrase, or a dead key resolving a frame later, is text with no key
behind it and says so. `Composing` is never set, for the same reason there is
no preedit display.

The two halves that follow the window system rather than the terminal are
thinner than a real emulator's. A drag is announced to a program only when the
files are dropped, because Ebitengine reports the drop and no hover before it,
so `Stream.DragLeave` and the operation the program answered with are logged
rather than acted on. A hyperlink that soft-wraps is underlined a row at a time,
since the hovered row is the only one scanned; what opens is the whole URI
either way.

The example is its own Go module, so its UI, PTY, and clipboard dependencies
stay out of the bindings' `go.mod`. The repository's `go.work` builds it against
the checkout; its own `go.mod` pins a published gostty so that

```sh
go install github.com/ironpark/gostty/examples/hypercat@latest
```

works outside the repository. After pushing bindings the example needs, run
`make example-sync` and commit the updated pin in `go.mod`, `go.sum` and
`go.work`.
