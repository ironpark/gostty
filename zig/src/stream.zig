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
    /// A sequence this library does not implement, captured so it can be
    /// looked at. Only APC today. `eventSequence` carries the content.
    ///
    /// Off until `setUnknownMaxBytes` turns it on: capturing costs a buffer
    /// per stream, and a program that never sends an unknown sequence would
    /// pay it for nothing.
    unknown_sequence,
};

/// Which color scheme the desktop is in, reported to a program that asks
/// (CSI ? 996 n) or that subscribed to changes (mode 2031).
pub const ColorScheme = vt.device_status.ColorScheme;

/// ghostty's device attributes reply. Not named by `lib_vt.zig`, so it is
/// recovered from the effect's own signature.
const Attributes = @typeInfo(@typeInfo(@typeInfo(
    @FieldType(vt.TerminalStream.Handler.Effects, "device_attributes"),
).optional.child).pointer.child).@"fn".return_type.?;

/// The DA1 reply and its feature codes, reached the same way.
const DaFeature = @FieldType(Attributes, "primary").Feature;

/// The DA1 feature codes this binding can advertise. Two today, so the
/// backing array is sized for the list rather than for ghostty's full set.
const max_da_features = 2;

/// How long an XTVERSION reply may be. ghostty drops anything longer.
const max_version_bytes = 256;

/// How long an ENQ reply may be. ghostty drops anything longer.
const max_enquiry_bytes = 255;

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

    /// The failure arm of a ghostty reply, which spells the same four reasons
    /// in two `Result` unions, one per direction.
    fn toGhostty(self: ClipboardDenial, comptime Result: type) Result {
        return switch (self) {
            .denied => .denied,
            .unsupported => .unsupported,
            .busy => .busy,
            .io_error => .io_error,
        };
    }
};

/// Called while a feed is in flight, once per clipboard request, with the
/// request to answer. The request is valid until the callback returns; a
/// request that is not answered by then is denied.
pub const ClipboardFn = *const fn (request: *ClipboardRequest, userdata: usize) callconv(.c) void;

/// One clipboard request from the running program: an OSC 52 or Kitty OSC
/// 5522 write or read. Handed to the clipboard callback, which reads what it
/// needs and answers with `allow`, `replyText` or `deny` before returning.
///
/// Lives inside the stream rather than on the stack so the handle Go holds
/// stays valid; outside a callback nothing is pending and every accessor
/// answers its zero value while every reply is a no-op.
pub const ClipboardRequest = struct {
    /// Borrowed for the duration of the effect call and nulled after.
    write: ?vt.clipboard.Write = null,
    read: ?vt.clipboard.Read = null,
    /// Whether the callback answered. An unanswered request is denied so the
    /// program is not left waiting.
    answered: bool = false,

    /// Which clipboard the request names.
    pub fn location(self: *ClipboardRequest) ClipboardLocation {
        if (self.write) |write| return write.location;
        if (self.read) |read| return read.location;
        return .standard;
    }

    /// The requesting program's name, empty when the protocol carries none.
    pub fn name(self: *ClipboardRequest) []const u8 {
        if (self.write) |write| return write.name;
        if (self.read) |read| return read.name;
        return "";
    }

    /// True when the terminal already holds a session grant, so the embedder
    /// should skip its permission prompt.
    pub fn granted(self: *ClipboardRequest) bool {
        if (self.write) |write| return write.granted;
        if (self.read) |read| return read.granted;
        return false;
    }

    /// True when the program supplied a session password, so a decision can be
    /// remembered via the `remember` argument when answering.
    pub fn canRemember(self: *ClipboardRequest) bool {
        if (self.write) |write| return write.can_remember;
        if (self.read) |read| return read.can_remember;
        return false;
    }

    /// How many representations a write carries. Zero clears the destination.
    pub fn contentCount(self: *ClipboardRequest) usize {
        const write = self.write orelse return 0;
        return write.contents.len;
    }

    /// The MIME type of one representation of a write.
    pub fn contentMime(self: *ClipboardRequest, index: usize) []const u8 {
        const write = self.write orelse return "";
        if (index >= write.contents.len) return "";
        return write.contents[index].mime;
    }

    /// The bytes of one representation of a write. Binary safe.
    pub fn contentData(self: *ClipboardRequest, index: usize) []const u8 {
        const write = self.write orelse return "";
        if (index >= write.contents.len) return "";
        return write.contents[index].data;
    }

    /// How many MIME types a read asks for, in order of preference.
    pub fn mimeCount(self: *ClipboardRequest) usize {
        const read = self.read orelse return 0;
        return read.mimes.len;
    }

    /// One of the MIME types a read asks for.
    pub fn mime(self: *ClipboardRequest, index: usize) []const u8 {
        const read = self.read orelse return "";
        if (index >= read.mimes.len) return "";
        return read.mimes[index];
    }

    /// Accept a write. Answering a read this way serves empty text.
    pub fn allow(self: *ClipboardRequest, remember: bool) void {
        if (self.answered) return;
        if (self.write) |write| {
            write.reply(.{ .success = .{ .remember = remember } });
            self.answered = true;
            return;
        }
        if (self.read) |read| {
            read.reply(.{ .success = .{ .remember = remember } });
            self.answered = true;
        }
    }

    /// Serve a read with plain text.
    ///
    /// `text` is borrowed for this call only; the terminal copies what it
    /// needs before returning.
    pub fn replyText(self: *ClipboardRequest, text: []const u8, remember: bool) void {
        if (self.answered) return;
        const read = self.read orelse return;
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

    /// Refuse the request.
    pub fn deny(self: *ClipboardRequest, reason: ClipboardDenial) void {
        if (self.answered) return;
        if (self.write) |write| {
            write.reply(reason.toGhostty(vt.clipboard.Write.Result));
        } else if (self.read) |read| {
            read.reply(reason.toGhostty(vt.clipboard.Read.Result));
        } else return;
        self.answered = true;
    }
};

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
    /// Events before this index have been handed out; `nextEvent` reads from
    /// here rather than shifting the array on every event.
    queue_head: usize = 0,

    /// Bytes the terminal answered a query with, waiting to be written back to
    /// the program. Collected during a feed and drained by `writeReplies`.
    replies: std.ArrayList(u8) = .empty,

    /// The clipboard request being answered, if a callback is running. Kept
    /// here so the pointer the callback receives stays valid.
    request: ClipboardRequest = .{},

    on_clipboard_write: ?ClipboardFn = null,
    write_userdata: usize = 0,
    on_clipboard_read: ?ClipboardFn = null,
    read_userdata: usize = 0,

    /// The XTVERSION reply, "name version". Empty means ghostty's own
    /// fallback, which is the library name rather than the embedder's.
    version_buf: [max_version_bytes]u8 = undefined,
    version_len: usize = 0,
    /// The ENQ (0x05) reply. Empty means no answer, which is the usual
    /// choice: an answerback string is a security hazard as often as a
    /// feature.
    enquiry_buf: [max_enquiry_bytes]u8 = undefined,
    enquiry_len: usize = 0,
    /// The desktop color scheme, or null to leave CSI ? 996 n unanswered.
    color_scheme: ?ColorScheme = null,
    /// Backing storage for the DA1 feature list, which ghostty borrows for
    /// the duration of the effect call.
    da_features: [max_da_features]DaFeature = undefined,

    /// Drag and drop, in `dnd.zig`: the handler for what the program does,
    /// and the representations staged for the next drop.
    on_drag: ?@import("dnd.zig").DragFn = null,
    drag_userdata: usize = 0,
    drag_items: std.ArrayList(vt.kitty.dnd.Item) = .empty,
    /// What `nextEvent` last handed out. Its payload stays readable until the
    /// following `nextEvent`.
    current: ?Queued = null,

    const Queued = struct {
        kind: StreamEvent,
        /// Notification title. Owned.
        title: []const u8 = "",
        /// Notification body. Owned.
        body: []const u8 = "",
        /// An unknown sequence's content. Owned. Kept apart from `body` so
        /// each accessor means one thing: an accessor that changes meaning
        /// with the event tag is a trap for the caller.
        sequence: []const u8 = "",
        progress_state: ProgressState = .remove,
        /// 0..100, or 255 when the report carried no percentage.
        progress: u8 = 255,

        fn deinit(self: Queued, gpa: Allocator) void {
            gpa.free(self.title);
            gpa.free(self.body);
            gpa.free(self.sequence);
        }
    };

    /// Dropping an event beats failing the feed: the terminal state the same
    /// sequence produced has already been applied.
    fn push(self: *Stream, event: Queued) void {
        self.queue.append(self.gpa, event) catch event.deinit(self.gpa);
    }

    fn onBell(handler: *vt.TerminalStream.Handler) void {
        streamFromHandler(handler).push(.{ .kind = .bell });
    }

    fn onTitleChanged(handler: *vt.TerminalStream.Handler) void {
        streamFromHandler(handler).push(.{ .kind = .title_changed });
    }

    fn onPwdChanged(handler: *vt.TerminalStream.Handler) void {
        streamFromHandler(handler).push(.{ .kind = .pwd_changed });
    }

    fn onDesktopNotification(
        handler: *vt.TerminalStream.Handler,
        notification: vt.TerminalStream.Action.ShowDesktopNotification,
    ) void {
        const self = streamFromHandler(handler);
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
        streamFromHandler(handler).push(.{
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
        const self = streamFromHandler(handler);
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
        const self = streamFromHandler(handler);
        const callback = self.on_clipboard_write orelse {
            write.reply(.denied);
            return;
        };
        self.request = .{ .write = write };
        defer self.request = .{};
        callback(&self.request, self.write_userdata);
        if (!self.request.answered) write.reply(.denied);
    }

    fn onClipboardRead(
        handler: *vt.TerminalStream.Handler,
        read: vt.clipboard.Read,
    ) void {
        const self = streamFromHandler(handler);
        const callback = self.on_clipboard_read orelse {
            read.reply(.denied);
            return;
        };
        self.request = .{ .read = read };
        defer self.request = .{};
        callback(&self.request, self.read_userdata);
        if (!self.request.answered) read.reply(.denied);
    }

    /// A sequence the library does not implement. The content is borrowed
    /// for the call, so it is copied like the notification strings.
    fn onUnknownSequence(
        handler: *vt.TerminalStream.Handler,
        value: vt.UnknownSequence,
    ) void {
        const self = streamFromHandler(handler);
        const content = switch (value) {
            .apc => |apc| apc.content,
        };
        const copy = self.gpa.dupe(u8, content) catch return;
        self.push(.{ .kind = .unknown_sequence, .sequence = copy });
    }

    /// What the terminal answers `CSI c`, `CSI > c` and `CSI = c` with.
    ///
    /// Not configurable: the conformance level and device type describe what
    /// the parser implements, which is ghostty's business rather than the
    /// embedder's. The one part that varies is whether OSC 52 is served, and
    /// the stream already knows that from whether a clipboard callback was
    /// installed -- an embedder should not have to declare a fact about its
    /// own wiring.
    fn onDeviceAttributes(handler: *vt.TerminalStream.Handler) Attributes {
        const self = streamFromHandler(handler);
        var n: usize = 0;
        self.da_features[n] = .ansi_color;
        n += 1;
        if (self.on_clipboard_read != null or self.on_clipboard_write != null) {
            self.da_features[n] = .clipboard;
            n += 1;
        }
        return .{ .primary = .{ .features = self.da_features[0..n] } };
    }

    fn onXtversion(handler: *vt.TerminalStream.Handler) []const u8 {
        const self = streamFromHandler(handler);
        return self.version_buf[0..self.version_len];
    }

    fn onEnquiry(handler: *vt.TerminalStream.Handler) []const u8 {
        const self = streamFromHandler(handler);
        return self.enquiry_buf[0..self.enquiry_len];
    }

    fn onColorScheme(handler: *vt.TerminalStream.Handler) ?ColorScheme {
        return streamFromHandler(handler).color_scheme;
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
        // Wired unconditionally. These answer queries a program blocks on --
        // `CSI c` and ENQ go unanswered without them -- and every answer is
        // either fixed or already known to the stream, so there is nothing
        // for an embedder to configure before they are correct.
        result.device_attributes = onDeviceAttributes;
        result.xtversion = onXtversion;
        result.enquiry = onEnquiry;
        result.color_scheme = onColorScheme;
        result.drag_and_drop = @import("dnd.zig").onDragEffect;
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
        if (self.queue_head == self.queue.items.len) {
            // Drained: reuse the buffer from the front for the next feed.
            self.queue.clearRetainingCapacity();
            self.queue_head = 0;
            return null;
        }
        const event = self.queue.items[self.queue_head];
        self.queue_head += 1;
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

    /// The current event's unknown-sequence content, empty for other events.
    ///
    /// Its own accessor rather than a second meaning for `eventBody`: one
    /// accessor whose meaning depends on the event tag is a trap, and the
    /// event tag is not in the type system to catch the mistake.
    pub fn eventSequence(self: *Stream) []const u8 {
        const event = self.current orelse return "";
        return event.sequence;
    }

    /// Capture up to `max` bytes of the sequences this library does not
    /// implement, and report them as `unknown_sequence` events. Zero, the
    /// default, turns capture off.
    ///
    /// A diagnostic: it is how you find out that a program is speaking a
    /// graphics protocol you did not wire up, rather than watching it draw
    /// nothing. The buffer is per stream, which is why it is off by default.
    pub fn setUnknownMaxBytes(self: *Stream, max: usize) void {
        self.inner.handler.apc_handler.unknown_max_bytes = max;
        self.inner.handler.unknown_sequence = if (max > 0) onUnknownSequence else null;
    }

    /// Report the embedder's name and version to programs that ask
    /// (XTVERSION, `CSI > 0 q`).
    ///
    /// Takes the two parts rather than one string because the reply is parsed
    /// as `"name version"`, and a caller with one string to hand over has no
    /// reason to know that. Without this the terminal answers `libghostty`,
    /// which is true of the parser and useless as an application identity.
    ///
    /// Silently truncated past 256 bytes, which is ghostty's cap.
    pub fn setVersionReport(self: *Stream, name: []const u8, version: []const u8) void {
        var writer: std.Io.Writer = .fixed(&self.version_buf);
        if (version.len == 0) {
            writer.writeAll(name) catch {};
        } else {
            writer.print("{s} {s}", .{ name, version }) catch {};
        }
        self.version_len = writer.end;
    }

    /// Answer ENQ (0x05) with `reply`. Empty, the default, answers nothing.
    ///
    /// An answerback string is echoed to anything that sends a single control
    /// byte, so it leaks whatever it holds to any program that asks. Terminals
    /// leave it empty for that reason and so does this.
    ///
    /// Silently truncated past 255 bytes, which is ghostty's cap.
    pub fn setEnquiryResponse(self: *Stream, reply: []const u8) void {
        const n = @min(reply.len, self.enquiry_buf.len);
        @memcpy(self.enquiry_buf[0..n], reply[0..n]);
        self.enquiry_len = n;
    }

    /// Tell the terminal the desktop is in `scheme`.
    ///
    /// Both halves of the protocol, because doing one without the other is a
    /// silent bug: a program that asks (CSI ? 996 n) gets the answer, and a
    /// program that subscribed (mode 2031) is told right now. The report goes
    /// into the reply buffer, so it leaves with the next `writeReplies`.
    ///
    /// Named for the event rather than the field it sets: this writes to the
    /// program, which `setColorScheme` would not have said.
    pub fn colorSchemeChanged(self: *Stream, scheme: ColorScheme) void {
        const previous = self.color_scheme;
        self.color_scheme = scheme;
        if (previous != null and previous.? == scheme) return;
        if (!self.inner.handler.terminal.modes.get(.report_color_scheme)) return;
        var buf: [vt.device_status.max_color_scheme_report_encode_size]u8 = undefined;
        var writer: std.Io.Writer = .fixed(&buf);
        vt.device_status.encodeColorSchemeReport(&writer, scheme) catch return;
        self.replies.appendSlice(self.gpa, writer.buffered()) catch {};
    }

    /// Leave color scheme queries unanswered, which is ghostty's default. For
    /// an embedder with no desktop to ask.
    pub fn clearColorScheme(self: *Stream) void {
        self.color_scheme = null;
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
};

/// The stream a ghostty handler belongs to. `dnd.zig` needs it to reach the
/// callback and the reply buffer from inside an effect.
pub fn streamFromHandler(handler: *vt.TerminalStream.Handler) *Stream {
    const inner: *vt.TerminalStream = @fieldParentPtr("handler", handler);
    return @fieldParentPtr("inner", inner);
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
    @import("dnd.zig").dragClearItems(self);
    self.drag_items.deinit(gpa);
    for (self.queue.items[self.queue_head..]) |event| event.deinit(gpa);
    self.queue.deinit(gpa);
    self.replies.deinit(gpa);
    if (self.current) |event| event.deinit(gpa);
    self.inner.deinit();
    gpa.destroy(self);
}
