#!/usr/bin/env python3
"""Send an image to the terminal with the Kitty graphics protocol.

There is nothing to install: the protocol is an escape sequence, so this writes
one. It exists so the example terminal's image support can be exercised without
icat, timg or anything else on the machine.

    ./kittydemo.py                    # a generated 128x128 gradient
    ./kittydemo.py --cells 20x10      # scaled to a cell box
    ./kittydemo.py --rgba             # with an alpha channel
    ./kittydemo.py --z -1             # behind the text
    ./kittydemo.py --query            # ask whether images work at all
    ./kittydemo.py --reply            # let the terminal answer, to see it work

Run it in the example terminal, or in Ghostty or Kitty to check the sequences
themselves are right.

Commands are sent with q=2, which asks the terminal not to answer. A terminal
answers on the pty, and the pty is the shell's input: a sender that is not going
to read the answer has to suppress it, or the shell reads it instead and echoes
the tail of it back ("Gi=1;OK") as if it had been typed. --reply turns the
suppression off, which is the way to see that replies work at all.

The image is generated rather than read from a file because the pixels go over
the wire raw (f=24/f=32). PNG (f=100) is not sent: libghostty-vt ships library
builds with no PNG decoder, so the terminal rejects it. See the example README.
"""

import argparse
import base64
import sys

# The protocol wants the payload split into chunks of at most 4096 base64
# characters, each carrying m=1 until the last, which carries m=0.
CHUNK = 4096


def emit(payload: bytes, **keys) -> None:
    """Write one APC command, chunked."""
    data = base64.standard_b64encode(payload)
    control = ",".join(f"{k}={v}" for k, v in keys.items() if v is not None)

    if not data:
        sys.stdout.write(f"\033_G{control}\033\\")
        return

    first = True
    while data:
        chunk, data = data[:CHUNK], data[CHUNK:]
        more = 1 if data else 0
        # Only the first chunk carries the control keys; the rest carry m only.
        prefix = f"{control},m={more}" if first else f"m={more}"
        sys.stdout.write(f"\033_G{prefix};{chunk.decode('ascii')}\033\\")
        first = False


def gradient(width: int, height: int, alpha: bool) -> bytes:
    """A red/green gradient with a blue grid, as raw RGB or RGBA."""
    out = bytearray()
    for y in range(height):
        for x in range(width):
            grid = 0xFF if (x % 16 == 0 or y % 16 == 0) else 0x20
            pixel = [x * 255 // max(width - 1, 1),
                     y * 255 // max(height - 1, 1),
                     grid]
            if alpha:
                # Fade out to the right, so the text underneath shows through.
                pixel.append(255 - (x * 255 // max(width - 1, 1)))
            out += bytes(pixel)
    return bytes(out)


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__,
                                 formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--size", default="128x128", metavar="WxH",
                    help="size of the generated gradient (default 128x128)")
    ap.add_argument("--cells", metavar="COLSxROWS",
                    help="scale the image into this many cells")
    ap.add_argument("--rgba", action="store_true", help="send RGBA instead of RGB")
    ap.add_argument("--id", type=int, default=1, help="image id (default 1)")
    ap.add_argument("--z", type=int, help="stacking order; negative is behind the text")
    ap.add_argument("--query", action="store_true",
                    help="only ask whether the protocol is supported")
    ap.add_argument("--reply", action="store_true",
                    help="do not suppress the terminal's answer (the shell will "
                         "read it as input and echo it)")
    args = ap.parse_args()

    if args.query:
        # a=q with a one-pixel image: the terminal answers OK or an error, and
        # nothing is displayed. The reply lands on stdin, so it shows up as
        # stray input rather than in this program's output.
        # A query is a question, so it is never suppressed. The answer arrives
        # on the pty, which is the shell's input, so it shows up as something
        # typed rather than in this program's output.
        emit(bytes((1, 2, 3)), a="q", f=24, s=1, v=1, t="d", i=31)
        sys.stdout.flush()
        print("\nsent a support query; the answer (_Gi=31;OK) arrives as input",
              file=sys.stderr)
        return 0

    cols = rows = None
    if args.cells:
        cols, rows = (int(v) for v in args.cells.lower().split("x"))

    width, height = (int(v) for v in args.size.lower().split("x"))
    # The raw formats carry no dimensions of their own, so the command does:
    # f=24 is RGB, f=32 is RGBA, s and v are the width and height.
    emit(gradient(width, height, args.rgba),
         a="T", f=32 if args.rgba else 24, s=width, v=height, t="d",
         i=args.id, c=cols, r=rows, z=args.z,
         q=None if args.reply else 2)

    sys.stdout.flush()
    # The image is drawn at the cursor and the cursor does not move, so leave
    # room under it rather than printing the next prompt over the top.
    print("\n" * (rows if rows else 10))
    return 0


if __name__ == "__main__":
    sys.exit(main())
