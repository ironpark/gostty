//! The VT stream: escape sequences in, terminal state and events out.
//!
//! ghostty reports what a program asked *of the embedder* -- a bell, a
//! notification, a clipboard request -- through `Handler.effects`, a struct of
//! function pointers called mid-feed with borrowed payloads. Reflecting that
//! into Go would turn every slice into a loose pointer and length and leave a
//! borrow in a Go callback's hands, so the stream copies each one into a queue
//! that `nextEvent` drains after the feed returns. Clipboard requests are the
//! exception: a program blocks on those, so they stay callbacks.
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
