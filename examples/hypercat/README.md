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

## Reading the example

The command is one Go package, split by domain. Everything a gostty user
came to see -- feeding the stream, reading the render state, encoding input,
answering the OSC side channels -- is in the root; the supporting packages
(`fonts`, `keys`, `shell`, `thecat`, `ui`) own fonts, platform keyboard
state, the pty, the cat and the widgets, and know nothing about the terminal.
Read the root in this order:

1. [`main.go`](main.go): install the PNG decoder, open the window, start the
   first tab, and hand the window to Ebitengine.
2. [`terminal.go`](terminal.go): one tab's terminal, in execution order.
   `start` builds a `gostty.Session` (terminal, stream, gesture) and the
   shell; `readOutput` is `Stream.Feed`, event dispatch and
   `Stream.WriteReplies` together; `refreshSnapshot` reads what the next draw
   needs; `resize` keeps the terminal and the pty the same size. The OSC
   events and the theme's palette are here too.
3. [`input.go`](input.go): what the host reports each frame (`hostInput`),
   and what the terminal does with it: host shortcuts are taken out of the
   key stream, the rest goes through `input.EncodeKey`, `EncodePaste`,
   `EncodeMouse` and `EncodeFocus`. The encoders read the terminal's modes,
   so whether the mouse is the program's or the selection's is their answer.
4. [`frame.go`](frame.go): `RenderState` → cells, dirty rows, colours,
   cursor and grapheme clusters, plus the `grid` that turns cells into pixels
   everywhere.
5. [`draw.go`](draw.go): those cells → pixels. Two GPU layers so Kitty
   images can sit between backgrounds and text, repainted a dirty row at a
   time.
6. [`window.go`](window.go): the Ebitengine game. `update` services every
   tab, routes input (tab bar and shortcuts → panels → terminal), and takes
   the snapshot; `Draw` composes the active tab, its panels and the tab bar.
   Tabs, the window title and the cat live here; [`settings.go`](settings.go)
   holds the font, theme and cat mode every tab shares and fans a change out.

```text
shell --pty--> Stream.Feed --> Terminal --> RenderState --> frame --> draw
shell <--pty-- input.EncodeKey <-- keys.Reader <-- hostInput <-- Ebitengine
```

The shell's reader goroutine only queues bytes; Ebitengine serialises `update`
and `Draw`, so every gostty call happens on the window loop. `Draw` never
reads a native handle: it paints what `update` read into the frame, which is
why a frame can be drawn without an update in between. Background tabs keep
parsing output and sending replies.

### Optional features

| Concern | File |
| --- | --- |
| Selection through `Gesture`: click counts, words and output, block drags, autoscroll; the viewport and scrollbar; HTML export through the formatter | [`selection.go`](selection.go) |
| Scrollback search, driven a slice a frame, with viewport highlights | [`search.go`](search.go) |
| The clipboard and OSC 52, OSC 8 hyperlinks, OSC 72 drag and drop, temporary files | [`desktop.go`](desktop.go) |
| Kitty graphics: placements, textures by generation, raw and PNG decoding | [`kitty.go`](kitty.go) |
| Tab lifecycle, shortcuts, tab bar, window title, the cat's grid | [`window.go`](window.go) |
| Shared font, theme and cat settings, and the settings panel's labels | [`settings.go`](settings.go) |
| Panels, tab bar, themes | [`ui/`](ui/) |
| Platform keyboard state and repeats | [`keys/`](keys/) |
| Font discovery, loading, and emoji | [`fonts/`](fonts/) |
| PTY and shell process lifecycle | [`shell/`](shell/) |
| The animated cat | [`thecat/`](thecat/) |

`shell/` handles the platform differences and stops, terminates, and reaps the
shell on close, even when its output queue is full. `internal/shelltest` runs
the test binary as the shell for the root and `shell` tests.

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

Each tab has its own shell, scrollback, selection, and search. One cat belongs
to the window and keeps its position and animation across tab switches,
following the active tab's grid. Background tabs continue processing output
and terminal replies. The font, theme, and cat
settings belong to the window, so a change made from any tab's settings panel
applies to every tab, including ones opened later. The clipboard is shared
across tabs. When a shell exits, its tab closes;
closing the last tab exits the window. If there are more tabs than fit in the
bar, it follows the active tab; use the wheel or keyboard shortcuts to reach
hidden tabs.

Everything else is sent to the shell. In search, use Enter or Shift+Enter to
move between matches and Escape to close.

## Kitty graphics

From `examples/hypercat`, run these commands inside HyperCat Term, Ghostty, or Kitty:

```sh
./scripts/kittydemo.py
./scripts/kittydemo.py --cells 20x10
./scripts/kittydemo.py --rgba
./scripts/kittydemo.py --png
./scripts/kittydemo.py --z -1
./scripts/kittydemo.py --query
```

The demo sends raw RGB and RGBA by default and a PNG with `--png`. Library
builds of libghostty-vt carry no PNG decoder, so the example installs Go's
`image/png` through `gostty/sys` at startup; without that the terminal refuses
`f=100` transmissions. Use `--reply` to request and inspect terminal replies.

## Fonts and the cat

Use settings to change the font, size, and theme (terminal, midnight,
Catppuccin, or Solarized). HyperCat discovers system fonts, prefers JetBrains
Mono, and falls back to a bundled bitmap font.

Override font paths with `GOSTTY_FONT` (normal text), `GOSTTY_FONT_CJK` (wide
characters), or `GOSTTY_FONT_EMOJI` (colour emoji). Windows renders emoji through
the text font. The bundled bitmap fallback also supports size changes, in whole
scale steps.

The window's single cat walks on the active terminal's cell grid and follows
its output and scrollback. Opening or closing tabs does not create a new cat.
In settings, choose `off`, `on`, or `hyper`. Hyper mode adds faster movement,
infinite stamina, and visual effects.

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

Dropped-path quoting follows the shell executable used to start the tab:
POSIX quoting by default, double quotes for `cmd.exe`, and literal strings for
PowerShell. Starting a different shell inside a tab does not change that choice.
Paths containing line breaks or NUL are rejected; `cmd.exe` also rejects paths
with quotes, `%`, or `!`, whose meaning depends on expansion state. A rejected
drop is logged without pasting a partial path list or closing the window.

The example is its own Go module, so its UI, PTY, and clipboard dependencies
stay out of the bindings' `go.mod`. The repository's `go.work` builds it against
the checkout; its own `go.mod` pins a published gostty so that

```sh
go install github.com/ironpark/gostty/examples/hypercat@latest
```

works outside the repository. After pushing bindings the example needs, run
`make example-sync` and commit the updated pin in `go.mod`, `go.sum` and
`go.work`.
