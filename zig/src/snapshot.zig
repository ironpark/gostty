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
