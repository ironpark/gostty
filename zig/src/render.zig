//! One frame of the viewport, flattened for a renderer.
//!
//! `RenderState` is ghostty's own dirty-tracking snapshot, bound as a handle.
//! What is added here is the flat `RenderCell` array a frame crosses the
//! boundary as, with palette indices resolved and defaults filled in so Go
//! never carries a palette.
const std = @import("std");
const vt = @import("ghostty_vt");
const common = @import("common.zig");

const Allocator = std.mem.Allocator;
const packColor = common.packColor;
const Underline = common.Underline;

// -- Rendering -------------------------------------------------------------
//
// ghostty ships `RenderState` for exactly this: a stateful, dirty-tracking
// snapshot of the viewport built for renderers. It is bound as a handle, and
// the viewport is handed to Go as one flat array of `RenderCell` so a frame
// costs a single crossing.

pub const RenderState = vt.RenderState;

/// How wide a cell is, and whether it is a spacer another cell owns.
pub const CellWidth = vt.page.Cell.Wide;

/// Everything about a cell that is not a codepoint or a color.
///
/// A `packed struct` rather than a hand-packed integer: the bit layout is
/// ghostty's and Go should not have to know it. zigo mirrors the fields and
/// still passes one `u32` across the boundary.
pub const CellFlags = packed struct(u32) {
    bold: bool = false,
    italic: bool = false,
    faint: bool = false,
    blink: bool = false,
    inverse: bool = false,
    invisible: bool = false,
    strikethrough: bool = false,
    overline: bool = false,
    underline: Underline = .none,
    /// Narrow, wide, or a spacer the renderer should skip.
    wide: CellWidth = .narrow,
    /// Whether the cell falls inside the screen's selection.
    selected: bool = false,
    _pad: u18 = 0,
};

/// One cell of the viewport, flattened for the C ABI.
///
/// Colors are already resolved: palette indices are looked up in the render
/// state's palette and defaults are filled in from the terminal's own
/// foreground and background, so Go never has to carry a palette. `inverse`
/// is applied here too, for the same reason.
pub const RenderCell = extern struct {
    /// The cell's codepoint, or 0 for an empty cell. Only the first codepoint
    /// of a grapheme cluster; combining marks are not carried across.
    codepoint: u32,
    /// 0xRRGGBB.
    fg: u32,
    bg: u32,
    flags: CellFlags,
};

/// An empty render state, filled by the first `RenderState.update`. Returned
/// by value so zigo boxes it and `RenderState.deinit` frees it; ghostty spells
/// the empty state as a constant, which has no function to bind.
pub fn newRenderState() RenderState {
    return .empty;
}

/// How many `RenderCell`s `renderCells` needs: `rows * cols`.
pub fn renderCellCount(self: *RenderState) usize {
    return @as(usize, self.rows) * @as(usize, self.cols);
}

/// Flatten the viewport into `dst`, row-major from the top, and report how
/// many cells were written.
pub fn renderCells(self: *RenderState, dst: []RenderCell) !usize {
    const width: usize = self.cols;
    const total = @as(usize, self.rows) * width;
    if (dst.len < total) return error.NoSpaceLeft;
    const defaults = rowDefaults(self);
    for (0..self.rows) |y| fillRow(self, defaults, y, dst[y * width .. (y + 1) * width]);
    return total;
}

/// Flatten one viewport row into `dst` and report how many cells were
/// written: `cols`, or zero for a row off the grid. For a renderer that
/// redraws only the rows `renderDirtyRows` names.
pub fn renderRowCells(self: *RenderState, y: u16, dst: []RenderCell) !usize {
    if (y >= self.rows) return 0;
    const width: usize = self.cols;
    if (dst.len < width) return error.NoSpaceLeft;
    fillRow(self, rowDefaults(self), y, dst[0..width]);
    return width;
}

/// What every cell of a frame starts from: the colors the terminal falls back
/// to, resolved once per frame rather than once per row.
const RowDefaults = struct { fg: u32, bg: u32 };

fn rowDefaults(self: *RenderState) RowDefaults {
    return .{
        .fg = packColor(self.colors.foreground),
        .bg = packColor(self.colors.background),
    };
}

fn fillRow(self: *RenderState, defaults: RowDefaults, y: usize, dst: []RenderCell) void {
    const row = self.row_data.items(.cells)[y];
    const raw = row.items(.raw);
    const styles = row.items(.style);
    const n = @min(raw.len, dst.len);
    for (raw[0..n], styles[0..n], dst[0..n]) |cell, style, *out| {
        out.* = .{
            .codepoint = 0,
            .fg = defaults.fg,
            .bg = defaults.bg,
            .flags = .{ .wide = cell.wide },
        };

        switch (cell.content_tag) {
            .codepoint, .codepoint_grapheme => out.codepoint = cell.content.codepoint.data,
            // A cell with no text but a background color; the color is in
            // the cell itself rather than the style map.
            .bg_color_palette => out.bg = packColor(self.colors.palette[cell.content.color_palette.data]),
            .bg_color_rgb => out.bg = packColor(.{
                .r = cell.content.color_rgb.r,
                .g = cell.content.color_rgb.g,
                .b = cell.content.color_rgb.b,
            }),
        }

        // `style` is only meaningful when the cell carries a style id;
        // the default-styled cells keep the defaults filled in above.
        if (cell.style_id != 0) {
            if (resolveColor(self, style.fg_color)) |c| out.fg = c;
            if (resolveColor(self, style.bg_color)) |c| out.bg = c;
            out.flags = mergeFlags(out.flags, style.flags);
            if (style.flags.inverse) {
                const tmp = out.fg;
                out.fg = out.bg;
                out.bg = tmp;
            }
        }
    }

    // The selection is one span per row, so mark it in one pass over the span
    // rather than testing every cell against it.
    if (self.row_data.items(.selection)[y]) |range| {
        const end = @min(@as(usize, range[1]) + 1, n);
        if (range[0] < end) for (dst[range[0]..end]) |*out| {
            out.flags.selected = true;
        };
    }
}

/// How much of the render state changed since it was last cleaned.
pub const RenderDirty = enum(u8) {
    /// Nothing: a renderer can skip the frame.
    clean,
    /// Some rows; `renderDirtyRows` names them.
    partial,
    /// Everything: colors or dimensions changed, so every row needs drawing.
    full,
};

/// What changed since `RenderState.clean`. `RenderState.update` raises this;
/// nothing lowers it but `clean`.
pub fn renderDirty(self: *RenderState) RenderDirty {
    return switch (self.dirty) {
        .false => .clean,
        .partial => .partial,
        .full => .full,
    };
}

/// Write the indexes of the dirty viewport rows into `dst`, top to bottom,
/// and report how many there are. `error.NoSpaceLeft` if `dst` is shorter
/// than that; size it to `rows`.
pub fn renderDirtyRows(self: *RenderState, dst: []u16) !usize {
    var n: usize = 0;
    for (self.row_data.items(.dirty)[0..self.rows], 0..) |dirty, y| {
        if (!dirty) continue;
        if (n >= dst.len) return error.NoSpaceLeft;
        dst[n] = @intCast(y);
        n += 1;
    }
    return n;
}

/// The codepoints of the cell at viewport `x`, `y`: the base codepoint
/// followed by any combining marks or ZWJ sequence members, which
/// `RenderCell.codepoint` alone drops. Copies them into `dst` and returns
/// how many were written; zero for an empty cell or a position off the grid.
/// `error.NoSpaceLeft` if `dst` is shorter than the cluster.
pub fn renderGraphemes(self: *RenderState, x: u16, y: u16, dst: []u32) !usize {
    if (x >= self.cols or y >= self.rows) return 0;
    const cells = self.row_data.items(.cells)[y];
    const cell = cells.items(.raw)[x];
    switch (cell.content_tag) {
        .codepoint => {
            // A blank cell is a codepoint of zero, the same rule `renderCells`
            // applies.
            if (cell.content.codepoint.data == 0) return 0;
            if (dst.len < 1) return error.NoSpaceLeft;
            dst[0] = cell.content.codepoint.data;
            return 1;
        },
        .codepoint_grapheme => {
            const extra = cells.items(.grapheme)[x];
            if (dst.len < 1 + extra.len) return error.NoSpaceLeft;
            dst[0] = cell.content.codepoint.data;
            for (dst[1 .. 1 + extra.len], extra) |*out, cp| out.* = cp;
            return 1 + extra.len;
        },
        else => return 0,
    }
}

/// The OSC 8 hyperlink under viewport `x`, `y`, or null when the cell has
/// none. Valid only until the terminal changes: like ghostty's own
/// `linkCells`, this reads page memory through the pins `RenderState.update`
/// captured, so call it right after an update.
pub fn renderHyperlinkAt(self: *RenderState, gpa: Allocator, x: u16, y: u16) !?[]const u8 {
    if (x >= self.cols or y >= self.rows) return null;
    const pin = self.row_data.items(.pin)[y];
    const pg = pin.node.page();
    const rac = pg.getRowAndCell(x, pin.y);
    if (!rac.cell.hyperlink) return null;
    const id = pg.lookupHyperlink(rac.cell) orelse return null;
    const entry = pg.hyperlink_set.get(pg.memory, id);
    return try gpa.dupe(u8, entry.uri.slice(pg.memory));
}

fn resolveColor(self: *RenderState, c: vt.Style.Color) ?u32 {
    return switch (c) {
        .none => null,
        .palette => |i| packColor(self.colors.palette[i]),
        .rgb => |v| packColor(v),
    };
}

/// Fold ghostty's style flags into ours, leaving the fields this binding owns
/// (`wide`, `selected`) as they were.
pub fn mergeFlags(out: CellFlags, f: anytype) CellFlags {
    var merged = out;
    merged.bold = f.bold;
    merged.italic = f.italic;
    merged.faint = f.faint;
    merged.blink = f.blink;
    merged.inverse = f.inverse;
    merged.invisible = f.invisible;
    merged.strikethrough = f.strikethrough;
    merged.overline = f.overline;
    merged.underline = f.underline;
    return merged;
}

/// The terminal's default background, 0xRRGGBB. Already reversed if the
/// terminal is in reverse-video mode.
pub fn renderBackground(self: *RenderState) u32 {
    return packColor(self.colors.background);
}

pub fn renderForeground(self: *RenderState) u32 {
    return packColor(self.colors.foreground);
}

/// The cursor's column within the viewport, or false if it is scrolled out.
pub fn renderCursorX(self: *RenderState) ?u16 {
    const vp = self.cursor.viewport orelse return null;
    return vp.x;
}

pub fn renderCursorY(self: *RenderState) ?u16 {
    const vp = self.cursor.viewport orelse return null;
    return vp.y;
}

/// Whether the cursor sits on the tail of a wide character. A renderer that
/// draws a one-cell cursor may want to move it back one column so it covers
/// the character rather than half of it. Null when the cursor is scrolled out
/// of the viewport, the same as `renderCursorX`.
pub fn renderCursorWideTail(self: *RenderState) ?bool {
    const vp = self.cursor.viewport orelse return null;
    return vp.wide_tail;
}

/// The cursor color as of this frame, 0xRRGGBB, or null when the program has
/// not set one and the renderer should pick. Read from the snapshot rather
/// than the terminal so it matches the cells drawn beside it.
pub fn renderCursorColor(self: *RenderState) ?u32 {
    return packColor(self.colors.cursor orelse return null);
}
