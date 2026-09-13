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
    .doc = "Package sys installs the process-global hooks libghostty-vt cannot provide in a library build: a PNG decoder for Kitty graphics, a secure entropy source for Kitty clipboard grants, and a sink for the diagnostics ghostty writes with std.log.",
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
        // The scope and the message are borrowed for the call: `logFn` renders
        // into a stack buffer, so a handler that keeps either has to copy. The
        // dispatcher copies them into Go strings before the handler runs, which
        // is what makes `func(LogLevel, string, string)` safe to hold.
        sys.callback("LogFn", .{
            .params = &.{
                .{ .index = 1, .semantic = .utf8_string },
                .{ .index = 3, .semantic = .utf8_string },
            },
            .retention = .retained,
            .reentrancy = .allowed,
            .thread = .caller,
        }).named("LogHandler"),
        sys.enumType("LogLevel", .{}).use(p.zigo.features.text, .{}),
        // Answered from inside the callback the same way a clipboard request
        // is.
        sys.func("onPngDecodeRequest", .{}),
        // Decoded pixels, four bytes to a pixel.
        sys.func("replyPngImage", .{ .params = &.{bytesArg(2)} }),
        sys.func("onSecureRandomRequest", .{}),
        sys.func("onLog", .{}),
        sys.func("clear", .{}),
        // Entropy, which is bytes by definition.
        sys.func("replySecureRandom", .{ .params = &.{bytesArg(0)} }),
    },
});

pub const declarations = [_]zigo.Entry{
    sys_package,
};
