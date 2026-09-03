# example

A small GUI terminal emulator built on [gostty](../). It runs your shell on a
pty and draws the screen in a window with [Ebitengine](https://ebitengine.org).

```
make example        # from the repository root
```

Everything a terminal has to know comes from the binding; this program only owns
the pixels.

```
shell --pty--> Stream.Feed --> Terminal --> RenderState --> Ebitengine
shell <--pty-- input.EncodeKey <-- KeyEvent <-- Ebitengine
```

## What it uses the binding for

- **Parsing.** Bytes from the shell go into `Stream.Feed`. Escape sequences
  split across reads are held for the next call.
- **The grid.** `RenderState` hands back the viewport as one flat
  `[]RenderCell` per frame, with palette indices and defaults already resolved
  to RGB. This is ghostty's own renderer-facing snapshot, so the dirty tracking
  and the palette lookups are not reimplemented here.
- **Keys.** Keystrokes go out through `input.EncodeKey`, so what Ctrl+C or an
  arrow key encodes to follows the modes the running program has set: DECCKM,
  the Kitty keyboard protocol, bracketed paste. The window only says which key
  was pressed.
- **Selection.** Dragging hands two viewport positions to `Screen.SelectRange`;
  ghostty works out what that means for wrapped lines and wide characters. The
  text comes back from `Screen.SelectionString`, and the render state marks the
  selected cells so they can be drawn inverted.
- **OSC side effects.** The title becomes the window title, the bell becomes a
  visual flash, and OSC 52 reads and writes go through the same clipboard the
  user does. `input.IsSafePaste` refuses a paste with a newline in it when the
  program has not asked for bracketed paste.

## Keys

| | |
|---|---|
| Drag | Select. Hold Alt for a block selection. |
| Click | Clear the selection. |
| Ctrl+Shift+C, Cmd+C | Copy the selection. |
| Ctrl+Shift+V, Cmd+V | Paste. |

Everything else goes to the shell.

## Text

The fonts come from the system. On macOS that is Menlo with Apple SD Gothic Neo
for the wide scripts; on Linux, DejaVu Sans Mono or Noto with Noto Sans CJK;
on Windows, Consolas with Malgun Gothic. `GOSTTY_FONT` and `GOSTTY_FONT_CJK`
override the search with a path to a `.ttf`, `.otf` or `.ttc`. If nothing is
found, a bundled 12px bitmap font covering Hangul, kana and CJK takes over, so
the example still runs on a machine with no fonts installed.

Two faces rather than a fallback chain, because the terminal has already decided
how many columns each character gets, and picking the face from that decision
keeps the glyph and the cell in agreement.

Both faces are drawn at the same em size, and the wide one is centred in its two
columns. Matching its *advance* to two cells instead -- the obvious thing, since
the cell is two columns wide -- makes it far too big: a monospace Latin advance
is about 0.6em while a CJK one is near 1em, so forcing the wide advance to twice
the narrow one inflates its em by half again, and a CJK glyph fills its em much
more fully than a Latin one fills its own. Nothing here needs the advance
anyway; every glyph is placed at its own cell's origin, so the advance only has
to fit. It is shrunk only if it does not.

The monospace face defines the grid, its height included. Sizing the row to
whichever face is taller would leave the Latin text swimming in a cell far
bigger than it needs, so the wide face shares its baseline and is allowed to
overflow the row a little instead.

Typed text is encoded from the UTF-8 the platform produced rather than from a
key code, so an IME's committed string goes through unchanged. Composition
itself is still drawn by the OS: Ebitengine does not expose preedit state, so
there is nothing to draw in the window until the text is committed.

## What it is not

Not a terminal you would use. No scrollback view, no preedit display, no mouse
reporting to the program, no font fallback, no ligatures, and it answers OSC 52
read requests without asking the user -- which hands the running program
whatever is on the clipboard, and is the wrong default for anything but a demo.

## Module layout

It needs Ebitengine, `pty` and a clipboard package, so it is its own Go module
with a `replace` back to the repository root. The binding module itself has no
dependencies, and keeping it that way is the point of the split.
