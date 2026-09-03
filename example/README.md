# HyperCat Term

A small GUI terminal emulator built with [gostty](../) and
[Ebitengine](https://ebitengine.org).

```sh
make example # run from the repository root
```

This example demonstrates:

- parsing PTY output with `Stream.Feed`
- rendering cells and Kitty graphics from frame snapshots
- mode-aware keyboard, mouse, focus, and paste encoding
- scrollback, selection, search, and clipboard integration
- terminal replies and OSC side effects such as title, bell, and progress

```text
shell --pty--> Stream.Feed --> Terminal --> RenderState --> Ebitengine
shell <--pty-- input.EncodeKey <-- KeyEvent <-- Ebitengine
```

## Controls

| Input | Action |
| --- | --- |
| Wheel | Scroll; sends arrow keys on the alternate screen. |
| Shift+wheel | Scroll while the application owns the mouse. |
| Drag | Select; hold Alt for block selection. |
| Shift+drag | Select while the application owns the mouse. |
| Ctrl+Shift+C / Cmd+C | Copy. |
| Ctrl+Shift+V / Cmd+V | Paste. |
| Ctrl+Shift+F / Cmd+F | Search. |
| Ctrl+Shift+, / Cmd+, | Open settings. |
| Click the cat | Interact with the cat and open settings. |

Everything else is sent to the shell. In search, use Enter or Shift+Enter to
move between matches and Escape to close.

## Kitty graphics

Run these commands inside HyperCat Term, Ghostty, or Kitty:

```sh
./kittydemo.py
./kittydemo.py --cells 20x10
./kittydemo.py --rgba
./kittydemo.py --z -1
./kittydemo.py --query
```

The demo supports raw RGB, RGBA, and grayscale images. PNG transmission is not
supported because library builds of libghostty-vt do not provide a PNG decoder.
Use `--reply` to request and inspect terminal replies.

## Fonts and the cat

The settings panel includes terminal, midnight, Catppuccin, and Solarized
themes. The example discovers system fonts and prefers JetBrains Mono when
available.
Set `GOSTTY_FONT` and `GOSTTY_FONT_CJK` to override the narrow and wide font
paths. A bundled bitmap font is used as a final fallback. Font and size changes
are available in settings.

Colour emoji are drawn from the emoji font's own bitmaps (`sbix` on macOS,
`CBDT` elsewhere) rather than through the text renderer, which rasterises
outlines and would draw them blank. Only two-column cells take that path, which
is the rule the terminal laid the line out with. Set `GOSTTY_FONT_EMOJI` to
override the search. Emoji written as several codepoints -- flags, ZWJ
sequences -- are not drawn, because a cell carries one codepoint and the picture
belongs to the combination; Windows keeps its text face, since Segoe UI Emoji is
a layered vector font rather than a bitmap one.

The cat uses the rendered cell grid as terrain, so it follows terminal output
and scrollback without maintaining a separate world model. It can be disabled
in settings. Hyper Cat mode adds infinite stamina, faster movement, a pulsing
aura, rainbow particles, and motion afterimages. The cat setting cycles through
`off`, `on`, and `hyper`.

Sprites are by **Jump Button**
([Bluesky](https://bsky.app/profile/jumpbutton.bsky.social),
[X](https://twitter.com/Jump_Button)); see `thecat/cat/Read_me.txt` for terms.

## Limitations

This is a binding demo, not a production terminal. It has no preedit display,
font fallback chain, ligatures, tabs, splits, or persistent settings. OSC 52
clipboard reads are accepted without confirmation, which is unsafe for a
general-purpose terminal.

The example is a separate Go module because its UI, PTY, and clipboard packages
are intentionally not dependencies of the binding module.
