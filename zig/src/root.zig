//! The libghostty-vt surface exposed to Go.
//!
//! `Terminal` is ghostty's own type, bound directly: its methods become Go
//! methods without any wrapper. This module only adds what ghostty does not
//! provide and zigo needs:
//!
//!   - an `std.Io` value for `.io` injection (ghostty ships `TinyIo` as a type,
//!     not as a ready-made `Io` declaration);
//!   - a constructor and destructor, because `Terminal.init` takes a
//!     `Terminal.Options` whose `Colors` field holds optionals and so cannot
//!     cross the C ABI, and because it returns by value;
//!   - accessors for fields, which zigo does not bind;
//!   - a release function for the string `plainString` hands out.
//!
//! Every `gpa` / `io_impl` parameter below is filled in by zigo's `.allocator`
//! and `.io` injection and never appears in the C or Go signatures.
const std = @import("std");
const vt = @import("ghostty_vt");

const Allocator = std.mem.Allocator;

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

/// A VT stream: parses escape sequences and applies them to a `Terminal`.
///
/// A stream borrows its terminal and must be closed before it: ghostty's
/// `Handler.deinit` reaches through the terminal for its allocator. The binding
/// declares the stream a child of its terminal, so closing the terminal first is
/// refused rather than left to the caller to get right.
pub const Stream = vt.TerminalStream;

pub const ProtectedMode = vt.ProtectedMode;

pub const Charset = vt.Charset;
pub const CharsetSlot = vt.CharsetSlot;
pub const CharsetActiveSlot = vt.CharsetActiveSlot;
pub const DeccolmMode = Terminal.DeccolmMode;
pub const ScrollViewport = Terminal.ScrollViewport;
pub const EraseDisplay = vt.EraseDisplay;
pub const EraseLine = vt.EraseLine;
pub const TabClear = vt.TabClear;

/// Change the viewport size.
///
/// Wrapped because `vt.Terminal.Resize` carries a nested optional struct for
/// the cell size in pixels, which has no C representation.
pub fn resize(self: *Terminal, gpa: Allocator, width: u16, height: u16) !void {
    try self.resize(gpa, .{ .cols = width, .rows = height });
}

/// The display width of a grapheme cluster given as codepoints.
///
/// Wrapped because `vt.unicode.graphemeWidth` is generic over the codepoint
/// integer type, and a generic function has no signature to bind.
pub fn graphemeWidth(cps: []const u32) u8 {
    return @intCast(vt.unicode.graphemeWidth(u32, cps).width);
}

/// Unicode helpers, re-exported as-is.
pub const unicode = vt.unicode;

/// Create a terminal with the given viewport size.
pub fn newTerminal(gpa: Allocator, io_impl: std.Io, width: u16, height: u16) !*Terminal {
    const self = try gpa.create(Terminal);
    errdefer gpa.destroy(self);
    self.* = try .init(io_impl, gpa, .{ .cols = width, .rows = height });
    return self;
}

/// Destroys a terminal created by `newTerminal`.
pub fn freeTerminal(self: *Terminal, gpa: Allocator) void {
    self.deinit(gpa);
    gpa.destroy(self);
}

/// Create a VT stream that applies escape sequences to `terminal`.
///
/// `continuation_max_bytes` caps the unfinished-sequence suffix the stream
/// tracks across feeds; zero disables tracking.
pub fn newStream(gpa: Allocator, terminal: *Terminal, continuation_max_bytes: usize) !*Stream {
    const self = try gpa.create(Stream);
    errdefer gpa.destroy(self);
    self.* = .init(.{
        .allocator = gpa,
        .handler = .init(terminal),
        .continuation_max_bytes = continuation_max_bytes,
    });
    return self;
}

/// Destroys a stream created by `newStream`.
pub fn freeStream(self: *Stream, gpa: Allocator) void {
    self.deinit();
    gpa.destroy(self);
}

/// True once a sequence failed in a way the terminal could not absorb, such as
/// an allocation failure. Streams are best-effort and keep going regardless.
pub fn streamFailed(self: *Stream) bool {
    return self.handler.semantic_failure;
}

/// Releases a string handed out by `plainString`.
pub fn freeString(gpa: Allocator, str: []const u8) void {
    gpa.free(str);
}

pub fn cols(self: *Terminal) u16 {
    return self.cols;
}

pub fn rows(self: *Terminal) u16 {
    return self.rows;
}

pub fn cursorX(self: *Terminal) u16 {
    return self.screens.active.cursor.x;
}

pub fn cursorY(self: *Terminal) u16 {
    return self.screens.active.cursor.y;
}

pub fn cursorStyle(self: *Terminal) CursorStyle {
    return self.screens.active.cursor.cursor_style;
}
