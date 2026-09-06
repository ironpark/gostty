//! Kitty's drag and drop protocol (OSC 72), both directions.
//!
//! A program running in the terminal can register to receive native drags: the
//! user drags a file over the window, the terminal tells the program where the
//! pointer is and what it could hand over, the program says whether it wants
//! it, and on drop the terminal holds the data until the program has read it.
//!
//! That makes this the one part of the binding where the embedder is not just
//! draining what a program did, but driving a conversation with it. The
//! terminal side is `terminal.kitty_dnd`, allocated when a program registers
//! and freed when it unregisters; every call here is a no-op while it is null,
//! which is the normal state.
//!
//! Everything written goes into the stream's reply buffer, so it leaves with
//! the next `writeReplies` like any other answer.
const std = @import("std");
const vt = @import("ghostty_vt");
const common = @import("common.zig");
const stream_mod = @import("stream.zig");

const Allocator = std.mem.Allocator;
const Terminal = common.Terminal;
const Stream = stream_mod.Stream;
const dnd = vt.kitty.dnd;

/// What changed on the program's side of a drag.
///
/// Delivered to the handler `onDrag` installs, with the payload as arguments
/// rather than as accessors to read afterwards: everything here is a scalar,
/// so the signature can carry it, and a handler that needs no follow-up call
/// cannot pair the wrong payload with the wrong event.
pub const DragEvent = enum(u8) {
    /// The program registered to accept drops, re-registered, or unregistered.
    /// `dragActive` says which, and `dragRegisteredMimes` lists the types it
    /// asked for so they can be registered with the window system.
    registration,
    /// The program answered the drag currently over the terminal. The
    /// `accepted` argument carries the answer.
    acceptance,
    /// The program finished with the drop and did nothing with it.
    concluded_none,
    /// The program copied the drop.
    concluded_copy,
    /// The program moved the drop.
    concluded_move,
};

/// What a drag source offers, or what a program did with a drop.
pub const DragOperation = enum(u8) {
    none,
    copy,
    move,
};

/// Which operations the drag source allows. A `packed struct` so Go sees named
/// fields and the C ABI sees one byte.
pub const DragOperations = packed struct(u8) {
    copy: bool = false,
    move: bool = false,
    _pad: u6 = 0,
};

/// Where a native drag is over the terminal, and what it allows.
pub const DragMove = extern struct {
    /// The cell under the pointer, zero-based from the top left.
    cell_x: u32 = 0,
    cell_y: u32 = 0,
    /// The pointer in pixels, relative to the top left of the content area.
    pixel_x: i32 = 0,
    pixel_y: i32 = 0,
    operations: DragOperations = .{},

    fn toGhostty(self: DragMove) dnd.State.MoveEvent {
        return .{
            .cell_x = self.cell_x,
            .cell_y = self.cell_y,
            .pixel_x = self.pixel_x,
            .pixel_y = self.pixel_y,
            .operations = .{ .copy = self.operations.copy, .move = self.operations.move },
        };
    }
};

/// Called once per drag event, on the thread that fed the stream.
///
/// Carries no payload: zigo spells a callback parameter as the wire scalar, so
/// a registered enum cannot ride in the signature and `DragEvent` would arrive
/// as a bare integer. The event and the program's answer are read off the
/// stream instead, the way a clipboard request is.
///
/// They are still captured at the moment the effect fires, not read live, so a
/// second event in the same feed cannot overwrite the first one's answer.
pub const DragFn = *const fn (userdata: usize) callconv(.c) void;

fn toEvent(event: dnd.Event) DragEvent {
    return switch (event) {
        .registration => .registration,
        .acceptance => .acceptance,
        .concluded_none => .concluded_none,
        .concluded_copy => .concluded_copy,
        .concluded_move => .concluded_move,
    };
}

fn toOperation(op: dnd.Operation) DragOperation {
    return switch (op) {
        .none => .none,
        .copy => .copy,
        .move => .move,
    };
}

/// The effect ghostty calls. The acceptance is captured here, while the event
/// being reported is the current one, rather than left for the handler to read
/// afterwards when a second event in the same feed would have replaced it.
pub fn onDragEffect(handler: *vt.TerminalStream.Handler, event: dnd.Event) void {
    const self = stream_mod.streamFromHandler(handler);
    const callback = self.on_drag orelse return;
    self.drag_event = toEvent(event);
    self.drag_accepted = null;
    if (handler.terminal.kitty_dnd) |state| {
        if (state.clientAccepted()) |op| self.drag_accepted = toOperation(op);
    }
    callback(self.drag_userdata);
}

/// The event the running handler was called for. Only meaningful inside the
/// handler; `registration` outside one.
pub fn dragEvent(self: *Stream) DragEvent {
    return self.drag_event;
}

/// What the program answered about the drag, as of the event the handler is
/// running for. Null before it has answered, which is not the same as `none`
/// -- that is a refusal.
pub fn dragAccepted(self: *Stream) ?DragOperation {
    return self.drag_accepted;
}

/// Handle drag and drop events from the running program. Without a handler
/// the protocol still works -- the terminal answers the program's queries --
/// but nothing connects it to the window system, so no native drag reaches it.
pub fn onDrag(self: *Stream, callback: DragFn, userdata: usize) void {
    self.on_drag = callback;
    self.drag_userdata = userdata;
}

/// Whether a program is currently registered to accept drops.
///
/// False is the normal state, and the answer to "should I hand this drag to
/// the program or handle it myself".
pub fn dragActive(self: *Stream) bool {
    return terminalOf(self).kitty_dnd != null;
}

/// The MIME types the registered program asked the window system to accept,
/// space separated, empty when it named none (the common case).
///
/// Space separated because that is how the protocol carries it and how
/// `dragMove` takes it back; MIME types cannot contain a space.
pub fn dragRegisteredMimes(self: *Stream) []const u8 {
    const state = terminalOf(self).kitty_dnd orelse return "";
    return state.drop.registered_mimes.items;
}

/// What the program answered about the drag currently over the terminal, or
/// null before it has answered. `none` is a refusal, which is not the same as
/// no answer: an embedder that has no answer yet should show its own default.
pub fn dragClientAccepted(self: *Stream) ?DragOperation {
    const state = terminalOf(self).kitty_dnd orelse return null;
    return toOperation(state.clientAccepted() orelse return null);
}

/// Tell the program a native drag is over the terminal, offering `mimes`
/// (space separated) if it is dropped.
///
/// The order matters: it is what the program's data requests index into, and
/// what `dragAddItem` has to match at drop time. A no-op when no program is
/// registered.
pub fn dragMove(self: *Stream, ev: DragMove, mimes: []const u8) !void {
    const state = terminalOf(self).kitty_dnd orelse return;
    var list = mimeList(mimes);
    var buf: [1024]u8 = undefined;
    var writer: std.Io.Writer = .fixed(&buf);
    try state.dragMove(allocatorOf(self), &writer, ev.toGhostty(), list.slice());
    try appendReply(self, writer.buffered());
}

/// Tell the program the drag left without dropping. A no-op after a drop:
/// some window systems report the drop itself as a leave, and the data has to
/// survive until the program is done with it.
pub fn dragLeave(self: *Stream) !void {
    const state = terminalOf(self).kitty_dnd orelse return;
    var buf: [256]u8 = undefined;
    var writer: std.Io.Writer = .fixed(&buf);
    try state.dragLeave(allocatorOf(self), &writer);
    try appendReply(self, writer.buffered());
}

/// Stage one representation of a drop.
///
/// Staged rather than passed to `dragDrop` because a drop is a list of
/// (type, bytes) pairs and a slice of slices has no C representation. Call it
/// once per representation, in the order `dragMove` offered them, then
/// `dragDrop`. The bytes are copied.
pub fn dragAddItem(self: *Stream, mime: []const u8, data: []const u8) !void {
    if (self.drag_items.items.len >= dnd.State.max_items) return error.NoSpaceLeft;
    const gpa = allocatorOf(self);
    const mime_copy = try gpa.dupe(u8, mime);
    errdefer gpa.free(mime_copy);
    const data_copy = try gpa.dupe(u8, data);
    errdefer gpa.free(data_copy);
    try self.drag_items.append(gpa, .{ .mime = mime_copy, .data = data_copy });
}

/// Drop the staged representations onto the program.
///
/// The terminal copies and holds them until the program has read what it
/// wants and concluded, which arrives as a `concluded_*` event. The staging
/// list is emptied either way, so a failed drop does not leak into the next.
pub fn dragDrop(self: *Stream, ev: DragMove) !void {
    defer clearDragItems(self);
    const state = terminalOf(self).kitty_dnd orelse return;
    var buf: [1024]u8 = undefined;
    var writer: std.Io.Writer = .fixed(&buf);
    try state.dragDrop(allocatorOf(self), &writer, ev.toGhostty(), self.drag_items.items);
    try appendReply(self, writer.buffered());
}

/// Drop the staged representations without sending anything. For a drag the
/// window system cancelled after the embedder had already staged it.
pub fn dragClearItems(self: *Stream) void {
    clearDragItems(self);
}

pub fn clearDragItems(self: *Stream) void {
    const gpa = allocatorOf(self);
    for (self.drag_items.items) |item| {
        gpa.free(item.mime);
        gpa.free(item.data);
    }
    self.drag_items.clearRetainingCapacity();
}

/// Split a space-separated MIME list into the slice ghostty wants. Bounded by
/// the protocol's own item cap, so it needs no allocation.
const MimeList = struct {
    buf: [dnd.State.max_items][]const u8 = undefined,
    len: usize = 0,

    fn slice(self: *MimeList) []const []const u8 {
        return self.buf[0..self.len];
    }
};

fn mimeList(mimes: []const u8) MimeList {
    var result: MimeList = .{};
    var it = std.mem.tokenizeScalar(u8, mimes, ' ');
    while (it.next()) |mime| {
        if (result.len == result.buf.len) break;
        result.buf[result.len] = mime;
        result.len += 1;
    }
    return result;
}

fn terminalOf(self: *Stream) *Terminal {
    return self.inner.handler.terminal;
}

fn allocatorOf(self: *Stream) Allocator {
    return self.gpa;
}

fn appendReply(self: *Stream, bytes: []const u8) !void {
    try self.replies.appendSlice(self.gpa, bytes);
}
