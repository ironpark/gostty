//! Process-global hooks ghostty needs the embedder for.
//!
//! Two things libghostty-vt cannot do on its own in a library build: decode PNG
//! bytes (a Kitty graphics transmission with `f=100`) and draw secure entropy
//! (a Kitty clipboard grant password). Both default to "unsupported": a PNG
//! transmission is refused and a grant that needs a password fails.
//!
//! Each hook is answered the way a clipboard request is: the callback carries
//! no payload, reads the pending request through the accessors, and replies
//! before returning. A zigo callback signature is a raw C signature, so handing
//! the bytes over directly would split every slice into a pointer and a length
//! and leave a borrow in Go's hands.
//!
//! Bound as `root.sys.*`. The callback type stays at the root, because a zigo
//! `.repr = .callback` entry names a type rather than a namespace.
const std = @import("std");
const vt = @import("ghostty_vt");

const Allocator = std.mem.Allocator;
const SysFn = @import("root.zig").SysFn;

var png_callback: ?SysFn = null;
var png_userdata: usize = 0;
/// The PNG bytes of the request being answered, borrowed for the callback.
var png_data: []const u8 = "";
/// The allocator the decoded pixels must come from, set only while a request
/// is pending. ghostty bounds it, so an oversized image fails the reply.
var png_alloc: ?Allocator = null;
var png_result: ?vt.sys.Image = null;

var random_callback: ?SysFn = null;
var random_userdata: usize = 0;
/// The buffer to fill, borrowed for the callback; empty when none is pending.
var random_buffer: []u8 = &.{};
var random_filled: bool = false;

fn decodePng(alloc: Allocator, data: []const u8) vt.sys.DecodeError!vt.sys.Image {
    const callback = png_callback orelse return error.InvalidData;
    png_data = data;
    png_alloc = alloc;
    png_result = null;
    defer {
        png_data = "";
        png_alloc = null;
        png_result = null;
    }
    callback(data.len, png_userdata);
    return png_result orelse error.InvalidData;
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
pub fn onPngDecodeRequest(callback: SysFn, userdata: usize) void {
    png_callback = callback;
    png_userdata = userdata;
    vt.sys.decode_png = &decodePng;
}

/// Copy the PNG bytes of the pending request into `dst` and return how many
/// were written: all of them, or `dst.len` if shorter.
pub fn pngRequestData(dst: []u8) usize {
    const n = @min(dst.len, png_data.len);
    @memcpy(dst[0..n], png_data[0..n]);
    return n;
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
pub fn onSecureRandomRequest(callback: SysFn, userdata: usize) void {
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
}
