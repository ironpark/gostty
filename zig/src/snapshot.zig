//! Saving and restoring a whole terminal.
//!
//! ghostty's binary snapshot carries the active state first, so a restored
//! terminal renders before its history arrives. Writing one is
//! `Stream.writeSnapshot`, since the stream owns the unfinished-sequence state
//! a snapshot records.
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

// -- Incremental decoding ---------------------------------------------------
//
// `decodeSnapshot` reads the whole thing before it returns anything, which
// throws away what the format was built for: the active state comes first, so
// a terminal can be on screen before its scrollback has been read. `Decoder`
// is that boundary -- `ready` gives the renderable terminal, `next` prepends
// one page of history at a time -- and a caller can put a frame between the
// two.
//
// It owns its bytes rather than reading from a Go `io.Reader`: the decoder
// holds its source across calls, and a reader that crosses the boundary is
// only alive for the one call it was passed to. A snapshot is a thing you
// have in a file, so having it in memory is no imposition.

/// A snapshot being decoded a piece at a time.
pub const SnapshotDecoder = struct {
    /// The encoded snapshot, owned.
    bytes: []const u8,
    reader: std.Io.Reader,
    inner: vt.snapshot.Decoder,
    /// What `ready` produced, until `restoreInto` takes the terminal.
    decoded: ?vt.snapshot.Decoded = null,

    /// zigo boxes the struct after this returns it by value, so the reader
    /// moves and the pointer the decoder holds goes stale. Every entry point
    /// re-points it; the reader keeps its own position, so this loses nothing.
    fn rebind(self: *SnapshotDecoder) void {
        self.inner.source = &self.reader;
    }
};

/// How much one `snapshotDecoderNext` applied.
///
/// ghostty also names the screen the page went to. It is not here: its enum is
/// backed by the narrowest integer that fits the keys, which an extern struct
/// cannot hold, and `next` routes the page itself so the key is informational.
pub const SnapshotProgress = extern struct {
    /// Rows prepended above what that screen already had, or zero when the
    /// page was read and validated but dropped -- because the screen is gone,
    /// the terminal was resized, or the scrollback has no room.
    rows: u64,
    /// Pages still to come for the same screen.
    remaining: u32,
    _pad: u32 = 0,
};

/// Start decoding `data`, which is copied.
///
/// Nothing is read until `snapshotDecoderReady`.
pub fn newSnapshotDecoder(gpa: Allocator, data: []const u8) !SnapshotDecoder {
    const owned = try gpa.dupe(u8, data);
    errdefer gpa.free(owned);
    var result: SnapshotDecoder = .{
        .bytes = owned,
        .reader = .fixed(owned),
        .inner = undefined,
    };
    // Initialized here rather than in `ready` so that calling `next` first
    // finds a decoder in its `start` state and is refused, instead of reading
    // undefined memory.
    result.inner = .init(&result.reader);
    return result;
}

pub fn freeSnapshotDecoder(self: *SnapshotDecoder, gpa: Allocator) void {
    if (self.decoded) |*decoded| decoded.deinit(gpa);
    gpa.free(self.bytes);
}

/// Decode as far as the READY marker: everything a terminal needs to be drawn.
///
/// `max_continuation_bytes` bounds the unfinished-sequence suffix. After this
/// the terminal can be taken with `restoreInto` and drawn, and the history
/// arrives through `next` at whatever pace suits.
pub fn snapshotDecoderReady(self: *SnapshotDecoder, gpa: Allocator, max_continuation_bytes: usize) !void {
    if (self.decoded != null) return error.AlreadyReady;
    self.rebind();
    self.decoded = try self.inner.ready(gpa, io, .{
        .max_continuation_bytes = max_continuation_bytes,
    });
}

/// Replace `term` with the decoded terminal. See `snapshotRestoreInto`; the
/// same one-shot rule applies, and `next` wants the same terminal afterwards.
pub fn snapshotDecoderRestoreInto(self: *SnapshotDecoder, gpa: Allocator, term: *Terminal) error{TerminalTaken}!void {
    const decoded = if (self.decoded) |*d| d else return error.TerminalTaken;
    if (decoded.terminal == null) return error.TerminalTaken;
    const restored = decoded.toOwned();
    term.deinit(gpa);
    term.* = restored;
}

/// The unfinished sequence the snapshot was taken in, empty at ground.
pub fn snapshotDecoderContinuation(self: *SnapshotDecoder) []const u8 {
    const decoded = self.decoded orelse return "";
    return switch (decoded.continuation) {
        .ground => "",
        .bytes => |bytes| bytes,
    };
}

/// Decode one page of history and prepend it to its screen in `term`, which
/// must be the one `restoreInto` filled.
///
/// Returns null once the snapshot is complete. Wire errors are fatal -- the
/// position in the stream is lost -- but a page that cannot be applied is
/// reported as zero rows rather than failing, because the terminal is live
/// and may have moved on since `ready`.
pub fn snapshotDecoderNext(self: *SnapshotDecoder, gpa: Allocator, term: *Terminal) !?SnapshotProgress {
    self.rebind();
    const progress = try self.inner.next(gpa, term) orelse return null;
    return .{ .rows = progress.rows, .remaining = progress.remaining };
}
