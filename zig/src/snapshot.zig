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
const io = common.io;

// Snapshots: ghostty's binary representation of a whole terminal, active
// state first so a restored terminal renders before its history arrives.
// Writing one is `Stream.writeSnapshot`, since the stream owns the
// unfinished-sequence state a snapshot carries.

/// A decoded snapshot: ghostty's own type, bound as a handle. Restore it into
/// a terminal with `restoreInto`, then feed `continuation` to a fresh stream
/// on it to resume mid-sequence.
pub const Snapshot = vt.snapshot.Decoded;

// Bind the native options struct with zigo.param.flatten; allocator and Io
// are injected and the returned value is boxed by zigo.
pub const decodeSnapshot = vt.snapshot.decode;

/// Replace `term` with the terminal the snapshot holds: its size, screens,
/// scrollback, modes and colors. The snapshot gives its terminal up once;
/// a second call fails. Streams on `term` keep pointing at it, but their
/// parser state belongs to the old contents, so open a new stream and feed
/// it `continuation` before any new input.
///
/// Kept beside `snapshotTerminal` rather than replaced by it: this one keeps
/// the caller's handle identity, so anything already holding that `*Terminal`
/// goes on holding the right one. Take the new handle when there is nothing to
/// keep, restore into an existing one when there is.
pub fn snapshotRestoreInto(self: *Snapshot, gpa: Allocator, term: *Terminal) error{TerminalTaken}!void {
    if (self.terminal == null) return error.TerminalTaken;
    const restored = self.toOwned();
    term.deinit(gpa);
    term.* = restored;
}

/// The terminal the snapshot holds, as a handle of its own: its size, screens,
/// scrollback, modes and colors. The snapshot gives its terminal up once; a
/// second call, or a call after `restoreInto`, fails.
///
/// The returned terminal has no streams. Open one and feed it `continuation`
/// before any new input, the same as after a restore.
pub fn snapshotTerminal(self: *Snapshot) error{TerminalTaken}!Terminal {
    if (self.terminal == null) return error.TerminalTaken;
    return self.toOwned();
}

// `Snapshot` is the decoded form itself, so the decoder's entry points
// delegate here rather than restate the one-shot rule.
comptime {
    std.debug.assert(Snapshot == vt.snapshot.Decoded);
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
    /// The Kitty graphics storage limit `ready` restores each screen with,
    /// or null for the default of a new screen.
    kitty_image_storage_limit: ?usize = null,

    /// zigo boxes the struct after this returns it by value, so the reader
    /// moves and the pointer the decoder holds goes stale. Every entry point
    /// re-points it; the reader keeps its own position, so this loses nothing.
    fn rebind(self: *SnapshotDecoder) void {
        self.inner.source = &self.reader;
    }
};

/// Which kind of record one `snapshotDecoderNext` consumed.
///
/// Backed by a u32 so it fills the last slot of `SnapshotProgress` exactly and
/// the struct needs no padding fields.
pub const SnapshotRecord = enum(u32) {
    /// A page of scrollback, with its Kitty graphics placements, described by
    /// the rows and remaining fields.
    page,
    /// The pixels of a Kitty graphics image or animation frame, described by
    /// the image ID, frame and bytes fields.
    image,
};

/// One record `snapshotDecoderNext` consumed: a page of scrollback, or the
/// pixels of a Kitty graphics image.
///
/// ghostty spells this as a tagged union. It is flattened here because a
/// tagged union cannot come back optional, and `next` reports the end of the
/// snapshot with null; the fields of the record `kind` does not name are zero.
///
/// ghostty also names the screen each record went to. It is not here: its enum
/// is backed by the narrowest integer that fits the keys, which an extern
/// struct cannot hold, and `next` routes the record itself so the key is
/// informational.
pub const SnapshotProgress = extern struct {
    /// Rows a page prepended above what that screen already had, or zero when
    /// the page was read and validated but dropped -- because the screen is
    /// gone, the terminal was resized, or the scrollback has no room.
    rows: u64,
    /// The pixel bytes attached to an image, or zero when the pixels were
    /// read and validated but dropped.
    bytes: u64,
    /// Pages still to come for the same screen.
    remaining: u32,
    /// The ID of the image the pixels are for.
    image_id: u32,
    /// The frame the pixels are for, as the Kitty graphics protocol counts
    /// frames: 1 is the image itself and 2 and up are its animation frames.
    frame: u32,
    /// Which record this is, and so which of the fields above are set.
    kind: SnapshotRecord,
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
        .kitty_image_storage_limit = self.kitty_image_storage_limit,
    });
}

/// Restore each screen with a Kitty graphics image storage limit of `bytes`
/// rather than the default of a new screen. Call it before `ready`.
///
/// The snapshot does not carry the limit of the terminal that wrote it, so
/// the decoder always applies one. Images are restored oldest first, and one
/// that does not fit evicts older ones, as a live transmission would. Zero
/// turns Kitty graphics off on the restored terminal.
pub fn snapshotDecoderSetKittyImageStorageLimit(self: *SnapshotDecoder, bytes: usize) error{AlreadyReady}!void {
    if (self.decoded != null) return error.AlreadyReady;
    self.kitty_image_storage_limit = bytes;
}

/// How many Kitty graphics images `ready` restored on each screen, and how
/// many bytes of their pixels are still to come through `next`.
///
/// For a progress bar while the pixels arrive: add up the `bytes` of each
/// image record `next` returns. Pixels the decoder drops report zero bytes,
/// so the sum can end below the total.
pub const SnapshotKittyCounts = extern struct {
    /// Bytes of image and frame pixels still to come for the primary screen.
    primary_pending_bytes: u64,
    /// The same for the alternate screen.
    alternate_pending_bytes: u64,
    /// Images restored on the primary screen.
    primary_images: u32,
    /// Images restored on the alternate screen.
    alternate_images: u32,
};

/// The Kitty graphics counts `ready` found, all zero before it.
pub fn snapshotDecoderKittyCounts(self: *SnapshotDecoder) SnapshotKittyCounts {
    const decoded = if (self.decoded) |*d| d else return std.mem.zeroes(SnapshotKittyCounts);
    const primary = decoded.kitty.get(.primary);
    const alternate = decoded.kitty.get(.alternate);
    return .{
        .primary_pending_bytes = primary.pending_bytes,
        .alternate_pending_bytes = alternate.pending_bytes,
        .primary_images = primary.images,
        .alternate_images = alternate.images,
    };
}

/// Replace `term` with the decoded terminal. See `snapshotRestoreInto`; the
/// same one-shot rule applies, and `next` wants the same terminal afterwards.
pub fn snapshotDecoderRestoreInto(self: *SnapshotDecoder, gpa: Allocator, term: *Terminal) error{TerminalTaken}!void {
    const decoded = if (self.decoded) |*d| d else return error.TerminalTaken;
    return snapshotRestoreInto(decoded, gpa, term);
}

/// The unfinished sequence the snapshot was taken in, empty at ground.
pub fn snapshotDecoderContinuation(self: *SnapshotDecoder) []const u8 {
    const decoded = if (self.decoded) |*d| d else return "";
    return snapshotContinuation(decoded);
}

/// Decode one record of history into `term`, which must be the one
/// `restoreInto` filled: a page is prepended to its screen, and Kitty graphics
/// pixels are attached to their image.
///
/// Returns null once the snapshot is complete. Wire errors are fatal -- the
/// position in the stream is lost -- but a record that cannot be applied is
/// reported as zero rows or bytes rather than failing, because the terminal
/// is live and may have moved on since `ready`.
pub fn snapshotDecoderNext(self: *SnapshotDecoder, gpa: Allocator, term: *Terminal) !?SnapshotProgress {
    self.rebind();
    const progress = try self.inner.next(gpa, term) orelse return null;
    return switch (progress) {
        .page => |page| .{
            .kind = .page,
            .rows = page.rows,
            .bytes = 0,
            .remaining = page.remaining,
            .image_id = 0,
            .frame = 0,
        },
        .image => |image| .{
            .kind = .image,
            .rows = 0,
            .bytes = image.bytes,
            .remaining = 0,
            .image_id = image.image_id,
            .frame = image.frame,
        },
    };
}
