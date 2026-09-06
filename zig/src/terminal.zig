//! Terminal-wide state: size, colours, modes, attributes and limits.
//!
//! Each wrapper here exists because ghostty's own signature cannot cross a C
//! ABI: a nested optional struct for the resize geometry, a `packed struct(u24)`
//! for a colour, a tagged union carrying parser leftovers for an SGR attribute,
//! or a null parameter standing for "no limit", which Go would have to spell as
//! a pointer.
const std = @import("std");
const vt = @import("ghostty_vt");
const common = @import("common.zig");

const Allocator = std.mem.Allocator;
const Terminal = common.Terminal;
const Screen = common.Screen;
const io = common.io;
const packColor = common.packColor;
const unpackColor = common.unpackColor;
const Underline = common.Underline;

pub const ProtectedMode = vt.ProtectedMode;

pub const Charset = vt.Charset;
pub const CharsetSlot = vt.CharsetSlot;
pub const CharsetActiveSlot = vt.CharsetActiveSlot;
pub const DeccolmMode = Terminal.DeccolmMode;
pub const ScrollViewport = Terminal.ScrollViewport;
pub const EraseDisplay = vt.EraseDisplay;
pub const EraseLine = vt.EraseLine;
pub const TabClear = vt.TabClear;

/// Change the viewport size, leaving the pixel geometry alone.
///
/// Wrapped because `vt.Terminal.Resize` carries a nested optional struct for
/// the cell size in pixels, which has no C representation.
pub fn resize(self: *Terminal, gpa: Allocator, width: u16, height: u16) !void {
    try self.resize(gpa, .{ .cols = width, .rows = height });
}

/// Change the viewport size and tell the terminal how many pixels a cell is.
///
/// The pixel geometry is only used by the parts of the protocol that measure in
/// pixels -- Kitty graphics placements above all -- and is zero until it is set,
/// which leaves every image sized zero. A renderer that draws images should
/// resize with this rather than `resize`: the terminal stores the pixel size of
/// the whole grid, so it goes stale as soon as the column count changes.
pub fn resizeCells(
    self: *Terminal,
    gpa: Allocator,
    width: u16,
    height: u16,
    cell_width: u32,
    cell_height: u32,
) !void {
    try self.resize(gpa, .{
        .cols = width,
        .rows = height,
        .cell_size_px = .{ .width = cell_width, .height = cell_height },
    });
}

/// Write the cursor's current SGR attributes into `dst` as a DECRPSS response
/// body, and report how many bytes were written.
///
/// Wrapped because ghostty returns a slice into the caller's buffer, and zigo
/// reports a written count instead.
pub fn printAttributesInto(self: *Terminal, dst: []u8) !usize {
    const written = try self.printAttributes(dst);
    return written.len;
}

/// The scrollback contents, oldest row first, newline separated.
///
/// Wrapped because the region is chosen with `point.Point`, a tagged union
/// carrying a coordinate, which zigo cannot take by value.
pub fn historyString(self: *Terminal, gpa: Allocator) ![]const u8 {
    return try self.screens.active.dumpStringAlloc(gpa, .{ .history = .{} });
}

/// One of the 16 named ANSI colors.
pub const ColorName = vt.color.Name;

/// The color's default value as `0xRRGGBB`, or null when the name has none.
///
/// Only the sixteen named colors have one. The enum is open because every
/// other 256-color palette index is a valid value, and those take their
/// color from the palette rather than from a default; read them with
/// `paletteColors`.
///
/// Wrapped because ghostty returns `color.RGB`, a `packed struct(u24)` with
/// no C representation, behind an error union.
pub fn colorNameDefault(name: ColorName) ?u32 {
    return packColor(name.default() catch return null);
}

/// A single SGR attribute to apply to the cursor's pen.
///
/// A curated mirror of ghostty's `sgr.Attribute`. Two things keep the original
/// from crossing: its `unknown` variant carries the raw CSI parameters, which
/// are a parser detail rather than something a caller sets, and its color
/// variants carry a `packed struct(u24)` that has no C representation. RGB is
/// carried here as `0xRRGGBB` instead.
pub const Attribute = union(enum) {
    unset,
    bold,
    reset_bold,
    italic,
    reset_italic,
    faint,
    underline: Underline,
    underline_color_rgb: u32,
    underline_color_256: u8,
    reset_underline_color,
    overline,
    reset_overline,
    blink,
    reset_blink,
    inverse,
    reset_inverse,
    invisible,
    reset_invisible,
    strikethrough,
    reset_strikethrough,
    direct_color_fg: u32,
    direct_color_bg: u32,
    color_256_fg: u8,
    color_256_bg: u8,
    named_fg: ColorName,
    named_bg: ColorName,
    bright_named_fg: ColorName,
    bright_named_bg: ColorName,
    reset_fg,
    reset_bg,

    fn toVt(self: Attribute) vt.Attribute {
        return switch (self) {
            .unset => .unset,
            .bold => .bold,
            .reset_bold => .reset_bold,
            .italic => .italic,
            .reset_italic => .reset_italic,
            .faint => .faint,
            .underline => |v| .{ .underline = v },
            .underline_color_rgb => |v| .{ .underline_color = unpackColor(v) },
            .underline_color_256 => |v| .{ .@"256_underline_color" = v },
            .reset_underline_color => .reset_underline_color,
            .overline => .overline,
            .reset_overline => .reset_overline,
            .blink => .blink,
            .reset_blink => .reset_blink,
            .inverse => .inverse,
            .reset_inverse => .reset_inverse,
            .invisible => .invisible,
            .reset_invisible => .reset_invisible,
            .strikethrough => .strikethrough,
            .reset_strikethrough => .reset_strikethrough,
            .direct_color_fg => |v| .{ .direct_color_fg = unpackColor(v) },
            .direct_color_bg => |v| .{ .direct_color_bg = unpackColor(v) },
            .color_256_fg => |v| .{ .@"256_fg" = v },
            .color_256_bg => |v| .{ .@"256_bg" = v },
            .named_fg => |v| .{ .@"8_fg" = v },
            .named_bg => |v| .{ .@"8_bg" = v },
            .bright_named_fg => |v| .{ .@"8_bright_fg" = v },
            .bright_named_bg => |v| .{ .@"8_bright_bg" = v },
            .reset_fg => .reset_fg,
            .reset_bg => .reset_bg,
        };
    }
};

/// Apply an SGR attribute to the cursor's pen. Everything printed afterwards
/// carries it until it is reset.
pub fn setAttribute(self: *Terminal, attr: Attribute) !void {
    try self.setAttribute(attr.toVt());
}

// ghostty expresses "no limit" and "use the default" as null parameters. A Go
// caller would have to pass a pointer for those, so each one is split into a
// setter that takes the value and a reset that selects the null case.

/// Set the default cursor blink. Applied immediately only when the cursor
/// currently follows its defaults; otherwise saved for the next reset.
pub fn setDefaultCursorBlink(self: *Terminal, blink: bool) void {
    self.setDefaultCursorBlink(blink);
}

/// Return the default cursor blink to the emulator default (blinking).
pub fn resetDefaultCursorBlink(self: *Terminal) void {
    self.setDefaultCursorBlink(null);
}

/// Limit the primary screen's scrollback to `max` bytes. Zero disables
/// scrollback and erases retained history.
pub fn setScrollbackMaxBytes(self: *Terminal, max: usize) void {
    self.setScrollbackMaxBytes(max);
}

/// Remove the primary screen's scrollback byte limit.
pub fn clearScrollbackMaxBytes(self: *Terminal) void {
    self.setScrollbackMaxBytes(null);
}

/// Limit the primary screen's scrollback to `max` physical lines.
pub fn setScrollbackMaxLines(self: *Terminal, max: usize) void {
    self.setScrollbackMaxLines(max);
}

/// Remove the primary screen's scrollback line limit.
pub fn clearScrollbackMaxLines(self: *Terminal) void {
    self.setScrollbackMaxLines(null);
}

// Colors. Everything is `0xRRGGBB`, the same packing `RenderCell` uses, so a
// renderer keeps one color representation.

/// The current background color: what OSC 11 set, else the default.
pub fn backgroundColor(self: *const Terminal) ?u32 {
    return packColor(self.colors.background.get() orelse return null);
}

/// The current foreground color: what OSC 10 set, else the default.
pub fn foregroundColor(self: *const Terminal) ?u32 {
    return packColor(self.colors.foreground.get() orelse return null);
}

/// The current cursor color, if one was set or configured. Null means the
/// cursor takes the foreground color.
pub fn cursorColor(self: *const Terminal) ?u32 {
    return packColor(self.colors.cursor.get() orelse return null);
}

/// Copy the current 256-color palette into `dst` and return how many entries
/// were written: 256, or `dst.len` if shorter.
pub fn paletteColors(self: *const Terminal, dst: []u32) usize {
    const n = @min(dst.len, self.colors.palette.current.len);
    for (dst[0..n], self.colors.palette.current[0..n]) |*out, c| out.* = packColor(c);
    return n;
}

/// Set the configured default background: the value in effect until OSC 11
/// overrides it and again after OSC 111 resets it.
pub fn setDefaultBackgroundColor(self: *Terminal, rgb: u32) void {
    self.colors.background.default = unpackColor(rgb);
}

/// Set the configured default foreground. See `setDefaultBackgroundColor`.
pub fn setDefaultForegroundColor(self: *Terminal, rgb: u32) void {
    self.colors.foreground.default = unpackColor(rgb);
}

/// Set the configured default cursor color. See `setDefaultBackgroundColor`.
pub fn setDefaultCursorColor(self: *Terminal, rgb: u32) void {
    self.colors.cursor.default = unpackColor(rgb);
}

/// An ANSI or DEC private mode: the switches a program flips with `CSI ? h`
/// and `CSI ? l`, such as cursor keys (DECCKM), bracketed paste or the
/// mouse tracking modes.
pub const Mode = vt.Mode;

/// Whether `mode` is currently on.
pub fn modeEnabled(self: *const Terminal, mode: Mode) bool {
    return self.modes.get(mode);
}

/// Turn `mode` on or off. This flips the state only; the side effects the
/// parser performs when a program changes a mode -- switching screens for
/// 1049, resizing for 132-column -- do not run. Use `switchScreenMode` and
/// `deccolm` for those.
pub fn setMode(self: *Terminal, mode: Mode, value: bool) void {
    self.modes.set(mode, value);
}
