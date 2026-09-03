//! The libghostty-vt surface exposed to Go.
//!
//! `Terminal` is ghostty's own type, bound directly: its methods become Go
//! methods without any wrapper. This module only adds what ghostty does not
//! provide and zigo needs:
//!
//!   - an `std.Io` value for `.io` injection (ghostty ships `TinyIo` as a type,
//!     not as a ready-made `Io` declaration);
//!   - a release function for the string `plainString` hands out.
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
        result.clipboard_write = onClipboardWrite;
        result.clipboard_read = onClipboardRead;
        return result;
    }

    /// Feed bytes to the parser, applying them to the terminal.
    pub fn feed(self: *Stream, bytes: []const u8) void {
        self.inner.nextSlice(bytes);
    }

    /// True once a sequence failed in a way the terminal could not absorb, such
    /// as an allocation failure. Streams are best-effort and keep going.
    pub fn failed(self: *Stream) bool {
        return self.inner.handler.semantic_failure;
    }

    /// Write the unfinished sequence suffix, when continuation tracking is on.
    pub fn writeContinuation(self: *Stream, writer: *std.Io.Writer) !void {
        try self.inner.writeContinuation(writer);
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

/// Change the viewport size.
///
/// Wrapped because `vt.Terminal.Resize` carries a nested optional struct for
/// the cell size in pixels, which has no C representation.
pub fn resize(self: *Terminal, gpa: Allocator, width: u16, height: u16) !void {
    try self.resize(gpa, .{ .cols = width, .rows = height });
}

/// Which of a terminal's screens is active.
pub const ScreenKey = vt.ScreenSet.Key;

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

/// Drop the current selection, if any.
pub fn screenClearSelection(self: *Screen) void {
    self.clearSelection();
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

/// Start searching `target` for `needle`. The search does not run until
/// `searchAll`.
pub fn newSearch(gpa: Allocator, target: *Screen, needle: []const u8) !*Search {
    const self = try gpa.create(Search);
    errdefer gpa.destroy(self);
    self.* = try .init(gpa, target, needle);
    return self;
}

pub fn freeSearch(self: *Search, gpa: Allocator) void {
    self.deinit();
    gpa.destroy(self);
}

/// The number of matches found so far.
pub fn searchMatchCount(self: *Search) usize {
    return self.matchesLen();
}

/// Move to the next or previous match and put it in the screen's selection, so
/// the text comes back out through `Screen.selectionString`.
///
/// ghostty tracks the search's current match separately from the screen's
/// selection; joining the two is what a search UI wants and what this does.
/// Returns false when there is no match to move to.
pub fn searchSelect(self: *Search, to: SearchDirection) !bool {
    if (!try self.select(to)) return false;
    const match = self.selectedMatch() orelse return false;
    const bounds = match.untracked();
    try self.screen.select(vt.Selection.init(bounds.start, bounds.end, false));
    return true;
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

/// Switch between the primary and alternate screens.
///
/// Wrapped because ghostty returns the screen being left, and a handle borrowed
/// from its receiver has no representation in zigo -- only tagged-union
/// projections produce one.
pub fn switchScreen(self: *Terminal, key: ScreenKey) !void {
    _ = try self.switchScreen(key);
}

/// Which screen is currently active.
pub fn activeScreenKey(self: *Terminal) ScreenKey {
    return self.screens.active_key;
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

/// The full screen: scrollback followed by the active area.
pub fn screenString(self: *Terminal, gpa: Allocator) ![]const u8 {
    return try self.screens.active.dumpStringAlloc(gpa, .{ .screen = .{} });
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

    fn rgb(value: u32) vt.color.RGB {
        return .{
            .r = @truncate(value >> 16),
            .g = @truncate(value >> 8),
            .b = @truncate(value),
        };
    }

    fn toVt(self: Attribute) vt.Attribute {
        return switch (self) {
            .unset => .unset,
            .bold => .bold,
            .reset_bold => .reset_bold,
            .italic => .italic,
            .reset_italic => .reset_italic,
            .faint => .faint,
            .underline => |v| .{ .underline = v },
            .underline_color_rgb => |v| .{ .underline_color = rgb(v) },
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
            .direct_color_fg => |v| .{ .direct_color_fg = rgb(v) },
            .direct_color_bg => |v| .{ .direct_color_bg = rgb(v) },
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

/// The display width of a grapheme cluster given as codepoints.
///
/// Wrapped because `vt.unicode.graphemeWidth` is generic over the codepoint
/// integer type, and a generic function has no signature to bind.
pub fn graphemeWidth(cps: []const u32) u8 {
    return @intCast(vt.unicode.graphemeWidth(u32, cps).width);
}

/// Unicode helpers, re-exported as-is.
pub const unicode = vt.unicode;

/// Input encoding: turning key, mouse and focus events into the bytes a
/// program reading the pty expects.
pub const input = vt.input;

pub const Key = vt.input.Key;
pub const KeyAction = vt.input.KeyAction;
pub const FocusEvent = vt.input.FocusEvent;

/// A single keyboard modifier.
///
/// ghostty stores modifiers as a `packed struct(u16)`, which has no C
/// representation, so they are set one at a time instead of passed as a mask.
pub const KeyMod = enum(u8) {
    shift,
    ctrl,
    alt,
    super,
    caps_lock,
    num_lock,
};

/// A key event being assembled for encoding.
///
/// ghostty's `KeyEvent` holds a borrowed `utf8` slice, so a Go caller could not
/// keep one alive across calls. This owns the text instead, which is also the
/// shape ghostty's own C API uses.
pub const KeyEvent = struct {
    /// Long enough for any single key event's text: a grapheme cluster with
    /// combining marks, not arbitrary input.
    const utf8_capacity = 64;

    inner: vt.input.KeyEvent = .{},
    utf8_buf: [utf8_capacity]u8 = undefined,

    fn applyMod(mods: *vt.input.KeyMods, mod: KeyMod, value: bool) void {
        switch (mod) {
            .shift => mods.shift = value,
            .ctrl => mods.ctrl = value,
            .alt => mods.alt = value,
            .super => mods.super = value,
            .caps_lock => mods.caps_lock = value,
            .num_lock => mods.num_lock = value,
        }
    }

    /// Return the event to its defaults so one handle can encode many keys.
    pub fn reset(self: *KeyEvent) void {
        self.inner = .{};
    }

    pub fn setAction(self: *KeyEvent, action: KeyAction) void {
        self.inner.action = action;
    }

    pub fn setKey(self: *KeyEvent, key: Key) void {
        self.inner.key = key;
    }

    pub fn setMod(self: *KeyEvent, mod: KeyMod, value: bool) void {
        applyMod(&self.inner.mods, mod, value);
    }

    /// Mark a modifier as consumed producing the event text. Effective
    /// modifiers are the set modifiers minus the consumed ones.
    pub fn setConsumedMod(self: *KeyEvent, mod: KeyMod, value: bool) void {
        applyMod(&self.inner.consumed_mods, mod, value);
    }

    /// True while the event is part of an unfinished dead-key composition.
    pub fn setComposing(self: *KeyEvent, composing: bool) void {
        self.inner.composing = composing;
    }

    /// The text this key produced, if any. Copied into the event.
    pub fn setUtf8(self: *KeyEvent, text: []const u8) error{NoSpaceLeft}!void {
        if (text.len > utf8_capacity) return error.NoSpaceLeft;
        @memcpy(self.utf8_buf[0..text.len], text);
        self.inner.utf8 = self.utf8_buf[0..text.len];
    }

    /// The codepoint this key produces unshifted, or zero for none.
    pub fn setUnshiftedCodepoint(self: *KeyEvent, cp: u32) void {
        self.inner.unshifted_codepoint = @intCast(cp);
    }
};

pub fn newKeyEvent(gpa: Allocator) !*KeyEvent {
    const self = try gpa.create(KeyEvent);
    self.* = .{};
    return self;
}

pub fn freeKeyEvent(self: *KeyEvent, gpa: Allocator) void {
    gpa.destroy(self);
}

/// Encode a key event for `terminal`, whose modes decide the encoding.
pub fn encodeKey(
    writer: *std.Io.Writer,
    terminal: *const Terminal,
    event: *const KeyEvent,
) !void {
    try vt.input.encodeKey(writer, event.inner, .fromTerminal(terminal));
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

/// A mouse event being assembled for encoding.
///
/// A handle rather than a value because ghostty's event carries an optional
/// button and packed modifiers, neither of which crosses as a struct field.
pub const MouseEvent = struct {
    inner: vt.input.MouseEncodeEvent = .{},

    /// Return the event to its defaults so one handle can encode many events.
    pub fn reset(self: *MouseEvent) void {
        self.inner = .{};
    }

    pub fn setAction(self: *MouseEvent, action: MouseAction) void {
        self.inner.action = action;
    }

    /// The button involved. Motion with no button held uses `clearButton`.
    pub fn setButton(self: *MouseEvent, button: MouseButton) void {
        self.inner.button = button;
    }

    pub fn clearButton(self: *MouseEvent) void {
        self.inner.button = null;
    }

    pub fn setMod(self: *MouseEvent, mod: KeyMod, value: bool) void {
        KeyEvent.applyMod(&self.inner.mods, mod, value);
    }

    /// The position in surface-space pixels, (0, 0) at the top left.
    pub fn setPosition(self: *MouseEvent, x: f32, y: f32) void {
        self.inner.pos = .{ .x = x, .y = y };
    }
};

pub fn newMouseEvent(gpa: Allocator) !*MouseEvent {
    const self = try gpa.create(MouseEvent);
    self.* = .{};
    return self;
}

pub fn freeMouseEvent(self: *MouseEvent, gpa: Allocator) void {
    gpa.destroy(self);
}

/// Encode a mouse event for `terminal`, whose reporting mode and format decide
/// whether anything is written at all.
///
/// `any_button_pressed` should include this event, so a press reports true.
pub fn encodeMouse(
    writer: *std.Io.Writer,
    terminal: *const Terminal,
    event: *const MouseEvent,
    size: RenderSize,
    any_button_pressed: bool,
) !void {
    var opts: vt.input.MouseEncodeOptions = .fromTerminal(terminal, size.toRenderer());
    opts.any_button_pressed = any_button_pressed;
    try vt.input.encodeMouse(writer, event.inner, opts);
}

/// Encode a focus in/out report (CSI I / CSI O).
pub fn encodeFocus(writer: *std.Io.Writer, event: FocusEvent) !void {
    try vt.input.encodeFocus(writer, event);
}

/// True if `data` can be pasted without the receiving program seeing it as
/// something other than literal text.
pub fn isSafePaste(data: []const u8) bool {
    return vt.input.isSafePaste(data);
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
    if (self.current) |event| event.deinit(gpa);
    self.inner.deinit();
    gpa.destroy(self);
}

/// Releases a string handed out by `plainString`.
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

/// One cell of the viewport, flattened for the C ABI.
///
/// Colors are already resolved: palette indices are looked up in the render
/// state's palette and defaults are filled in from the terminal's own
/// foreground and background, so Go never has to carry a palette. `inverse`
/// is applied here too, for the same reason.
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
    /// `CellFlags` in its backing integer. Decode it with `CellFlagsFromBacking`
    /// on the cells you actually draw: keeping the array element plain is what
    /// lets a whole viewport cross the boundary without being copied.
    flags: u32,
};

pub fn newRenderState(gpa: Allocator) !*RenderState {
    const self = try gpa.create(RenderState);
    errdefer gpa.destroy(self);
    self.* = .empty;
    return self;
}

pub fn freeRenderState(gpa: Allocator, self: *RenderState) void {
    self.deinit(gpa);
    gpa.destroy(self);
}

/// Pull the latest viewport out of `term`. Resets the terminal's dirty state.
pub fn renderUpdate(self: *RenderState, gpa: Allocator, term: *Terminal) !void {
    try self.update(gpa, term);
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

    const fg_default = packRgb(self.colors.foreground);
    const bg_default = packRgb(self.colors.background);

    const row_cells = self.row_data.items(.cells);
    const row_sels = self.row_data.items(.selection);
    for (row_cells, row_sels, 0..) |row, sel, y| {
        if (y >= self.rows) break;
        const raw = row.items(.raw);
        const styles = row.items(.style);
        for (raw, styles, 0..) |cell, style, x| {
            if (x >= width) break;
            const out = &dst[y * width + x];
            out.* = .{
                .codepoint = 0,
                .fg = fg_default,
                .bg = bg_default,
                .flags = packFlags(.{ .wide = cell.wide }),
            };

            switch (cell.content_tag) {
                .codepoint, .codepoint_grapheme => out.codepoint = cell.content.codepoint.data,
                // A cell with no text but a background color; the color is in
                // the cell itself rather than the style map.
                .bg_color_palette => out.bg = packRgb(self.colors.palette[cell.content.color_palette.data]),
                .bg_color_rgb => out.bg = packRgb(.{
                    .r = cell.content.color_rgb.r,
                    .g = cell.content.color_rgb.g,
                    .b = cell.content.color_rgb.b,
                }),
            }

            // `style` is only meaningful when the cell carries a style id;
            // the default-styled cells keep the defaults filled in above.
            if (sel) |range| {
                if (x >= range[0] and x <= range[1]) {
                    var flags = unpackFlags(out.flags);
                    flags.selected = true;
                    out.flags = packFlags(flags);
                }
            }

            if (cell.style_id != 0) {
                if (resolveColor(self, style.fg_color)) |c| out.fg = c;
                if (resolveColor(self, style.bg_color)) |c| out.bg = c;
                out.flags = packFlags(mergeFlags(unpackFlags(out.flags), style.flags));
                if (style.flags.inverse) {
                    const tmp = out.fg;
                    out.fg = out.bg;
                    out.bg = tmp;
                }
            }
        }
    }
    return total;
}

fn packRgb(c: vt.color.RGB) u32 {
    return (@as(u32, c.r) << 16) | (@as(u32, c.g) << 8) | @as(u32, c.b);
}

fn resolveColor(self: *RenderState, c: vt.Style.Color) ?u32 {
    return switch (c) {
        .none => null,
        .palette => |i| packRgb(self.colors.palette[i]),
        .rgb => |v| packRgb(v),
    };
}

fn packFlags(f: CellFlags) u32 {
    return @bitCast(f);
}

fn unpackFlags(v: u32) CellFlags {
    return @bitCast(v);
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

pub fn renderRows(self: *RenderState) u16 {
    return self.rows;
}

pub fn renderCols(self: *RenderState) u16 {
    return self.cols;
}

/// The terminal's default background, 0xRRGGBB. Already reversed if the
/// terminal is in reverse-video mode.
pub fn renderBackground(self: *RenderState) u32 {
    return packRgb(self.colors.background);
}

pub fn renderForeground(self: *RenderState) u32 {
    return packRgb(self.colors.foreground);
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

/// Whether the terminal mode has the cursor shown at all.
pub fn renderCursorVisible(self: *RenderState) bool {
    return self.cursor.visible;
}

pub fn renderCursorStyle(self: *RenderState) CursorStyle {
    return self.cursor.visual_style;
}
