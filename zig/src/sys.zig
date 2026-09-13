//! Process-global hooks ghostty needs the embedder for.
//!
//! Two things libghostty-vt cannot do on its own in a library build: decode PNG
//! bytes (a Kitty graphics transmission with `f=100`) and draw secure entropy
//! (a Kitty clipboard grant password). Both default to "unsupported": a PNG
//! transmission is refused and a grant that needs a password fails.
//!
//! Each hook is answered from inside the callback, the way a clipboard request
//! is: the callback receives the request and replies before returning.
//!
//! Bound as `root.sys.*`. The callback types are declared at the Zig root
//! because a `.callback` entry names a type rather than a namespace, but that
//! is a Zig-side placement only: the generated `PngDecodeHandler` and
//! `SecureRandomHandler` land in the Go `sys` package with everything else
//! `zigo.package` collects, not at the Go root.
const std = @import("std");
const vt = @import("ghostty_vt");

const Allocator = std.mem.Allocator;
const common = @import("common.zig");
const root = @import("root.zig");
const PngDecodeFn = root.PngDecodeFn;
const SecureRandomFn = root.SecureRandomFn;

var png_callback: ?PngDecodeFn = null;
var png_userdata: usize = 0;
/// The allocator the decoded pixels must come from, set only while a request
/// is pending. ghostty bounds it, so an oversized image fails the reply.
var png_alloc: ?Allocator = null;
var png_result: ?vt.sys.Image = null;

/// How serious a log line is, mirroring `std.log.Level` in its order.
pub const LogLevel = enum(u8) {
    /// Something has gone wrong. It may be recoverable.
    err,
    /// It is uncertain whether something has gone wrong, but it is worth
    /// investigating.
    warn,
    /// General messages about what the terminal is doing.
    info,
    /// Only useful while debugging. Nothing at this level is emitted unless
    /// the native library was built in Debug.
    debug,
};

/// Receives one log line from ghostty. `scope` names the subsystem and
/// `message` is already formatted. Both are borrowed for the call: copy them
/// to keep them. `userdata` is last, where zigo expects the handle.
pub const LogFn = *const fn (
    level: LogLevel,
    scope: [*]const u8,
    scope_len: usize,
    message: [*]const u8,
    message_len: usize,
    userdata: usize,
) callconv(.c) void;

var log_callback: ?LogFn = null;
var log_userdata: usize = 0;

var random_callback: ?SecureRandomFn = null;
var random_userdata: usize = 0;
/// The buffer to fill, borrowed for the callback; empty when none is pending.
var random_buffer: []u8 = &.{};
var random_filled: bool = false;

fn decodePng(alloc: Allocator, data: []const u8) vt.sys.DecodeError!vt.sys.Image {
    const callback = png_callback orelse return error.InvalidData;
    png_alloc = alloc;
    png_result = null;
    defer {
        png_alloc = null;
        png_result = null;
    }
    callback(data.ptr, data.len, png_userdata);
    return png_result orelse error.InvalidData;
}

/// What `std_options.logFn` points at. `std.log` calls this with the format
/// and its arguments still separate, so the message is rendered here; there is
/// no allocator on this path and no failure it could report, so the buffer is
/// a fixed one on the stack and a message longer than it is cut rather than
/// dropped. Called on whichever thread produced the log line, which for a
/// parse diagnostic is the thread inside `feed`.
pub fn logFn(
    comptime level: std.log.Level,
    comptime scope: @EnumLiteral(),
    comptime format: []const u8,
    args: anytype,
) void {
    const callback = log_callback orelse return;
    var buffer: [2048]u8 = undefined;
    var writer: std.Io.Writer = .fixed(&buffer);
    // Truncation is the only outcome worth having: a diagnostic that does not
    // fit is still worth most of itself.
    writer.print(format, args) catch {};
    const message = writer.buffered();
    const scope_name = @tagName(scope);
    callback(
        common.mirror(LogLevel, level),
        scope_name.ptr,
        scope_name.len,
        message.ptr,
        message.len,
        log_userdata,
    );
}

/// Install the log sink. ghostty writes its internal diagnostics with
/// `std.log`, which reads the compile root's `std_options` -- the generated
/// shim, which forwards this library's. Without a sink those lines go to
/// stderr and an embedder has no way to take them.
///
/// Which lines arrive is `std.log`'s own threshold, not this hook's: `.info`
/// and above for the release builds, everything in Debug. A parse diagnostic
/// -- an unimplemented mode, a malformed Kitty payload -- is `.warn`, so it
/// arrives in the archives this repository ships.
///
/// Process-global, and called on whichever thread logged, so a handler that
/// touches shared state has to do its own locking. The strings are valid for
/// the call only.
pub fn onLog(callback: LogFn, userdata: usize) void {
    log_callback = callback;
    log_userdata = userdata;
}

fn randomSecure(buffer: []u8) vt.sys.RandomSecureError!void {
    const callback = random_callback orelse return error.EntropyUnavailable;
    random_buffer = buffer;
    random_filled = false;
    defer random_buffer = &.{};
    callback(buffer.len, random_userdata);
    if (!random_filled) return error.EntropyUnavailable;
}

/// Install the PNG decoder. Until one is installed ghostty refuses PNG
/// Kitty transmissions outright; with one, the image is decoded as it
/// arrives and reaches `kittyImage` as `rgba`. Process-global, and read
/// from whichever thread feeds a stream, so install it at startup.
pub fn onPngDecodeRequest(callback: PngDecodeFn, userdata: usize) void {
    png_callback = callback;
    png_userdata = userdata;
    vt.sys.decode_png = &decodePng;
}

/// Answer the pending PNG request with decoded pixels, four bytes per pixel,
/// row-major. `rgba` is copied; it must be exactly `width * height * 4` bytes.
pub fn replyPngImage(width: u32, height: u32, rgba: []const u8) error{ NoPendingRequest, SizeMismatch, OutOfMemory }!void {
    const alloc = png_alloc orelse return error.NoPendingRequest;
    const expected = @as(u64, width) * @as(u64, height) * 4;
    if (rgba.len != expected) return error.SizeMismatch;
    if (png_result) |old| alloc.free(old.data);
    png_result = .{
        .width = width,
        .height = height,
        .data = try alloc.dupe(u8, rgba),
    };
}

/// Install the secure entropy source. ghostty uses it for secrets, so it
/// must be a real CSPRNG; on failure the operation that needed the bytes
/// fails rather than falling back to weaker randomness. Without one,
/// ghostty draws from the platform through its own `std.Io`, so this is
/// for embedders that want to control the source.
pub fn onSecureRandomRequest(callback: SecureRandomFn, userdata: usize) void {
    random_callback = callback;
    random_userdata = userdata;
    vt.sys.random_secure = &randomSecure;
}

/// Answer the pending request. `bytes` is copied and must be exactly as
/// long as the callback's `len`.
pub fn replySecureRandom(bytes: []const u8) error{ NoPendingRequest, SizeMismatch }!void {
    if (random_buffer.len == 0) return error.NoPendingRequest;
    if (bytes.len != random_buffer.len) return error.SizeMismatch;
    @memcpy(random_buffer, bytes);
    random_filled = true;
}

/// Remove both hooks: PNG transmissions are refused again and entropy
/// comes from the platform. Releases the Go callbacks.
pub fn clear() void {
    png_callback = null;
    vt.sys.decode_png = null;
    random_callback = null;
    vt.sys.random_secure = null;
    log_callback = null;
}
