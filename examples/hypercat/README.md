# HyperCat Term

A small GUI terminal emulator built with [gostty](../) and
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

Five subpackages keep the parts that are not about the bindings out of the way.
`keys` reads a frame of keyboard state -- Ebitengine's key set, the macOS call
that says whether a key is really down, and the repeat policy an emulator has
to invent -- and reports which terminal keys were pressed, repeated and
released; `input.go` is what hypercat then does with them. The two key sets are
not mapped by hand: both spell keys as W3C codes, so `input.KeyFromW3C` names an
Ebitengine key and `Key.Printable` says whether the platform reports it as text
or it has to be described to the encoder. `thecat` owns the cat, pointer interaction,
visual effects, and a grid terrain adapter that has no terminal dependency.
`fonts` discovers and loads
font families, derives cell metrics, supplies the bitmap fallback, and caches
colour emoji glyphs. It has no dependency on terminal or tab state. `ui` owns
the search bar, settings panel, tab bar, and themes. Its components consume
input snapshots and return actions; drawing uses a host-supplied text renderer
and cell metrics, without a terminal, shell, font loader, or cat dependency.
`shell` runs the user's shell on a pseudo-terminal and reads what it writes: it
is the half of an emulator that has nothing to do with terminal emulation, and
it is where every platform difference lives, since a POSIX pty and a Windows
pseudoconsole disagree about who owns the far end and about what ends a read
once the program has gone. `internal/shelltest` starts a test binary as that
shell, which is how both the session tests and the tab tests get a child that
writes something and then does as it is told.

Dependencies in the main package run one way. `terminalApp` owns the window and
what every tab shares -- the fonts, the theme, the cat mode, the clipboard --
and reaches down into the tabs; a `terminalTab` holds no reference back. What a
tab cannot decide alone it reports and the window applies: a settings step comes
back as a `ui.Actions` field, and the window title is set from the visible tab
rather than by each tab in turn. The window says what happened -- `activate`,
`deactivate`, `themeChanged`, `fontsChanged` -- and each tab decides what that
means to its own state, so `appearance` keeps one writer and many readers.

The files are organized around the terminal's frame and resource lifecycle:

- `main.go` sets up the app's shared state, the clipboard, and the window.
- `app.go` owns the window, the tab lifecycle, and the state tabs share; it
  drives `ui.TabBar` and the panels, and applies what a tab asks of the window.
- `tab.go` owns each terminal tab and services its output and input.
- `terminal.go` creates and releases terminal resources, configures the stream,
  and starts the tab's `shell.Session`.
- `events.go` handles terminal events such as title, bell, and progress, and owns
  `windowTitle`, the three things a program says about itself.
- `viewport.go` refreshes cells, colors, and the cursor, and owns `redrawSet`,
  the rows the next frame has to repaint from either side of the boundary.
- `layout.go` keeps the terminal and PTY sizes aligned with the window.
- `input.go` encodes keys through a per-frame `frameBuffer`; `report.go` does
  the same for mouse and focus events the program has asked for, and owns
  `reportState`, what the program has been told about the pointer so far.
- `selection.go` owns the selection the keyboard makes: select-all, stepping an
  end of a selection around the screen, and writing a screen out through the
  formatter.
- `scrollbar.go` owns the scrollbar, which is `Screen.Scrollbar` and a thumb.
- `mouse.go` owns selection, which is a `gostty.Gesture`: the click count, what
  each count selects, the drag, and the autoscroll at the edges are the
  terminal's, and this file supplies the pointer, the clock, and the policy.
- `hyperlink.go` owns the OSC 8 link under the pointer and opens it.
- `dnd.go` owns OSC 72 drag and drop: what the window system dropped, staged as
  representations for a program that registered for them.
- `clipboard.go` owns `sharedClipboard`, the one
  clipboard every tab copies to and pastes from.
- `render.go` draws the grid layers, cursor, and decorations, and owns
  `gridCanvas`: the two layers the grid is drawn into and their lifetime.
- `kitty.go` owns `imageCache`: the Kitty placement snapshot and its textures.
- `cat.go` connects terminal cells and the window's `thecat.Mode` to `thecat.Companion`.
- `panels.go` translates input and UI actions and connects the text renderer.
- `search.go` owns `tabSearch`: the native search handle, its scanning, match
  selection, and the per-cell highlight the grid is drawn with.
- `settings.go` holds `appearance`, the fonts, theme, and cat mode every tab is
  drawn with, and applies each panel row -- including the font and the display
  scale -- to all of them.
- `ui/search.go`, `ui/settings.go`, and `ui/tabbar.go` define the components.
- `ui/panels.go` routes panel input; `ui/canvas.go` and `ui/theme.go` share drawing and colors.
- `fonts/discovery.go` finds system fonts and selects the default family.
- `fonts/font.go` loads text faces and derives grid and decoration metrics.
- `fonts/emoji.go` loads and caches colour emoji; `emoji.go` places them in cells.
- `shell/session.go` owns the shell process, pty, and cancellable output reader;
  `shell/pty_unix.go`, `shell/pty_windows.go`, and `shell/conpty_windows.go` are
  the two platforms underneath it.

On startup failure, partially created terminal resources are released in dependency
order. Closing the window stops the output reader and terminates and reaps the shell,
including when its output queue is full.

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
