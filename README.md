# **G**~~h~~**o**stty

Go bindings for [libghostty-vt](https://github.com/ghostty-org/ghostty), the terminal emulation core of [Ghostty](https://ghostty.org).

Parse terminal output, inspect cells and scrollback, and encode input for terminal
applications. gostty provides the terminal state and protocol handling; your
application supplies the PTY, window, and renderer.

![HyperCat Term demo](assets/demo.gif)

Try [HyperCat Term](examples/hypercat/README.md), the included GUI terminal emulator
with Kitty graphics and animated cats.

[Features](#features) · [Installation](#installation) · [Quick start](#quick-start) ·
[HyperCat Term](#hypercat-term) · [Usage notes](#usage-notes) · [How it is built](#how-it-is-built)

## Features

- VT parsing, terminal state, scrollback, selection, and search.
- Cell and cursor snapshots for custom renderers, including Kitty graphics.
- Mode-aware keyboard, mouse, focus, and paste encoding.
- Terminal replies, clipboard callbacks, and events for titles, bells, and notifications.
- Terminal snapshots with restore support, including unfinished escape sequences.

## Installation

Requires **Go 1.25+**, **cgo enabled**, and a **C compiler**. Native archives are
included for macOS, Linux, and Windows on amd64 and arm64.

From your Go module:

```sh
go get github.com/ironpark/gostty
```

The native libraries are bundled, so using the package does not require Zig.

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

Output:

```text
hello
world
```

`PlainString` removes color formatting. For styled output, use `RenderState` to
read cells, colors, and cursor state.

## HyperCat Term

[HyperCat Term](examples/hypercat/README.md) is a complete example built with
Ebitengine: a shell running on a PTY, rendered in a window with tabs, search,
clipboard integration, Kitty graphics, and animated cats.

Run it from a checkout using the bundled native archives:

```sh
git clone https://github.com/ironpark/gostty.git
cd gostty
go run ./examples/hypercat
```

The example has its own Go module, so applications importing gostty do not inherit
its GUI dependencies. See the [example README](examples/hypercat/README.md) for
controls and implementation details.

## Usage notes

| Package | Purpose |
| --- | --- |
| `github.com/ironpark/gostty` | Terminal state, streams, rendering data, search, and snapshots. |
| `github.com/ironpark/gostty/input` | Encode keyboard, mouse, focus, and paste input. |
| `github.com/ironpark/gostty/sys` | Process-wide PNG decoding and entropy hooks. |

For a PTY integration, feed process output into a stream, drain its events, and
write terminal replies back to the PTY. Send encoded user input to the same PTY.
HyperCat demonstrates this integration in [terminal.go](examples/hypercat/terminal.go) and
[terminal_input.go](examples/hypercat/terminal_input.go).

- **Serialize terminal access.** This includes getters and operations on its
  streams, screens, searches, and tracked references. The lifetime mutex does
  not serialize terminal operations.
- **Close children before their terminal.** Use `defer` in creation order, as in
  the quick start. Closing a terminal with live children returns `ErrHandleInUse`.
  `Close` is idempotent; calls on closed handles return `ErrInvalidHandle`.
- **Keep callbacks synchronous.** Answer clipboard requests during their callback;
  unanswered requests are denied. Defer terminal mutations until the active call
  returns, and obtain user consent before serving clipboard reads.
- **Install system hooks before use.** PNG Kitty graphics need a decoder supplied
  through `sys`; system-hook callbacks should only call their reply APIs.

## How it is built

gostty uses Ghostty's Zig terminal core, **libghostty-vt**.
[zigo](https://github.com/ironpark/zigo) generates the Go API and C ABI bridge
from Zig binding declarations. Go calls the native core through cgo, so parsing
and terminal state stay in Ghostty while applications work with Go types.

The generated bindings and prebuilt static libraries are committed together.
Native libraries are built with Zig's `ReleaseSafe` optimization for macOS,
Linux, and Windows on amd64 and arm64.

## License

[MIT](LICENSE) for this repository's code. libghostty-vt is part of Ghostty and
is also MIT licensed.
