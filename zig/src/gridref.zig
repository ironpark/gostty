//! A reference to one cell that follows it as the terminal changes.
//!
//! Every coordinate this binding hands over -- a `Selection`, a viewport
//! position, a search match -- names a cell by where it is now, and stops
//! naming it as soon as the scrollback grows or the viewport moves. A
//! `GridRef` names the cell itself: ghostty's tracked pin, which its page
//! storage keeps updated as pages are added, reflowed and pruned. It is what a
//! renderer keeps for the cell under the mouse between frames, or for the
//! start of a hyperlink hover, and what an embedder keeps for a mark it wants
//! to scroll back to later.
//!
//! The pin stays inside ghostty's storage, so the reference is a child handle
//! of its terminal and closes first, like `Search` and `Gesture`. It can lose
//! its cell: a reset discards every page, and a scrollback limit prunes the
//! oldest, after which `hasValue` is false and the reads report nothing until
//! `set` points it somewhere new.
const std = @import("std");
const vt = @import("ghostty_vt");
const common = @import("common.zig");
const render = @import("render.zig");

const Allocator = std.mem.Allocator;
const Terminal = common.Terminal;
const ScreenKey = common.ScreenKey;
const packColor = common.packColor;
const RenderCell = render.RenderCell;

/// Which coordinate system a position is in.
///
/// A plain mirror of ghostty's `point.Tag`: that one is built by `lib.Enum`
/// and cannot be registered as a Go enum, so the two are converted by name.
pub const PointTag = enum(u8) {
    /// The rows a program can address: what the cursor moves in. The
    /// bottom-right includes rows not yet written to.
    active,
    /// What is on screen right now, so the origin moves as the user scrolls.
    viewport,
    /// Every row from the top of the scrollback; the coordinates a
    /// `Selection` uses.
    screen,
    /// Only the rows above the active area.
    history,

    fn toGhostty(self: PointTag) vt.point.Tag {
        return common.mirror(vt.point.Tag, self);
    }
};

/// A position in one of the coordinate systems `PointTag` names. `y` is
/// wider than `x` because the scrollback can hold more rows than a page.
pub const GridPoint = extern struct {
    x: u16 = 0,
    y: u32 = 0,
};

fn toPoint(tag: PointTag, x: u16, y: u32) vt.Point {
    const coord: vt.Coordinate = .{ .x = x, .y = y };
    return switch (tag) {
        .active => .{ .active = coord },
        .viewport => .{ .viewport = coord },
        .screen => .{ .screen = coord },
        .history => .{ .history = coord },
    };
}

/// A tracked reference to one cell of a terminal.
///
/// The pin is tracked by the page list of the screen it was created on, and
/// stays with that screen: a reference made on the primary screen keeps
/// pointing at the primary screen's cell while the alternate screen is shown.
/// A full reset replaces the screens, which is when a reference silently
/// becomes empty; the generation is what detects that.
pub const GridRef = struct {
    terminal: *Terminal,
    screen_key: ScreenKey,
    generation: usize,
    pin: *vt.Pin,

    /// The page list holding the pin, or null once the screen it was tracked
    /// by has been reinitialized: the pin is gone with it and must not be
    /// touched, let alone untracked.
    fn pages(self: *const GridRef) ?*vt.PageList {
        const t = self.terminal;
        if (t.screens.generation(self.screen_key) != self.generation) return null;
        const screen = t.screens.get(self.screen_key) orelse return null;
        return &screen.pages;
    }

    /// The pin, if it still names a cell.
    fn live(self: *const GridRef) ?vt.Pin {
        _ = self.pages() orelse return null;
        if (self.pin.garbage) return null;
        return self.pin.*;
    }
};

/// Start tracking the cell at `x`, `y` in the coordinate system `tag` names,
/// on the active screen. `error.OutOfBounds` if there is no such cell.
///
/// Returned by value, like `Terminal.init`: zigo boxes the result and frees
/// the box in `close`.
pub fn newGridRef(t: *Terminal, tag: PointTag, x: u16, y: u32) error{ OutOfBounds, OutOfMemory }!GridRef {
    const list = &t.screens.active.pages;
    const pin = list.pin(toPoint(tag, x, y)) orelse return error.OutOfBounds;
    const tracked = try list.trackPin(pin);
    return .{
        .terminal = t,
        .screen_key = t.screens.active_key,
        .generation = t.screens.generation(t.screens.active_key),
        .pin = tracked,
    };
}

/// Stop tracking and release the pin. Must happen before the terminal is
/// closed.
pub fn gridRefClose(self: *GridRef) void {
    if (self.pages()) |list| list.untrackPin(self.pin);
}

/// Whether the reference still names a cell. False after a reset, or after
/// the scrollback limit pruned the page the cell was on.
pub fn gridRefHasValue(self: *const GridRef) bool {
    return self.live() != null;
}

/// Where the cell is now, in the coordinate system `tag` names. Null when the
/// reference is empty, or when the cell is outside that system -- a cell in
/// the scrollback has no `active` position, and one scrolled off screen has
/// no `viewport` position.
pub fn gridRefPoint(self: *const GridRef, tag: PointTag) ?GridPoint {
    const list = self.pages() orelse return null;
    const pin = self.live() orelse return null;
    const pt = list.pointFromPin(tag.toGhostty(), pin) orelse return null;
    const coord = pt.coord();
    return .{ .x = coord.x, .y = coord.y };
}

/// Point the reference at another cell of the active screen, clearing an
/// empty state. Returns false, leaving the reference as it was, if there is no
/// such cell.
pub fn gridRefSet(self: *GridRef, tag: PointTag, x: u16, y: u32) error{OutOfMemory}!bool {
    const t = self.terminal;
    const list = &t.screens.active.pages;
    const pin = list.pin(toPoint(tag, x, y)) orelse return false;
    const tracked = try list.trackPin(pin);
    if (self.pages()) |old| old.untrackPin(self.pin);
    self.screen_key = t.screens.active_key;
    self.generation = t.screens.generation(self.screen_key);
    self.pin = tracked;
    return true;
}

/// The cell, with its colors resolved the way `RenderState` resolves them.
/// Null when the reference is empty. `selected` is never set: the selection
/// is a property of a frame, not of a cell.
pub fn gridRefCell(self: *const GridRef) ?RenderCell {
    const pin = self.live() orelse return null;
    return cellFromPin(self.terminal, pin);
}

/// The codepoints of the cell: the base codepoint followed by any combining
/// marks or ZWJ sequence members, which `RenderCell.codepoint` alone drops.
/// Copies them into `dst` and returns how many were written; zero for an
/// empty cell or an empty reference. `error.NoSpaceLeft` if `dst` is shorter
/// than the cluster.
pub fn gridRefGraphemes(self: *const GridRef, dst: []u32) error{NoSpaceLeft}!usize {
    const pin = self.live() orelse return 0;
    return graphemesFromPin(pin, dst);
}

/// The URI of the hyperlink (OSC 8) the cell is part of, or null when the
/// cell is not a link or the reference is empty.
pub fn gridRefHyperlinkUri(self: *const GridRef, gpa: Allocator) Allocator.Error!?[]const u8 {
    const pin = self.live() orelse return null;
    return hyperlinkFromPin(pin, gpa);
}

/// The cell at `x`, `y` of the active screen, in the coordinate system `tag`
/// names, without tracking it. Null if there is no such cell. For a read
/// that happens once, such as what is under a click; a cell that is read
/// again after the terminal changes wants a `GridRef`.
pub fn cellAt(t: *Terminal, tag: PointTag, x: u16, y: u32) ?RenderCell {
    const pin = t.screens.active.pages.pin(toPoint(tag, x, y)) orelse return null;
    return cellFromPin(t, pin);
}

/// The hyperlink URI of the cell at `x`, `y` of the active screen, or null
/// when the cell is not a link or there is no such cell.
pub fn hyperlinkAt(t: *Terminal, gpa: Allocator, tag: PointTag, x: u16, y: u32) Allocator.Error!?[]const u8 {
    const pin = t.screens.active.pages.pin(toPoint(tag, x, y)) orelse return null;
    return hyperlinkFromPin(pin, gpa);
}

fn cellFromPin(t: *const Terminal, pin: vt.Pin) RenderCell {
    const cell = pin.rowAndCell().cell;
    const page = pin.node.page();
    var out: RenderCell = .{
        .codepoint = 0,
        .fg = packColor(t.colors.foreground.get() orelse .{ .r = 0xff, .g = 0xff, .b = 0xff }),
        .bg = packColor(t.colors.background.get() orelse .{ .r = 0, .g = 0, .b = 0 }),
        .flags = .{ .wide = cell.wide },
    };
    switch (cell.content_tag) {
        .codepoint, .codepoint_grapheme => out.codepoint = cell.content.codepoint.data,
        .bg_color_palette => out.bg = packColor(t.colors.palette.current[cell.content.color_palette.data]),
        .bg_color_rgb => out.bg = packColor(.{
            .r = cell.content.color_rgb.r,
            .g = cell.content.color_rgb.g,
            .b = cell.content.color_rgb.b,
        }),
    }
    if (cell.style_id != 0) {
        const style = page.styles.get(page.memory, cell.style_id).*;
        if (resolveColor(t, style.fg_color)) |c| out.fg = c;
        if (resolveColor(t, style.bg_color)) |c| out.bg = c;
        out.flags = render.mergeFlags(out.flags, style.flags);
        if (style.flags.inverse) {
            const tmp = out.fg;
            out.fg = out.bg;
            out.bg = tmp;
        }
    }
    return out;
}

fn resolveColor(t: *const Terminal, c: vt.Style.Color) ?u32 {
    return switch (c) {
        .none => null,
        .palette => |i| packColor(t.colors.palette.current[i]),
        .rgb => |v| packColor(v),
    };
}

fn graphemesFromPin(pin: vt.Pin, dst: []u32) error{NoSpaceLeft}!usize {
    const cell = pin.rowAndCell().cell;
    if (!cell.hasText()) return 0;
    const extra: []const u21 = if (cell.hasGrapheme()) pin.grapheme(cell) orelse &.{} else &.{};
    const total = 1 + extra.len;
    if (dst.len < total) return error.NoSpaceLeft;
    dst[0] = cell.codepoint();
    for (extra, dst[1..total]) |cp, *out| out.* = cp;
    return total;
}

fn hyperlinkFromPin(pin: vt.Pin, gpa: Allocator) Allocator.Error!?[]const u8 {
    const cell = pin.rowAndCell().cell;
    if (!cell.hyperlink) return null;
    const page = pin.node.page();
    const id = page.lookupHyperlink(cell) orelse return null;
    const entry = page.hyperlink_set.get(page.memory, id);
    return try gpa.dupe(u8, entry.uri.slice(page.memory));
}
