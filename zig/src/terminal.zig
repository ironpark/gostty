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
const RGB = common.RGB;
const packColor = common.packColor;
const unpackColor = common.unpackColor;
const Underline = common.Underline;

pub const ProtectedMode = vt.ProtectedMode;

pub const Charset = vt.Charset;
pub const CharsetSlot = vt.CharsetSlot;
pub const CharsetActiveSlot = vt.CharsetActiveSlot;
pub const DeccolmMode = Terminal.DeccolmMode;
pub const ScrollViewport = Terminal.ScrollViewport;
pub const CompressionMode = Terminal.CompressionMode;
pub const CompressionResult = Terminal.CompressionResult;
pub const EraseDisplay = vt.EraseDisplay;
pub const EraseLine = vt.EraseLine;
pub const TabClear = vt.TabClear;

/// Change the viewport size and tell the terminal how many pixels a cell is.
///
/// The pixel geometry is only used by the parts of the protocol that measure in
/// pixels -- Kitty graphics placements above all -- and is zero until it is set,
/// which leaves every image sized zero. A renderer that draws images should
/// resize with this rather than `resize`: the terminal stores the pixel size of
/// the whole grid, so it goes stale as soon as the column count changes.
///
/// A terminal a stream feeds should be resized through `Stream.resizeCells`
/// instead: this one never reaches the stream, so a program that asked for
/// in-band size reports (mode 2048) is not told and a render hold does not end.
pub fn resizeCells(
    self: *Terminal,
    gpa: Allocator,
    width: u16,
    height: u16,
    cell_width: u32,
    cell_height: u32,
) !void {
    try self.resize(gpa, cellResize(width, height, cell_width, cell_height));
}

/// The resize `resizeCells` and `Stream.resizeCells` both make.
pub fn cellResize(width: u16, height: u16, cell_width: u32, cell_height: u32) Terminal.Resize {
    return .{
        .cols = width,
        .rows = height,
        .cell_size_px = .{ .width = cell_width, .height = cell_height },
    };
}

/// The scrollback contents, oldest row first, newline separated.
///
/// Wrapped to fix the region. zigo takes `point.Point` by value now -- its
/// payload is an `extern struct` -- so this could hand the union to Go and let
/// a caller pick any of the four origins. It does not, because the rest of this
/// binding spells a position as `(tag, x, y)` (`cellAt`, `hyperlinkAt`,
/// `newGridRef`), and the other three origins are already reachable:
/// `plainString` is the active area and `Screen.format` is the screen with its
/// scrollback. What is left for this one to be is the scrollback, named.
pub fn historyString(self: *Terminal, gpa: Allocator) ![]const u8 {
    return try self.screens.active.dumpStringAlloc(gpa, .{ .history = .{} });
}

/// One of the 16 named ANSI colors.
pub const ColorName = vt.color.Name;

/// The color's default value, or null when the name has none.
///
/// Only the sixteen named colors have one. The enum is open because every
/// other 256-color palette index is a valid value, and those take their
/// color from the palette rather than from a default; read them with
/// `paletteColors`.
///
/// Wrapped because ghostty returns `color.RGB`, a `packed struct(u24)` with
/// no C representation, behind an error union.
pub fn colorNameDefault(name: ColorName) ?RGB {
    return packColor(name.default() catch return null);
}

/// A single SGR attribute to apply to the cursor's pen.
///
/// A curated mirror of ghostty's `sgr.Attribute`. Two things keep the original
/// from crossing: its `unknown` variant carries the raw CSI parameters, which
/// are a parser detail rather than something a caller sets, and its color
/// variants carry a `packed struct(u24)` that has no C representation. RGB is
/// carried here as `RGB` instead.
pub const Attribute = union(enum) {
    unset,
    bold,
    reset_bold,
    italic,
    reset_italic,
    faint,
    underline: Underline,
    underline_color: RGB,
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
    direct_color_fg: RGB,
    direct_color_bg: RGB,
    color_256_fg: u8,
    color_256_bg: u8,
    named_fg: ColorName,
    named_bg: ColorName,
    bright_named_fg: ColorName,
    bright_named_bg: ColorName,
    reset_fg,
    reset_bg,
    /// An SGR code ghostty does not implement. Added last so the tag values Go
    /// already knows do not move. ghostty's own variant carries the raw
    /// parameters that stumped it; those are a parser detail, so only the fact
    /// crosses. Applying it is a no-op, which is what ghostty does with it too.
    unknown,

    fn toVt(self: Attribute) vt.Attribute {
        return switch (self) {
            .unset => .unset,
            .bold => .bold,
            .reset_bold => .reset_bold,
            .italic => .italic,
            .reset_italic => .reset_italic,
            .faint => .faint,
            .underline => |v| .{ .underline = v },
            .underline_color => |v| .{ .underline_color = unpackColor(v) },
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
            .unknown => .{ .unknown = .{ .full = &.{}, .partial = &.{} } },
        };
    }

    /// The inverse of `toVt`, for the standalone SGR parser in `parser.zig`.
    pub fn fromVt(attr: vt.Attribute) Attribute {
        return switch (attr) {
            .unset => .unset,
            .unknown => .unknown,
            .bold => .bold,
            .reset_bold => .reset_bold,
            .italic => .italic,
            .reset_italic => .reset_italic,
            .faint => .faint,
            .underline => |v| .{ .underline = v },
            .underline_color => |v| .{ .underline_color = packColor(v) },
            .@"256_underline_color" => |v| .{ .underline_color_256 = v },
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
            .direct_color_fg => |v| .{ .direct_color_fg = packColor(v) },
            .direct_color_bg => |v| .{ .direct_color_bg = packColor(v) },
            .@"256_fg" => |v| .{ .color_256_fg = v },
            .@"256_bg" => |v| .{ .color_256_bg = v },
            .@"8_fg" => |v| .{ .named_fg = v },
            .@"8_bg" => |v| .{ .named_bg = v },
            .@"8_bright_fg" => |v| .{ .bright_named_fg = v },
            .@"8_bright_bg" => |v| .{ .bright_named_bg = v },
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

/// Apply every attribute of one CSI SGR sequence to the cursor's pen at once.
/// `params` and `colon_mask` are what `sgrAttributeCount` takes: the
/// parameters of `CSI ... m` without the `m`, and bit `i` set when parameter
/// `i` was followed by a colon. The resulting style is committed once, so
/// `CSI 0;1;31 m` costs one style update rather than three, and on failure the
/// pen reverts to what it was before the sequence.
///
/// Wrapped because ghostty takes its `sgr.Parser`, whose separator list is a
/// `std.StaticBitSet` with no C representation.
pub fn setAttributes(self: *Terminal, params: []const u16, colon_mask: u32) !void {
    try self.setAttributes(try @import("parser.zig").sgrParser(params, colon_mask));
}

/// Move the cursor `delta` rows down, or up when negative, keeping its column
/// (VPR, and its reverse). Unlike `cursorDown`/`cursorUp`, this stops only at
/// the edge of the screen, or of the scrolling region in origin mode.
///
/// Wrapped because ghostty takes an unsigned count and only moves forward,
/// which the name "relative" does not suggest.
pub fn cursorRowRelative(self: *Terminal, delta: isize) void {
    if (delta >= 0) return self.cursorRowRelative(@intCast(delta));
    const pos = cursorPosForSet(self);
    // setCursorPos clamps a row of 0 to 1, so saturating is enough.
    self.setCursorPos(pos.row -| @abs(delta), pos.col);
}

/// Move the cursor `delta` columns right, or left when negative, keeping its
/// row (HPR, and its reverse). Unlike `cursorRight`/`cursorLeft`, this stops
/// only at the edge of the screen, or of the scrolling region in origin mode.
pub fn cursorColRelative(self: *Terminal, delta: isize) void {
    if (delta >= 0) return self.cursorColRelative(@intCast(delta));
    const pos = cursorPosForSet(self);
    self.setCursorPos(pos.row, pos.col -| @abs(delta));
}

/// ghostty's private `cursorPosForSet`: the cursor in `setCursorPos`
/// coordinates, 1-based and relative to the scrolling region in origin mode.
fn cursorPosForSet(self: *const Terminal) struct { row: usize, col: usize } {
    const cursor = &self.screens.active.cursor;
    const origin = self.modes.get(.origin);
    return .{
        .row = @as(usize, cursor.y -| if (origin) self.scrolling_region.top else 0) + 1,
        .col = @as(usize, cursor.x -| if (origin) self.scrolling_region.left else 0) + 1,
    };
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

// Colors. Everything is `RGB`, the same type `RenderCell` uses, so a
// renderer keeps one color representation.

/// The current background color: what OSC 11 set, else the default.
pub fn backgroundColor(self: *const Terminal) ?RGB {
    return packColor(self.colors.background.get() orelse return null);
}

/// The current foreground color: what OSC 10 set, else the default.
pub fn foregroundColor(self: *const Terminal) ?RGB {
    return packColor(self.colors.foreground.get() orelse return null);
}

/// The current cursor color, if one was set or configured. Null means the
/// cursor takes the foreground color.
pub fn cursorColor(self: *const Terminal) ?RGB {
    return packColor(self.colors.cursor.get() orelse return null);
}

/// Copy the current 256-color palette into `dst` and return how many entries
/// were written: 256, or `dst.len` if shorter.
pub fn paletteColors(self: *const Terminal, dst: []RGB) usize {
    const n = @min(dst.len, self.colors.palette.current.len);
    for (dst[0..n], self.colors.palette.current[0..n]) |*out, c| out.* = packColor(c);
    return n;
}

/// Set the configured default background: the value in effect until OSC 11
/// overrides it and again after OSC 111 resets it.
pub fn setDefaultBackgroundColor(self: *Terminal, rgb: RGB) void {
    self.colors.background.default = unpackColor(rgb);
}

/// Set the configured default foreground. See `setDefaultBackgroundColor`.
pub fn setDefaultForegroundColor(self: *Terminal, rgb: RGB) void {
    self.colors.foreground.default = unpackColor(rgb);
}

/// Set the configured default cursor color. See `setDefaultBackgroundColor`.
pub fn setDefaultCursorColor(self: *Terminal, rgb: RGB) void {
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
