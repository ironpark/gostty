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

The root contains the gostty integration. Ebitengine input polling, window
callbacks, and drawing live in [`internal/frontend/`](internal/frontend/).
Start with these files:

1. [`main.go`](main.go): install the PNG decoder, create the window and first
   terminal, and call `frontend.Run`. Deferred cleanup closes all terminals.
2. [`terminal.go`](terminal.go): one terminal's resources and lifecycle, in
   execution order: `start` → `readOutput` → `refreshSnapshot` → `resize` →
   `close`. `readOutput` shows `Stream.Feed`, event dispatch and
   `Stream.WriteReplies` together. `refreshSnapshot` collects the cells and
   overlays that the next draw consumes. `resize` updates both terminal and PTY.
3. [`terminal_input.go`](terminal_input.go): route terminal input, filter host
   shortcuts, encode keys with `input.EncodeKey`, and paste with
   `input.EncodePaste`. Window input priority is in
   [`window_input.go`](window_input.go): tabs → panels → terminal.
4. [`render_frame.go`](render_frame.go): read cells, dirty rows, colors, cursor,
   and grapheme clusters from `RenderState` during updates.
5. [`window.go`](window.go) and [`presentation.go`](presentation.go): implement
   the frontend contract. `Update` services shells and routes supplied input;
   `Resize` keeps terminal and PTY geometry in sync; `Present` supplies cached
   data for drawing without reading native handles.

```text
internal/frontend                    root (gostty integration)
  Ebitengine.Update → Input ────────→ window.Update
                                       ├─ PTY → Feed → events → replies
                                       ├─ input → EncodeKey/Mouse/Paste → PTY
                                       └─ RenderState → cached snapshot
  Ebitengine.Draw   ← Presentation ← window.Present
  Ebitengine.Layout ───────────────→ window.Resize → terminal + PTY
```

The frontend drives three methods: `Update(Input)`, `Resize(width, height,
scale)`, and `Present() Presentation`. No Ebitengine types cross this contract.
The frontend owns the window and GPU drawing; the root owns native terminal
handles and protocol decisions. Its `window` model manages the tab list and
shared settings; each `terminal` owns its shell, snapshot, selection, and search.

`Presentation` borrows cached cells, clusters, and overlays until the next
update. Drawing consumes pending repaint marks but performs no native terminal
reads. Keyboard events are collected only after tab and panel routing decide
who owns typing and how long the active terminal has had focus.

The shell's reader goroutine only queues bytes. The frontend serializes model
callbacks, so terminal access stays in the window loop. Background terminals
continue parsing output and sending replies.

### Optional features and host plumbing

These parts support the full demo but can be skipped on the first pass through
the feed → encode → snapshot flow:

| Concern | Files or package |
| --- | --- |
| Ebitengine window, input collection, and rendering | [`internal/frontend/`](internal/frontend/) |
| Tab lifecycle, shortcuts, and tab bar | [`window_tabs.go`](window_tabs.go), [`ui/tabbar.go`](ui/tabbar.go) |
| Search/settings panels | [`window_panels.go`](window_panels.go), [`ui/`](ui/) |
| OSC events and clipboard callbacks | [`terminal_events.go`](terminal_events.go) |
| Shared system clipboard | [`window_clipboard.go`](window_clipboard.go) |
| Mouse and focus reporting | [`terminal_mouse.go`](terminal_mouse.go) |
| Selection, scrollback, and search | [`terminal_selection.go`](terminal_selection.go), [`terminal_scroll.go`](terminal_scroll.go), [`terminal_search.go`](terminal_search.go) |
| Hyperlinks and file drops | [`terminal_hyperlink.go`](terminal_hyperlink.go), [`terminal_drop.go`](terminal_drop.go) |
| Kitty image snapshots, textures, and PNG decoding | [`internal/graphics/`](internal/graphics/) |
| Shared appearance and per-terminal palette | [`window_settings.go`](window_settings.go), [`terminal_style.go`](terminal_style.go) |
| Grid coordinates and content sizing | [`terminal_geometry.go`](terminal_geometry.go) |
| Platform keyboard state and repeats | [`keys/`](keys/) |
| Font discovery, loading, and emoji | [`fonts/`](fonts/) |
| PTY and shell process lifecycle | [`shell/`](shell/) |
| Animated cat and its connection to the cell grid | [`terminal_cat.go`](terminal_cat.go), [`thecat/`](thecat/) |

`internal/graphics` depends on gostty and Ebitengine, with no dependency on the
command. Its `Cache` borrows a terminal: the tab creates it at startup and calls
`Refresh` during updates; the frontend draws its cached layers. The tab closes
it before the terminal. `png.go` handles the process-wide decoder callback; `pixels.go`
converts raw image samples.

`shell/` handles platform differences and stops, terminates, and reaps the shell
on close, even when its output queue is full. The existing `fonts`, `keys`,
`shell`, `thecat`, and `ui` packages retain their import paths. These packages
handle supporting work without depending on the command's window or terminal
types. `internal/frontend` and `internal/graphics` are private to this example.

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

Use settings to change the font, size, and theme (terminal, midnight,
Catppuccin, or Solarized). HyperCat discovers system fonts, prefers JetBrains
Mono, and falls back to a bundled bitmap font.

Override font paths with `GOSTTY_FONT` (normal text), `GOSTTY_FONT_CJK` (wide
characters), or `GOSTTY_FONT_EMOJI` (colour emoji). Windows renders emoji through
the text font.

The cat walks on the terminal's cell grid and follows output and scrollback.
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

The example is its own Go module, so its UI, PTY, and clipboard dependencies
stay out of the bindings' `go.mod`. The repository's `go.work` builds it against
the checkout; its own `go.mod` pins a published gostty so that

```sh
go install github.com/ironpark/gostty/examples/hypercat@latest
```

works outside the repository. After pushing bindings the example needs, run
`make example-sync` and commit the updated pin in `go.mod`, `go.sum` and
`go.work`.
