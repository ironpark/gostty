//! Screens, selections and search.
//!
//! `Screen` and `Search` are ghostty's own types, bound directly. What is here
//! is the calls whose values hold page pins -- tracked positions in the
//! scrollback -- which cannot cross a C ABI. `Selection` is the same
//! information as plain screen coordinates, and the `select*` wrappers apply
//! ghostty's result rather than handing it back.
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
const ScreenKey = common.ScreenKey;

/// Which xterm alternate-screen mode a DEC private mode switch selects.
pub const SwitchScreenMode = vt.Terminal.SwitchScreenMode;

/// The screen the terminal is currently writing to.
pub fn activeScreen(self: *Terminal) *Screen {
    return self.screens.active;
}

/// A specific screen, or absent if the terminal has not created it yet. The
/// alternate screen only exists once something has switched to it.
pub fn screen(self: *Terminal, key: ScreenKey) ?*Screen {
    return self.screens.get(key);
}

/// Select the whole screen. Returns false when there is nothing to select.
pub fn screenSelectAll(self: *Screen) !bool {
    const selection = self.selectAll() orelse return false;
    try self.select(selection);
    return true;
}

/// Select the cells between two viewport positions, inclusive of both ends.
///
/// `rectangle` selects the block between the two corners rather than the flow
/// of text from one to the other. Returns false when either end is outside the
/// viewport, which is what a drag that left the window looks like.
pub fn screenSelectRange(
    self: *Screen,
    x1: u16,
    y1: u16,
    x2: u16,
    y2: u16,
    rectangle: bool,
) !bool {
    const start = self.pages.pin(.{ .viewport = .{ .x = x1, .y = y1 } }) orelse return false;
    const end = self.pages.pin(.{ .viewport = .{ .x = x2, .y = y2 } }) orelse return false;
    try self.select(vt.Selection.init(start, end, rectangle));
    return true;
}

/// A selection in screen coordinates: `y` counts rows from the top of the
/// scrollback, so a selection stays valid as the viewport scrolls. Convert to
/// viewport rows with `screenViewportTop`. `start` and `end` are in the order
/// the selection was made, which may be backwards.
///
/// ghostty's own `Selection` holds tracked pins into page memory, which cannot
/// cross the C boundary; this is the same information as coordinates.
pub const Selection = extern struct {
    start_x: u16,
    start_y: u32,
    end_x: u16,
    end_y: u32,
    /// A rectangle between the two corners rather than a run of lines.
    rectangle: bool,

    fn fromPins(pages: *const vt.PageList, start: vt.Pin, end: vt.Pin, rectangle: bool) ?Selection {
        const s = pages.pointFromPin(.screen, start) orelse return null;
        const e = pages.pointFromPin(.screen, end) orelse return null;
        return .{
            .start_x = s.screen.x,
            .start_y = s.screen.y,
            .end_x = e.screen.x,
            .end_y = e.screen.y,
            .rectangle = rectangle,
        };
    }
};

/// The screen's current selection, or null when there is none.
pub fn screenSelection(self: *Screen) ?Selection {
    const sel = self.selection orelse return null;
    return Selection.fromPins(&self.pages, sel.start(), sel.end(), sel.rectangle);
}

/// Replace the screen's selection. Returns false, leaving the selection as it
/// was, if either end is outside the screen.
pub fn screenSetSelection(self: *Screen, sel: Selection) !bool {
    try self.select(selectionToPins(self, sel) orelse return false);
    return true;
}

/// How `screenSelectionAdjust` moves the end of a selection.
pub const SelectionAdjustment = vt.Selection.Adjustment;

pub fn selectionToPins(self: *Screen, sel: Selection) ?vt.Selection {
    const start = self.pages.pin(.{ .screen = .{ .x = sel.start_x, .y = sel.start_y } }) orelse return null;
    const end = self.pages.pin(.{ .screen = .{ .x = sel.end_x, .y = sel.end_y } }) orelse return null;
    return vt.Selection.init(start, end, sel.rectangle);
}

/// Whether the screen cell at `x`, `y` (screen coordinates) is inside `sel`.
/// False when either is outside the screen.
pub fn screenSelectionContains(self: *Screen, sel: Selection, x: u16, y: u32) bool {
    const inner = selectionToPins(self, sel) orelse return false;
    const pin = self.pages.pin(.{ .screen = .{ .x = x, .y = y } }) orelse return false;
    return inner.contains(self, pin);
}

/// Move the end of `sel` by `adjustment` -- what shift+arrow does to a
/// selection -- and return the result. Null if `sel` is outside the screen.
pub fn screenSelectionAdjust(self: *Screen, sel: Selection, adjustment: SelectionAdjustment) ?Selection {
    var inner = selectionToPins(self, sel) orelse return null;
    inner.adjust(self, adjustment);
    return Selection.fromPins(&self.pages, inner.start(), inner.end(), inner.rectangle);
}

/// The screen row shown at the top of the viewport: subtract it from a
/// `Selection` row to get the viewport row a renderer draws at.
pub fn screenViewportTop(self: *Screen) u32 {
    const top = self.pages.pointFromPin(.screen, self.pages.getTopLeft(.viewport)) orelse return 0;
    return top.screen.y;
}

/// A text search over one screen, including its scrollback.
///
/// A child of the screen it reads, which is itself borrowed from a terminal, so
/// the close order is search, then terminal.
pub const Search = vt.search.Screen;

/// Which way `Search.select` moves.
///
/// Named `SearchDirection` rather than mirroring ghostty's `Select`: the C
/// typedef for a `SearchSelect` would be `zg_search_select`, colliding with the
/// function symbol for `Search.select`.
pub const SearchDirection = vt.search.Screen.Select;

/// Copy the matches found so far into `dst`, most recent screen content
/// first, and return how many were written. Matches are in screen
/// coordinates; size `dst` from `Search.matchesLen`.
pub fn searchMatches(self: *Search, dst: []Selection) usize {
    var written: usize = 0;
    const total = self.matchesLen();
    var i: usize = 0;
    while (i < total and written < dst.len) : (i += 1) {
        const match = self.matchAt(i) orelse continue;
        const bounds = match.untracked();
        dst[written] = Selection.fromPins(&self.screen.pages, bounds.start, bounds.end, false) orelse continue;
        written += 1;
    }
    return written;
}

/// The match `Search.select` last moved to, or null before the first move.
pub fn searchSelectedMatch(self: *Search) ?Selection {
    const match = self.selectedMatch() orelse return null;
    const bounds = match.untracked();
    return Selection.fromPins(&self.screen.pages, bounds.start, bounds.end, false);
}

/// Select the word under a viewport position -- what a double click does.
///
/// `boundaries` are the codepoints that end a word. ghostty has no default for
/// them on purpose: its own UI reads the set from configuration, so the choice
/// belongs to the embedder.
///
/// Wrapped because ghostty returns the `Selection` rather than applying it, and
/// a `Selection` holds page pins that cannot cross the C ABI. False when the
/// position is outside the viewport or there is no word under it.
pub fn screenSelectWord(self: *Screen, x: u16, y: u16, boundaries: []const u21) !bool {
    const pin = self.pages.pin(.{ .viewport = .{ .x = x, .y = y } }) orelse return false;
    const selection = self.selectWord(pin, boundaries) orelse return false;
    try self.select(selection);
    return true;
}

/// Select the line under a viewport position -- what a triple click does.
///
/// Soft-wrapped lines are followed as one line, leading and trailing whitespace
/// is trimmed, and a semantic prompt boundary ends the selection.
pub fn screenSelectLine(self: *Screen, x: u16, y: u16) !bool {
    const pin = self.pages.pin(.{ .viewport = .{ .x = x, .y = y } }) orelse return false;
    const selection = self.selectLine(.{ .pin = pin }) orelse return false;
    try self.select(selection);
    return true;
}

/// Select the command output the given position belongs to.
///
/// Needs the shell to mark its prompts with OSC 133; without those marks there
/// is no output block to find and this returns false.
pub fn screenSelectOutput(self: *Screen, x: u16, y: u16) !bool {
    const pin = self.pages.pin(.{ .viewport = .{ .x = x, .y = y } }) orelse return false;
    const selection = self.selectOutput(pin) orelse return false;
    try self.select(selection);
    return true;
}

/// True when the screen has a selection.
pub fn screenHasSelection(self: *Screen) bool {
    return self.selection != null;
}

/// The text of the current selection, absent when nothing is selected.
pub fn screenSelectionString(self: *Screen, gpa: Allocator) !?[]const u8 {
    const selection = self.selection orelse return null;
    return try self.selectionString(gpa, .{ .sel = selection });
}

/// Open a hyperlink on the screen; cells printed until `Screen.endHyperlink`
/// carry it. An empty `id` leaves the link without an explicit id, which is
/// what OSC 8 does when the parameter is absent.
pub fn screenStartHyperlink(self: *Screen, uri: []const u8, id: []const u8) !void {
    try self.startHyperlink(uri, if (id.len == 0) null else id);
}
