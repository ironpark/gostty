//! The libghostty-vt surface exposed to Go.
//!
//! `Terminal`, `Screen`, `Search` and `RenderState` are ghostty's own types,
//! bound directly: their methods become Go methods without any wrapper. This
//! module adds only what ghostty does not provide and zigo needs:
//!
//!   - an `std.Io` value for `.io` injection (ghostty ships `TinyIo` as a type,
//!     not as a ready-made `Io` declaration);
//!   - a release function for the strings bound with `.returns = .caller`;
//!   - wrappers where ghostty's signature cannot cross the C ABI (page pins,
//!     nested optional structs, generic functions), each saying why.
//!
//! Plain field reads are not here: `.fields` in `bindings.zig` generates those
//! accessors from the field path.
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

/// Something a running program asked the embedder to do, rather than a change
/// to terminal state.
///
/// ghostty reports these through `Handler.effects`, a struct of function
/// pointers called from inside a feed with payloads borrowed for the duration
/// of the call. Rather than reflect that into Go -- where a callback signature
/// is a raw C signature and the borrow would be a trap -- the stream copies
/// each one into a queue that `nextEvent` drains after the feed returns.
pub const StreamEvent = enum(u8) {
    /// BEL. No payload.
    bell,
    /// OSC 0/2. The new title is on the terminal.
    title_changed,
    /// OSC 7. The new working directory is on the terminal.
    pwd_changed,
    /// OSC 9 or 777. `eventTitle` and `eventBody` carry the text.
    desktop_notification,
    /// OSC 9;4. `eventProgressState` and `eventProgress` carry the report.
    progress_report,
};

/// How far along an OSC 9;4 progress report says the program is.
pub const ProgressState = vt.osc.Command.ProgressReport.State;

/// Which clipboard a request names.
pub const ClipboardLocation = vt.clipboard.Location;

/// Why a clipboard request was not served.
pub const ClipboardDenial = enum(u8) {
    /// Policy or the user said no.
    denied,
    /// This embedder cannot reach that clipboard.
    unsupported,
    /// The clipboard is temporarily unavailable.
    busy,
    /// Reading or writing the clipboard failed.
    io_error,
};

/// Called while a feed is in flight, once per clipboard request.
///
/// The callback carries no payload: a zigo callback signature is a raw C
/// signature, so slices would arrive as loose pointer and length pairs. It
/// instead reads the pending request off the stream and answers it there.
pub const ClipboardFn = *const fn (userdata: usize) callconv(.c) void;

/// A VT stream: parses escape sequences and applies them to a `Terminal`.
///
/// A stream borrows its terminal and must be closed before it: ghostty's
/// `Handler.deinit` reaches through the terminal for its allocator. The binding
/// declares the stream a child of its terminal, so closing the terminal first is
/// refused rather than left to the caller to get right.
pub const Stream = struct {
    inner: vt.TerminalStream,
    gpa: Allocator,

    /// Events collected during a feed, oldest first.
    queue: std.ArrayList(Queued) = .empty,

    /// Bytes the terminal answered a query with, waiting to be written back to
    /// the program. Collected during a feed and drained by `writeReplies`.
    replies: std.ArrayList(u8) = .empty,

    /// The clipboard request being answered, if a callback is running. Both
    /// are borrowed for the duration of the effect call and nulled after.
    pending_write: ?vt.clipboard.Write = null,
    pending_read: ?vt.clipboard.Read = null,
    /// Whether the callback answered. An unanswered request is denied so the
    /// program is not left waiting.
    answered: bool = false,

    on_clipboard_write: ?ClipboardFn = null,
    write_userdata: usize = 0,
    on_clipboard_read: ?ClipboardFn = null,
    read_userdata: usize = 0,
    /// What `nextEvent` last handed out. Its payload stays readable until the
    /// following `nextEvent`.
    current: ?Queued = null,

    const Queued = struct {
        kind: StreamEvent,
        /// Notification title. Owned.
        title: []const u8 = "",
        /// Notification body. Owned.
        body: []const u8 = "",
        progress_state: ProgressState = .remove,
        /// 0..100, or 255 when the report carried no percentage.
        progress: u8 = 255,

        fn deinit(self: Queued, gpa: Allocator) void {
            gpa.free(self.title);
            gpa.free(self.body);
        }
    };

    fn fromHandler(handler: *vt.TerminalStream.Handler) *Stream {
        const inner: *vt.TerminalStream = @fieldParentPtr("handler", handler);
        return @fieldParentPtr("inner", inner);
    }

    /// Dropping an event beats failing the feed: the terminal state the same
    /// sequence produced has already been applied.
    fn push(self: *Stream, event: Queued) void {
        self.queue.append(self.gpa, event) catch event.deinit(self.gpa);
    }

    fn onBell(handler: *vt.TerminalStream.Handler) void {
        fromHandler(handler).push(.{ .kind = .bell });
    }

    fn onTitleChanged(handler: *vt.TerminalStream.Handler) void {
        fromHandler(handler).push(.{ .kind = .title_changed });
    }

    fn onPwdChanged(handler: *vt.TerminalStream.Handler) void {
        fromHandler(handler).push(.{ .kind = .pwd_changed });
    }

    fn onDesktopNotification(
        handler: *vt.TerminalStream.Handler,
        notification: vt.TerminalStream.Action.ShowDesktopNotification,
    ) void {
        const self = fromHandler(handler);
        const title = self.gpa.dupe(u8, notification.title) catch return;
        const body = self.gpa.dupe(u8, notification.body) catch {
            self.gpa.free(title);
            return;
        };
        self.push(.{ .kind = .desktop_notification, .title = title, .body = body });
    }

    fn onProgressReport(
        handler: *vt.TerminalStream.Handler,
        report: vt.osc.Command.ProgressReport,
    ) void {
        fromHandler(handler).push(.{
            .kind = .progress_report,
            .progress_state = report.state,
            .progress = report.progress orelse 255,
        });
    }

    /// A query the terminal answered itself, such as a device status report or
    /// a Kitty graphics acknowledgement. The bytes are borrowed for the call,
    /// so they are copied out and handed over after the feed like the events;
    /// writing to the pty from inside a feed would reenter the caller.
    fn onWritePty(handler: *vt.TerminalStream.Handler, data: []const u8) void {
        const self = fromHandler(handler);
        // A dropped reply leaves the program waiting, but so does failing the
        // feed, and this way the rest of the screen still arrives.
        self.replies.appendSlice(self.gpa, data) catch {};
    }

    /// The geometry XTWINOPS (CSI 14/16/18 t) and in-band size reports answer
    /// with. Read off the terminal, which is where `resizeCells` put it.
    fn onSize(handler: *vt.TerminalStream.Handler) ?vt.size_report.Size {
        const term = handler.terminal;
        if (term.cols == 0 or term.rows == 0) return null;
        return .{
            .rows = term.rows,
            .columns = term.cols,
            .cell_width = @intCast(term.width_px / term.cols),
            .cell_height = @intCast(term.height_px / term.rows),
        };
    }

    fn onClipboardWrite(
        handler: *vt.TerminalStream.Handler,
        write: vt.clipboard.Write,
    ) void {
        const self = fromHandler(handler);
        const callback = self.on_clipboard_write orelse {
            write.reply(.denied);
            return;
        };
        self.pending_write = write;
        self.answered = false;
        defer self.pending_write = null;
        callback(self.write_userdata);
        if (!self.answered) write.reply(.denied);
    }

    fn onClipboardRead(
        handler: *vt.TerminalStream.Handler,
        read: vt.clipboard.Read,
    ) void {
        const self = fromHandler(handler);
        const callback = self.on_clipboard_read orelse {
            read.reply(.denied);
            return;
        };
        self.pending_read = read;
        self.answered = false;
        defer self.pending_read = null;
        callback(self.read_userdata);
        if (!self.answered) read.reply(.denied);
    }

    fn effects() vt.TerminalStream.Handler.Effects {
        var result: vt.TerminalStream.Handler.Effects = .readonly;
        result.bell = onBell;
        result.title_changed = onTitleChanged;
        result.pwd_changed = onPwdChanged;
        result.desktop_notification = onDesktopNotification;
        result.progress_report = onProgressReport;
        result.write_pty = onWritePty;
        result.size = onSize;
        result.clipboard_write = onClipboardWrite;
        result.clipboard_read = onClipboardRead;
        return result;
    }

    /// Feed bytes to the parser, applying them to the terminal.
    pub fn feed(self: *Stream, bytes: []const u8) void {
        self.inner.nextSlice(bytes);
    }

    /// Whether the terminal has answered a query since the last `writeReplies`.
    pub fn hasReplies(self: *Stream) bool {
        return self.replies.items.len > 0;
    }

    /// Write everything the terminal has answered to `writer` and forget it.
    ///
    /// These are the terminal's own replies -- device status, Kitty graphics
    /// acknowledgements, size reports -- and they go back to the program the
    /// same way a keystroke does. Nothing is written from inside a feed, so
    /// this belongs next to it: feed, then drain.
    /// Replies are cleared only after the writer flushes successfully. On
    /// failure the whole batch is kept; retrying a partially completed write
    /// can repeat bytes the writer already accepted.
    pub fn writeReplies(self: *Stream, writer: *std.Io.Writer) !void {
        if (self.replies.items.len == 0) return;
        try writer.writeAll(self.replies.items);
        try writer.flush();
        self.replies.clearRetainingCapacity();
    }

    /// Write the unfinished sequence suffix, when continuation tracking is on.
    pub fn writeContinuation(self: *Stream, writer: *std.Io.Writer) !void {
        try self.inner.writeContinuation(writer);
    }

    /// Write a snapshot of the terminal this stream feeds, with the
    /// stream's unfinished sequence so a restored stream can pick up
    /// mid-sequence.
    pub fn writeSnapshot(self: *Stream, writer: *std.Io.Writer) !void {
        var suffix: std.Io.Writer.Allocating = .init(self.gpa);
        defer suffix.deinit();
        const cont: vt.snapshot.Continuation = if (self.inner.ground()) .ground else blk: {
            self.inner.writeContinuation(&suffix.writer) catch |err| switch (err) {
                error.ContinuationDisabled, error.ContinuationUnavailable => break :blk .ground,
                else => |e| return e,
            };
            break :blk .{ .bytes = suffix.written() };
        };
        try vt.snapshot.encode(self.gpa, writer, self.inner.handler.terminal, .{ .continuation = cont });
    }

    /// Take the next event a feed produced, absent when the queue is empty.
    ///
    /// The payload accessors below describe the event this returned, until the
    /// next call.
    pub fn nextEvent(self: *Stream) ?StreamEvent {
        if (self.current) |event| {
            event.deinit(self.gpa);
            self.current = null;
        }
        if (self.queue.items.len == 0) return null;
        const event = self.queue.orderedRemove(0);
        self.current = event;
        return event.kind;
    }

    /// The current event's notification title, empty for other events.
    pub fn eventTitle(self: *Stream) []const u8 {
        const event = self.current orelse return "";
        return event.title;
    }

    /// The current event's notification body, empty for other events.
    pub fn eventBody(self: *Stream) []const u8 {
        const event = self.current orelse return "";
        return event.body;
    }

    pub fn eventProgressState(self: *Stream) ProgressState {
        const event = self.current orelse return .remove;
        return event.progress_state;
    }

    /// The current event's progress percentage, absent when the report carried
    /// none.
    pub fn eventProgress(self: *Stream) ?u8 {
        const event = self.current orelse return null;
        if (event.progress > 100) return null;
        return event.progress;
    }

    /// Handle clipboard writes (OSC 52 set, Kitty OSC 5522). Without a
    /// callback the terminal answers every write with `denied`.
    pub fn onClipboardWriteRequest(
        self: *Stream,
        callback: ClipboardFn,
        userdata: usize,
    ) void {
        self.on_clipboard_write = callback;
        self.write_userdata = userdata;
    }

    /// Handle clipboard reads (OSC 52 query, Kitty OSC 5522). Without a
    /// callback OSC 52 reads are ignored, which is the safe default: answering
    /// one lets the running program read the user's clipboard.
    pub fn onClipboardReadRequest(
        self: *Stream,
        callback: ClipboardFn,
        userdata: usize,
    ) void {
        self.on_clipboard_read = callback;
        self.read_userdata = userdata;
    }

    /// Which clipboard the pending request names.
    pub fn clipboardLocation(self: *Stream) ClipboardLocation {
        if (self.pending_write) |write| return write.location;
        if (self.pending_read) |read| return read.location;
        return .standard;
    }

    /// The requesting program's name, empty when the protocol carries none.
    pub fn clipboardName(self: *Stream) []const u8 {
        if (self.pending_write) |write| return write.name;
        if (self.pending_read) |read| return read.name;
        return "";
    }

    /// True when the terminal already holds a session grant, so the embedder
    /// should skip its permission prompt.
    pub fn clipboardGranted(self: *Stream) bool {
        if (self.pending_write) |write| return write.granted;
        if (self.pending_read) |read| return read.granted;
        return false;
    }

    /// True when the program supplied a session password, so a decision can be
    /// remembered via the `remember` argument when answering.
    pub fn clipboardCanRemember(self: *Stream) bool {
        if (self.pending_write) |write| return write.can_remember;
        if (self.pending_read) |read| return read.can_remember;
        return false;
    }

    /// How many representations a pending write carries. Zero clears the
    /// destination.
    pub fn clipboardContentCount(self: *Stream) usize {
        const write = self.pending_write orelse return 0;
        return write.contents.len;
    }

    /// The MIME type of one representation of a pending write.
    pub fn clipboardContentMime(self: *Stream, index: usize) []const u8 {
        const write = self.pending_write orelse return "";
        if (index >= write.contents.len) return "";
        return write.contents[index].mime;
    }

    /// The bytes of one representation of a pending write. Binary safe.
    pub fn clipboardContentData(self: *Stream, index: usize) []const u8 {
        const write = self.pending_write orelse return "";
        if (index >= write.contents.len) return "";
        return write.contents[index].data;
    }

    /// How many MIME types a pending read asks for, in order of preference.
    pub fn clipboardMimeCount(self: *Stream) usize {
        const read = self.pending_read orelse return 0;
        return read.mimes.len;
    }

    /// One of the MIME types a pending read asks for.
    pub fn clipboardMime(self: *Stream, index: usize) []const u8 {
        const read = self.pending_read orelse return "";
        if (index >= read.mimes.len) return "";
        return read.mimes[index];
    }

    /// Accept a pending write. Answering a read this way serves empty text.
    pub fn allowClipboard(self: *Stream, remember: bool) void {
        if (self.answered) return;
        if (self.pending_write) |write| {
            write.reply(.{ .success = .{ .remember = remember } });
            self.answered = true;
            return;
        }
        if (self.pending_read) |read| {
            read.reply(.{ .success = .{ .remember = remember } });
            self.answered = true;
        }
    }

    /// Serve a pending read with plain text.
    ///
    /// `text` is borrowed for this call only; the terminal copies what it
    /// needs before returning.
    pub fn replyClipboardText(self: *Stream, text: []const u8, remember: bool) void {
        if (self.answered) return;
        const read = self.pending_read orelse return;
        const contents: [1]vt.clipboard.Content = .{.{
            .mime = "text/plain",
            .data = text,
        }};
        read.reply(.{ .success = .{
            .contents = &contents,
            .remember = remember,
        } });
        self.answered = true;
    }

    /// Refuse a pending request.
    pub fn denyClipboard(self: *Stream, reason: ClipboardDenial) void {
        if (self.answered) return;
        if (self.pending_write) |write| {
            write.reply(switch (reason) {
                .denied => .denied,
                .unsupported => .unsupported,
                .busy => .busy,
                .io_error => .io_error,
            });
            self.answered = true;
            return;
        }
        if (self.pending_read) |read| {
            read.reply(switch (reason) {
                .denied => .denied,
                .unsupported => .unsupported,
                .busy => .busy,
                .io_error => .io_error,
            });
            self.answered = true;
        }
    }
};

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

/// Which of a terminal's screens is active.
pub const ScreenKey = vt.ScreenSet.Key;

/// Which xterm alternate-screen mode a DEC private mode switch selects.
pub const SwitchScreenMode = vt.Terminal.SwitchScreenMode;

/// One of a terminal's screens: its grid, scrollback and selection.
///
/// A screen is owned by its terminal, so handles to one are borrowed. They stay
/// valid while the terminal is open and are invalidated by its `Close`.
pub const Screen = vt.Screen;

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

fn selectionToPins(self: *Screen, sel: Selection) ?vt.Selection {
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

/// What `formatTerminal` and `Screen.format` emit. Declared here rather than
/// re-exported because ghostty's enum has a two-bit tag, which cannot sit in
/// the extern `FormatOptions`.
pub const FormatterFormat = enum(u8) {
    /// Plain text.
    plain,
    /// VT sequences that replay colors, styles and links; lines end in CRLF.
    vt,
    /// HTML with inline styles; palette colors become CSS variables unless
    /// `resolve_palette` is set.
    html,

    fn toGhostty(self: FormatterFormat) vt.formatter.Format {
        return switch (self) {
            .plain => .plain,
            .vt => .vt,
            .html => .html,
        };
    }
};

/// How the formatters render screen contents. Every field is off in its
/// zero value, so a Go `FormatOptions{}` is plain text, trimmed, with styles
/// and links included where the format can carry them.
pub const FormatOptions = extern struct {
    format: FormatterFormat = .plain,
    /// Join soft-wrapped lines back into one instead of emitting them as
    /// they are laid out at the current width.
    unwrap: bool = false,
    /// Keep trailing spaces on lines that have other text. Trailing blank
    /// lines are always dropped.
    keep_trailing_whitespace: bool = false,
    /// Include the cursor position. Styled formats only.
    cursor: bool = false,
    /// Leave out text styles. Styled formats only.
    no_styles: bool = false,
    /// Leave out OSC 8 hyperlinks. Styled formats only.
    no_hyperlinks: bool = false,
    /// Resolve palette indices to the terminal's current RGB values rather
    /// than emitting the index. Styled formats only.
    resolve_palette: bool = false,

    fn toGhostty(self: FormatOptions) vt.formatter.Options {
        return .{ .emit = self.format.toGhostty(), .unwrap = self.unwrap, .trim = !self.keep_trailing_whitespace };
    }

    fn screenExtra(self: FormatOptions) vt.formatter.ScreenFormatter.Extra {
        var extra: vt.formatter.ScreenFormatter.Extra = .none;
        extra.cursor = self.cursor;
        extra.style = !self.no_styles;
        extra.hyperlink = !self.no_hyperlinks;
        return extra;
    }
};

/// Format the active area -- the rows on screen, not the scrollback -- with
/// the terminal's colors and, for styled output, its palette, modes and
/// other state a replay needs. `Screen.format` covers the scrollback too.
pub fn formatTerminal(self: *Terminal, opts: FormatOptions, writer: *std.Io.Writer) !void {
    var f = vt.formatter.TerminalFormatter.init(self, opts.toGhostty());
    f.opts.background = self.colors.background.get();
    f.opts.foreground = self.colors.foreground.get();
    if (opts.resolve_palette) f.opts.palette = &self.colors.palette.current;
    try f.format(writer);
}

/// Format a whole screen, scrollback included. `Terminal.format` is the
/// active area only.
pub fn screenFormat(self: *Screen, opts: FormatOptions, writer: *std.Io.Writer) !void {
    var f = vt.formatter.ScreenFormatter.init(self, opts.toGhostty());
    f.extra = opts.screenExtra();
    try f.format(writer);
}

/// Format the part of a screen inside `sel`. Returns false, writing
/// nothing, if `sel` is outside the screen.
pub fn screenFormatSelection(self: *Screen, opts: FormatOptions, sel: Selection, writer: *std.Io.Writer) !bool {
    const inner = selectionToPins(self, sel) orelse return false;
    var f = vt.formatter.ScreenFormatter.init(self, opts.toGhostty());
    f.content = .{ .selection = inner };
    f.extra = opts.screenExtra();
    try f.format(writer);
    return true;
}

// Snapshots: ghostty's binary representation of a whole terminal, active
// state first so a restored terminal renders before its history arrives.
// Writing one is `Stream.writeSnapshot`, since the stream owns the
// unfinished-sequence state a snapshot carries.

/// A decoded snapshot: ghostty's own type, bound as a handle. Restore it into
/// a terminal with `restoreInto`, then feed `continuation` to a fresh stream
/// on it to resume mid-sequence.
pub const Snapshot = vt.snapshot.Decoded;

/// Decode a snapshot from `reader`. `max_continuation_bytes` bounds the
/// unfinished-sequence suffix the snapshot may carry.
///
/// Wrapped because ghostty's `decode` takes an options struct; returned by
/// value so zigo boxes it and `Snapshot.deinit` frees it.
pub fn decodeSnapshot(gpa: Allocator, reader: *std.Io.Reader, max_continuation_bytes: usize) !Snapshot {
    return try vt.snapshot.decode(gpa, io, reader, .{ .max_continuation_bytes = max_continuation_bytes });
}

/// Replace `term` with the terminal the snapshot holds: its size, screens,
/// scrollback, modes and colors. The snapshot gives its terminal up once;
/// a second call fails. Streams on `term` keep pointing at it, but their
/// parser state belongs to the old contents, so open a new stream and feed
/// it `continuation` before any new input.
///
/// zigo allows one constructor per handle and `newTerminal` is it, so a
/// restore fills a terminal the caller made rather than returning one.
pub fn snapshotRestoreInto(self: *Snapshot, gpa: Allocator, term: *Terminal) error{TerminalTaken}!void {
    if (self.terminal == null) return error.TerminalTaken;
    const restored = self.toOwned();
    term.deinit(gpa);
    term.* = restored;
}

/// The bytes of the unfinished sequence the snapshot was taken in, empty
/// when the stream was at ground. Feed them to the restored terminal's
/// stream before any new input.
pub fn snapshotContinuation(self: *Snapshot) []const u8 {
    return switch (self.continuation) {
        .ground => "",
        .bytes => |bytes| bytes,
    };
}

/// The underline style an `Attribute` selects.
pub const Underline = vt.Attribute.Underline;

/// One of the 16 named ANSI colors.
pub const ColorName = vt.color.Name;

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

/// Open a hyperlink on the screen; cells printed until `Screen.endHyperlink`
/// carry it. An empty `id` leaves the link without an explicit id, which is
/// what OSC 8 does when the parameter is absent.
pub fn screenStartHyperlink(self: *Screen, uri: []const u8, id: []const u8) !void {
    try self.startHyperlink(uri, if (id.len == 0) null else id);
}

/// The display width of a grapheme cluster given as codepoints.
///
/// Wrapped because `vt.unicode.graphemeWidth` is generic over the codepoint
/// integer type, and a generic function has no signature to bind.
pub fn graphemeWidth(cps: []const u32) u8 {
    return @intCast(vt.unicode.graphemeWidth(u32, cps).width);
}

// Colors. Everything is `0xRRGGBB`, the same packing `RenderCell` uses, so a
// renderer keeps one color representation.

fn packColor(c: vt.color.RGB) u32 {
    return (@as(u32, c.r) << 16) | (@as(u32, c.g) << 8) | c.b;
}

fn unpackColor(v: u32) vt.color.RGB {
    return .{ .r = @truncate(v >> 16), .g = @truncate(v >> 8), .b = @truncate(v) };
}

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

/// Unicode helpers, re-exported as-is.
pub const unicode = vt.unicode;

/// Input encoding: turning key, mouse and focus events into the bytes a
/// program reading the pty expects. `encodeFocus` and `isSafePaste` are
/// bound straight out of here; the others take a `Terminal` and need the
/// wrappers below.
pub const input = vt.input;

pub const Key = vt.input.Key;
/// What happened to the key. Declared here rather than re-exported so that
/// press is zero: `KeyEvent` is a value, and a Go literal that names only
/// the key should describe a press, which is what ghostty's own default is.
pub const KeyAction = enum(u8) {
    press,
    release,
    repeat,

    fn toGhostty(self: KeyAction) vt.input.KeyAction {
        return switch (self) {
            .press => .press,
            .release => .release,
            .repeat => .repeat,
        };
    }
};

// `Key` is an enum, and zigo binds functions on structs and opaque types only,
// so its helpers are re-exported as free functions here. They are for a
// caller mapping platform key codes onto the enum, or deciding whether a
// press can carry text.

/// The key for a printable ASCII byte, or null if none maps to it.
pub fn keyFromASCII(ch: u8) ?Key {
    return Key.fromASCII(ch);
}

/// The Unicode codepoint the key produces on a US layout, if it has one.
pub fn keyCodepoint(key: Key) ?u21 {
    return key.codepoint();
}

/// True for keys that produce text on a US layout.
pub fn keyPrintable(key: Key) bool {
    return key.printable();
}

/// True for modifier keys such as shift, control and alt.
pub fn keyModifier(key: Key) bool {
    return key.modifier();
}

/// True for keys on the numeric keypad.
pub fn keyKeypad(key: Key) bool {
    return key.keypad();
}

/// True for shift on either side.
pub fn keyLeftOrRightShift(key: Key) bool {
    return key.leftOrRightShift();
}

/// True for alt on either side.
pub fn keyLeftOrRightAlt(key: Key) bool {
    return key.leftOrRightAlt();
}
pub const FocusEvent = vt.input.FocusEvent;

/// The modifiers held during a key or mouse event.
///
/// ghostty's own `Mods` is a `packed struct(u16)` that also records which
/// side each modifier was pressed on; the encoders do not read the sides, so
/// this carries the six flags in one byte.
pub const KeyMods = packed struct(u8) {
    shift: bool = false,
    ctrl: bool = false,
    alt: bool = false,
    super: bool = false,
    caps_lock: bool = false,
    num_lock: bool = false,
    _padding: u2 = 0,

    fn toGhostty(self: KeyMods) vt.input.KeyMods {
        return .{
            .shift = self.shift,
            .ctrl = self.ctrl,
            .alt = self.alt,
            .super = self.super,
            .caps_lock = self.caps_lock,
            .num_lock = self.num_lock,
        };
    }
};

/// A key event to encode. A plain value: build one per press and pass it to
/// `encodeKey` together with the text the key produced.
///
/// ghostty's `KeyEvent` holds a borrowed `utf8` slice and modifiers with no C
/// representation, so this is the flattened equivalent.
pub const KeyEvent = extern struct {
    action: KeyAction = .press,
    key: Key = .unidentified,
    mods: KeyMods = .{},
    /// Modifiers that were consumed producing the event text. Effective
    /// modifiers are `mods` minus these.
    consumed_mods: KeyMods = .{},
    /// True while the event is part of an unfinished dead-key composition.
    composing: bool = false,
    /// The codepoint the key produces unshifted, or zero for none.
    unshifted_codepoint: u32 = 0,
};

/// Encode a key event for `terminal`, whose modes decide the encoding. `utf8`
/// is the text the key produced, empty when it produced none.
pub fn encodeKey(
    writer: *std.Io.Writer,
    terminal: *const Terminal,
    event: KeyEvent,
    utf8: []const u8,
) !void {
    const inner: vt.input.KeyEvent = .{
        .action = event.action.toGhostty(),
        .key = event.key,
        .mods = event.mods.toGhostty(),
        .consumed_mods = event.consumed_mods.toGhostty(),
        .composing = event.composing,
        .utf8 = utf8,
        .unshifted_codepoint = @intCast(event.unshifted_codepoint),
    };
    try vt.input.encodeKey(writer, inner, .fromTerminal(terminal));
}

pub const MouseAction = vt.input.MouseAction;
pub const MouseButton = vt.input.MouseButton;

/// The renderer geometry mouse encoding needs to turn a pixel position into a
/// grid cell. All values are in already-DPI-scaled pixels.
///
/// ghostty's own `renderer.Size` nests three extern structs inside a plain one,
/// which has no C representation, so this is the flattened equivalent.
pub const RenderSize = extern struct {
    /// The size of the area the grid is drawn into, padding included.
    screen_width: u32,
    screen_height: u32,
    /// The size of one cell.
    cell_width: u32,
    cell_height: u32,
    padding_top: u32 = 0,
    padding_bottom: u32 = 0,
    padding_right: u32 = 0,
    padding_left: u32 = 0,

    const Size = @FieldType(vt.input.MouseEncodeOptions, "size");

    fn toRenderer(self: RenderSize) Size {
        return .{
            .screen = .{ .width = self.screen_width, .height = self.screen_height },
            .cell = .{ .width = self.cell_width, .height = self.cell_height },
            .padding = .{
                .top = self.padding_top,
                .bottom = self.padding_bottom,
                .right = self.padding_right,
                .left = self.padding_left,
            },
        };
    }
};

/// A mouse event to encode. A plain value: build one per event and pass it
/// to `encodeMouse`.
pub const MouseEvent = extern struct {
    action: MouseAction = .press,
    /// The button the event is about. Ignored unless `has_button` is set,
    /// which a motion event without a held button leaves clear.
    button: MouseButton = .unknown,
    has_button: bool = false,
    mods: KeyMods = .{},
    /// Position in already-DPI-scaled pixels, relative to the surface.
    x: f32 = 0,
    y: f32 = 0,
};

/// Encode a mouse event for `terminal`, whose reporting mode and format decide
/// whether anything is written at all.
///
/// `any_button_pressed` should include this event, so a press reports true.
pub fn encodeMouse(
    writer: *std.Io.Writer,
    terminal: *const Terminal,
    event: MouseEvent,
    size: RenderSize,
    any_button_pressed: bool,
) !void {
    var opts: vt.input.MouseEncodeOptions = .fromTerminal(terminal, size.toRenderer());
    opts.any_button_pressed = any_button_pressed;
    const inner: vt.input.MouseEncodeEvent = .{
        .action = event.action,
        .button = if (event.has_button) event.button else null,
        .mods = event.mods.toGhostty(),
        .pos = .{ .x = event.x, .y = event.y },
    };
    try vt.input.encodeMouse(writer, inner, opts);
}

/// Encode `data` for pasting into `terminal`, respecting bracketed paste mode.
pub fn encodePaste(
    writer: *std.Io.Writer,
    terminal: *const Terminal,
    data: []const u8,
) !void {
    try vt.input.encodePasteWriter(writer, data, .fromTerminal(terminal));
}

/// Create a VT stream that applies escape sequences to `terminal`.
///
/// `continuation_max_bytes` caps the unfinished-sequence suffix the stream
/// tracks across feeds; zero disables tracking.
pub fn newStream(gpa: Allocator, terminal: *Terminal, continuation_max_bytes: usize) !*Stream {
    const self = try gpa.create(Stream);
    errdefer gpa.destroy(self);
    self.* = .{
        .inner = .init(.{
            .allocator = gpa,
            .handler = handler: {
                var value: vt.TerminalStream.Handler = .init(terminal);
                value.effects = Stream.effects();
                break :handler value;
            },
            .continuation_max_bytes = continuation_max_bytes,
        }),
        .gpa = gpa,
    };
    return self;
}

/// Destroys a stream created by `newStream`.
pub fn freeStream(self: *Stream, gpa: Allocator) void {
    for (self.queue.items) |event| event.deinit(gpa);
    self.queue.deinit(gpa);
    self.replies.deinit(gpa);
    if (self.current) |event| event.deinit(gpa);
    self.inner.deinit();
    gpa.destroy(self);
}

/// Releases a string a bound function handed out with `.returns = .caller`:
/// `plainString`, `selectionString`, `historyString`, `renderHyperlinkAt`.
pub fn freeString(gpa: Allocator, str: []const u8) void {
    gpa.free(str);
}

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
    for (0..self.rows) |y| fillRow(self, y, dst[y * width .. (y + 1) * width]);
    return total;
}

/// Flatten one viewport row into `dst` and report how many cells were
/// written: `cols`, or zero for a row off the grid. For a renderer that
/// redraws only the rows `renderDirtyRows` names.
pub fn renderRowCells(self: *RenderState, y: u16, dst: []RenderCell) !usize {
    if (y >= self.rows) return 0;
    const width: usize = self.cols;
    if (dst.len < width) return error.NoSpaceLeft;
    fillRow(self, y, dst[0..width]);
    return width;
}

fn fillRow(self: *RenderState, y: usize, dst: []RenderCell) void {
    const fg_default = packColor(self.colors.foreground);
    const bg_default = packColor(self.colors.background);
    const row = self.row_data.items(.cells)[y];
    const sel = self.row_data.items(.selection)[y];
    const raw = row.items(.raw);
    const styles = row.items(.style);
    for (raw, styles, 0..) |cell, style, x| {
        if (x >= dst.len) break;
        const out = &dst[x];
        out.* = .{
            .codepoint = 0,
            .fg = fg_default,
            .bg = bg_default,
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

        if (sel) |range| {
            if (x >= range[0] and x <= range[1]) {
                out.flags.selected = true;
            }
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
fn mergeFlags(out: CellFlags, f: anytype) CellFlags {
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

// -- Kitty graphics ---------------------------------------------------------
//
// The image protocol's state lives on the active screen's `ImageStorage`: a map
// of images by id, and a map of placements that say where each is drawn. What a
// renderer needs out of it is not that shape, though. It needs, per frame, a
// flat list of "draw this image, this part of it, here, this big", in the order
// they stack -- and it needs to know when an image's pixels have changed so it
// can keep its own textures rather than copy them every frame.
//
// `KittyImages` is that: the same kind of snapshot `RenderState` is for cells.
// `kittyUpdate` walks the storage, resolves every placement's position against
// the current viewport, drops the ones that are not on screen, sorts what is
// left by z, and hands the result over in one array. The pixels stay behind and
// are fetched by id, once per change, keyed by the generation stamp ghostty
// already maintains for exactly this purpose.

const kitty = vt.kitty.graphics;

/// How an image's bytes are laid out.
///
/// Ours rather than ghostty's: ghostty's has a backing type chosen for
/// compactness (three bits, at the time of writing), which cannot appear in an
/// extern struct. The tags and their order are the same, and the conversion is
/// a `switch`, so a tag added upstream is a compile error here rather than a
/// silent renumbering.
pub const KittyFormat = enum(u8) {
    /// Three bytes per pixel.
    rgb,
    /// Four bytes per pixel.
    rgba,
    /// A PNG file, to be decoded by the renderer.
    png,
    /// Two bytes per pixel. Only reachable by decoding a PNG.
    gray_alpha,
    /// One byte per pixel. Only reachable by decoding a PNG.
    gray,
};

/// Whether the image data is still compressed.
pub const KittyCompression = enum(u8) {
    none,
    zlib_deflate,
};

fn kittyFormat(f: @FieldType(kitty.Image, "format")) KittyFormat {
    return switch (f) {
        .rgb => .rgb,
        .rgba => .rgba,
        .png => .png,
        .gray_alpha => .gray_alpha,
        .gray => .gray,
    };
}

fn kittyCompression(c: @FieldType(kitty.Image, "compression")) KittyCompression {
    return switch (c) {
        .none => .none,
        .zlib_deflate => .zlib_deflate,
    };
}

/// One image drawn at one place, flattened for the C ABI.
///
/// Everything a renderer needs for one draw call, in the coordinates it works
/// in: cells for the position, pixels for the size. The image's own bytes are
/// not here -- see `kittyImage` and `kittyImageData`.
pub const KittyPlacement = extern struct {
    /// The image to draw. Look it up with `kittyImage`.
    image_id: u32,
    /// Which placement of that image this is. Together with `image_id` it
    /// identifies the placement for as long as it exists.
    placement_id: u32,

    /// Where the top-left corner goes, in viewport cells. The row is negative
    /// when the image has scrolled partly above the viewport, and either can be
    /// negative for a placement positioned relative to another one.
    viewport_col: i32,
    viewport_row: i32,
    /// Offset within that cell, in pixels.
    x_offset: u32,
    y_offset: u32,

    /// How big to draw it, in pixels. This is the source rectangle scaled to
    /// whatever the program asked for, so it is what the image should be
    /// stretched to rather than its natural size.
    pixel_width: u32,
    pixel_height: u32,
    /// The same size in cells, which is what the placement occupies on the
    /// grid. Useful for clipping; the pixel size is what to draw.
    grid_cols: u32,
    grid_rows: u32,

    /// The part of the image to draw, in image pixels. Already clamped to the
    /// image, and a zero-sized request already turned into the full dimension.
    source_x: u32,
    source_y: u32,
    source_width: u32,
    source_height: u32,

    /// Stacking order. The snapshot is sorted by it, so drawing the array in
    /// order is correct; it is here for the one decision it still leaves, which
    /// is whether a placement goes under the text (negative) or over it.
    z: i32,
};

/// A snapshot of where the images on the active screen are drawn.
///
/// Rebuilt by `kittyUpdate` and read out in one crossing, the same way
/// `RenderState` hands over cells.
pub const KittyImages = struct {
    gpa: Allocator,
    items: std.ArrayList(KittyPlacement) = .empty,
    /// The storage's generation stamp as of the last `kittyUpdate`.
    ///
    /// Bumped whenever an image or a placement is added, replaced or removed,
    /// and not by scrolling or resizing. An unchanged stamp means every
    /// image's bytes are the ones already fetched, so it is what a texture
    /// cache should be keyed on to decide whether to look at all.
    generation: u64 = 0,
};

pub fn newKittyImages(gpa: Allocator) !*KittyImages {
    const self = try gpa.create(KittyImages);
    self.* = .{ .gpa = gpa };
    return self;
}

pub fn freeKittyImages(self: *KittyImages, gpa: Allocator) void {
    self.items.deinit(gpa);
    gpa.destroy(self);
}

/// Rebuild the snapshot from `term`'s active screen.
///
/// Placements that cannot be drawn are left out: the ones scrolled off the
/// viewport, the ones whose text has been pruned out of the scrollback, and the
/// virtual (unicode placeholder) ones, which have no position of their own
/// because they are laid out by the cells that reference them.
pub fn kittyUpdate(self: *KittyImages, term: *Terminal) !void {
    self.items.clearRetainingCapacity();

    const storage = &term.screens.active.kitty_images;
    self.generation = storage.generation;

    var it = storage.placements.iterator();
    while (it.next()) |entry| {
        const key = entry.key_ptr;
        const placement = entry.value_ptr;
        const image = storage.images.getPtr(key.image_id) orelse continue;

        const pos = kittyViewportPos(storage, placement, image, term) orelse continue;

        const size = placement.pixelSize(image.*, term);
        const grid = placement.gridSize(image.*, term);
        const source = placement.sourceRect(image.*);

        try self.items.append(self.gpa, .{
            .image_id = key.image_id,
            .placement_id = key.placement_id.id,
            .viewport_col = pos.col,
            .viewport_row = pos.row,
            .x_offset = placement.x_offset,
            .y_offset = placement.y_offset,
            .pixel_width = size.width,
            .pixel_height = size.height,
            .grid_cols = grid.cols,
            .grid_rows = grid.rows,
            .source_x = source.x,
            .source_y = source.y,
            .source_width = source.width,
            .source_height = source.height,
            .z = placement.z,
        });
    }

    // Back to front. A hash map has no order at all, so without this the same
    // two overlapping images could swap places from one frame to the next; the
    // ids break the tie so equal z is stable too.
    std.mem.sort(KittyPlacement, self.items.items, {}, struct {
        fn lessThan(_: void, a: KittyPlacement, b: KittyPlacement) bool {
            if (a.z != b.z) return a.z < b.z;
            if (a.image_id != b.image_id) return a.image_id < b.image_id;
            return a.placement_id < b.placement_id;
        }
    }.lessThan);
}

/// Where a placement's top-left corner is, relative to the viewport, or absent
/// if it is not on screen at all.
///
/// A placement is anchored to a pin -- a tracked position in the scrollback --
/// rather than to a row number, so it follows its text as the screen scrolls.
/// Turning that back into a viewport row means asking the page list where both
/// the pin and the viewport's top-left corner are on the screen-absolute axis
/// and subtracting.
fn kittyViewportPos(
    storage: *const kitty.ImageStorage,
    placement: *const kitty.ImageStorage.Placement,
    image: *const kitty.Image,
    term: *Terminal,
) ?struct { col: i32, row: i32 } {
    // A placement positioned relative to another one has no pin of its own: it
    // hangs off its parent's, at an accumulated offset. A chain that ends at a
    // virtual placement has no resolvable position, since only a renderer
    // scanning the cells can find the placeholders it is anchored to.
    var col_offset: i32 = 0;
    var row_offset: i32 = 0;
    const pin = switch (placement.location) {
        .pin => |p| p,
        .virtual => return null,
        .relative => |rel| pin: {
            const chain = storage.resolveChain(rel) orelse return null;
            col_offset = chain.horizontal_offset;
            row_offset = chain.vertical_offset;
            break :pin switch (chain.root.location) {
                .pin => |p| p,
                .virtual, .relative => return null,
            };
        },
    };
    if (pin.garbage) return null;

    const pages = &term.screens.active.pages;
    const pin_point = pages.pointFromPin(.screen, pin.*) orelse return null;
    const top_left = pages.pointFromPin(.screen, pages.getTopLeft(.viewport)) orelse return null;

    const row: i32 = (@as(i32, @intCast(pin_point.screen.y)) -
        @as(i32, @intCast(top_left.screen.y))) +| row_offset;
    const col: i32 = @as(i32, @intCast(pin_point.screen.x)) +| col_offset;

    // Off the top, off the bottom, or pushed off either side by a relative
    // placement's offsets. A placement measured in cells has no size at all
    // until the terminal has been told how big a cell is, and something with no
    // size is off every edge at once, so it counts as one cell until then --
    // otherwise forgetting `resizeCells` would silently hide every image.
    const grid = placement.gridSize(image.*, term);
    const height: i64 = @max(grid.rows, 1);
    const width: i64 = @max(grid.cols, 1);
    if (@as(i64, row) + height <= 0 or row >= @as(i32, term.rows)) return null;
    if (@as(i64, col) + width <= 0 or col >= @as(i32, term.cols)) return null;

    return .{ .col = col, .row = row };
}

/// How many placements `kittyPlacements` will write.
pub fn kittyPlacementCount(self: *KittyImages) usize {
    return self.items.items.len;
}

/// Copy the placements into `dst`, back to front, and report how many.
pub fn kittyPlacements(self: *KittyImages, dst: []KittyPlacement) !usize {
    if (dst.len < self.items.items.len) return error.NoSpaceLeft;
    @memcpy(dst[0..self.items.items.len], self.items.items);
    return self.items.items.len;
}

/// What an image is, without its bytes.
pub const KittyImage = extern struct {
    /// Changes whenever the bytes behind `kittyImageData` change, including
    /// when an animation advances a frame. Cache textures on it.
    generation: u64,
    /// The length `kittyImageData` will write.
    data_len: u64,
    /// The image's own size in pixels, which is what `source_*` on a placement
    /// indexes into. Not the size it is drawn at.
    width: u32,
    height: u32,
    format: KittyFormat,
    compression: KittyCompression,
    _pad: u16 = 0,
};

/// Look up an image on the active screen by id.
pub fn kittyImage(term: *Terminal, image_id: u32) ?KittyImage {
    const image = term.screens.active.kitty_images.images.getPtr(image_id) orelse return null;
    return .{
        .generation = image.generation,
        .data_len = image.renderData().len(),
        .width = image.width,
        .height = image.height,
        .format = kittyFormat(image.format),
        .compression = kittyCompression(image.compression),
    };
}

/// Copy an image's pixels into `dst` and report how many bytes were written,
/// or zero if there is no such image.
///
/// The bytes are as the program transmitted them, decompressed: a PNG is still
/// a PNG, and it is the renderer that decodes it. For an animated image these
/// are the current frame's, which is why the generation stamp moves when the
/// frame does.
pub fn kittyImageData(term: *Terminal, image_id: u32, dst: []u8) !usize {
    const image = term.screens.active.kitty_images.images.getPtr(image_id) orelse return 0;
    const bytes = image.renderData().bytes() orelse return 0;
    if (dst.len < bytes.len) return error.NoSpaceLeft;
    @memcpy(dst[0..bytes.len], bytes);
    return bytes.len;
}
