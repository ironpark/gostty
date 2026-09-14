# vtdump

Render terminal output that was captured to a file.

A program's stdout is not what you saw on screen: it is the instructions that
produced it. Progress bars rewrite their line, `clear` erases the scrollback,
an editor paints over itself. Reading that back with `cat` replays the
instructions; `less -R` shows some of them. `vtdump` runs them through a real
terminal and prints the grid they left.

```sh
some-build-tool 2>&1 | go run ./examples/vtdump
go run ./examples/vtdump -format html < session.log > session.html
go run ./examples/vtdump -events -cols 120 < session.log
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `-cols`, `-rows` | `80`, `24` | The grid the capture is replayed into. |
| `-scrollback` | `10000` | Lines of scrollback to keep. Pruned at page boundaries, so not an exact cap. |
| `-format` | `plain` | `plain`, `vt` (replays into a terminal), or `html`. |
| `-keep-trailing-space` | off | Keep trailing whitespace on each line. |
| `-events` | off | Report titles, bells and progress on stderr. |

It is one file, and it is the smallest complete tour of the binding:

- `gostty.New` builds a terminal and its parser under one handle.
- A `Session` is an `io.Writer`, so feeding it is `io.Copy`. Escape sequences
  split across reads are fine; the parser keeps its state.
- A `Formatter` holds its options and streams the result with `WriteTo`.
- `Stream.EventValues` drains what the program asked for -- titles, bells,
  progress -- after feeding rather than during it.
