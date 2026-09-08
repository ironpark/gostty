//! Process-global system hooks in the sys Go package.
const p = @import("policy.zig");
const sys = api.in("sys");
const zigo = p.zigo;
const api = p.api;
const callback = p.callback;
const bytesArg = p.bytesArg;

// Process-global hooks ghostty needs the embedder for. Their own package so
// that `sys.Clear` reads as what it is, and because a renderer that never
// shows images or a program that trusts the platform's entropy never needs to
// name them. Unlike the stream callbacks, these are held by the process and
// released by `sys.Clear` -- there is no handle whose close ends them.
const sys_package = zigo.package(.{
    .path = "sys",
    .doc = "Package sys installs the process-global hooks libghostty-vt cannot provide in a library build: a PNG decoder for Kitty graphics and a secure entropy source for Kitty clipboard grants.",
    .declarations = &.{
        // The PNG bytes are copied into Go before the callback runs, so the
        // decoder may keep them.
        api.callback("PngDecodeFn", .{
            .params = &.{.{ .index = 0, .semantic = .opaque_bytes }},
            .retention = .retained,
            .reentrancy = .allowed,
            .thread = .caller,
        }).named("PngDecodeHandler"),
        callback("SecureRandomFn", "SecureRandomHandler"),
        // Answered from inside the callback the same way a clipboard request
        // is.
        sys.func("onPngDecodeRequest", .{}),
        // Decoded pixels, four bytes to a pixel.
        sys.func("replyPngImage", .{ .params = &.{bytesArg(2)} }),
        sys.func("onSecureRandomRequest", .{}),
        sys.func("clear", .{}),
        // Entropy, which is bytes by definition.
        sys.func("replySecureRandom", .{ .params = &.{bytesArg(0)} }),
    },
});

pub const declarations = [_]zigo.Entry{
    sys_package,
};
