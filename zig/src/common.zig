//! The handful of declarations every other file here needs: the injected
//! `std.Io`, the two ghostty types the wrappers hang off, and the colour
//! packing every surface shares.
const std = @import("std");
const vt = @import("ghostty_vt");

/// The `std.Io` injected into every bound call. TinyIo is ghostty's own
/// blocking implementation, recommended for embedders.
pub const io: std.Io = (vt.TinyIo.init).io();

pub const Terminal = vt.Terminal;

/// The visual style of the cursor.
pub const CursorStyle = vt.CursorStyle;

/// The DECSCUSR cursor style request.
///
/// Not taken from `vt.CursorStyleReq`: that alias points at
/// `terminal.CursorStyle` (the 4-tag screen cursor style) rather than
/// `terminal.CursorStyleReq`, so the 7-tag request enum is unreachable by name
/// from libghostty-vt's public API. Reported upstream; read it off the method
/// signature until that is fixed.
pub const CursorStyleReq = @typeInfo(@TypeOf(Terminal.setCursorStyle)).@"fn".params[1].type.?;

/// Which of a terminal's screens is active.
pub const ScreenKey = vt.ScreenSet.Key;

/// One of a terminal's screens: its grid, scrollback and selection.
///
/// A screen is owned by its terminal, so handles to one are borrowed. They stay
/// valid while the terminal is open and are invalidated by its `Close`.
pub const Screen = vt.Screen;

/// The underline style an `Attribute` selects. Shared with `CellFlags`, which
/// carries the same field for a rendered cell.
pub const Underline = vt.Attribute.Underline;

// Colours are `0xRRGGBB` everywhere the binding hands one over, so a renderer
// keeps a single representation.

pub fn packColor(c: vt.color.RGB) u32 {
    return (@as(u32, c.r) << 16) | (@as(u32, c.g) << 8) | c.b;
}

pub fn unpackColor(v: u32) vt.color.RGB {
    return .{ .r = @truncate(v >> 16), .g = @truncate(v >> 8), .b = @truncate(v) };
}
