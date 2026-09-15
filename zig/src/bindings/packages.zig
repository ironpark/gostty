//! Input encoders and process-global hooks in their own Go packages.
const p = @import("policy.zig");
const zigo = p.zigo;
const api = p.api;
const sys = api.namespace("sys");
const input = api.namespace("input");

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
            .contract = .{ .retention = .retained, .reentrancy = .allowed, .thread = .caller },
        }).with(.{ .name = "PngDecodeHandler" }),
        p.callback("SecureRandomFn", "SecureRandomHandler"),
        // The scope and the message are borrowed for the call: `logFn` renders
        // into a stack buffer, so a handler that keeps either has to copy. The
        // dispatcher copies them into Go strings before the handler runs, which
        // is what makes `func(LogLevel, string, string)` safe to hold.
        sys.callback("LogFn", .{
            .params = &.{
                .{ .index = 1, .semantic = .utf8_string },
                .{ .index = 3, .semantic = .utf8_string },
            },
            .contract = .{ .retention = .retained, .reentrancy = .allowed, .thread = .caller },
        }).with(.{ .name = "LogHandler" }),
        sys.enumeration("LogLevel", .{ .text = true }),
        // Answered from inside the callback the same way a clipboard request
        // is.
        sys.func("onPngDecodeRequest", .{}),
        // Decoded pixels, four bytes to a pixel.
        sys.func("replyPngImage", .{ .params = &.{p.bytesArg(2)} }),
        sys.func("onSecureRandomRequest", .{}),
        sys.func("onLog", .{}),
        sys.func("clear", .{}),
        // Entropy, which is bytes by definition.
        sys.func("replySecureRandom", .{ .params = &.{p.bytesArg(0)} }),
    },
});

const Key = p.enumeration("Key", .{ .text = true, .kit = true }).context();

// Key and mouse encoding is over half the public surface -- the `Key` enum
// alone is 176 constants -- and nothing on the terminal side names it, so it
// gets its own package. The dependency runs one way: `input` names
// `*Terminal`, the root package names nothing from `input`.
const input_package = zigo.package(.{
    .path = "input",
    .doc = "Package input encodes key, mouse, focus and paste events into the bytes a program reading the pty expects.",
    .declarations = &.{
        // What a key is, for a caller deciding whether a press can carry text.
        // ghostty's own methods on the enum, bound as Go value-receiver
        // methods; the two that map onto a key rather than off one are the
        // wrappers below, which take no receiver.
        Key.members(Key.funcs(.{ .names = &.{
            "codepoint",
            "printable",
            "modifier",
            "keypad",
            "leftOrRightShift",
            "leftOrRightAlt",
            "ctrlOrSuper",
            "shouldBeRemappable",
        } }) ++ &[_]zigo.Entry{
            Key.func("w3c", .{ .name = "W3C" }),
        }),
        p.enumeration("KeyAction", .{ .text = true }),
        api.value("KeyEvent", .{ .fields = &.{.{ .name = "unshifted_codepoint", .semantic = .codepoint }} }),
        p.flags("KeyMods", "_padding"),
        p.enumeration("FocusEvent", .{ .text = true }),
        p.enumeration("MouseAction", .{ .text = true }),
        p.enumeration("MouseButton", .{ .text = true }),
        api.value("MouseEvent", .{}),
        api.value("RenderSize", .{}),

        api.func("encodeKey", .{}),
        api.func("encodeMouse", .{}),
        // ghostty's own constructors, bound where they are declared rather
        // than through a wrapper that would only forward: the documentation
        // and the behaviour are then theirs, and there is no second body to
        // drift. `.name` is what keeps the `Key` prefix in Go, and `.symbol`
        // stops the owner and the Go name from both spelling `key` into the
        // exported C name.
        Key.func("fromASCII", .{ .name = "keyFromASCII", .symbol = "zg_key_from_ascii" }),
        Key.func("fromW3C", .{
            .name = "keyFromW3C",
            .symbol = "zg_key_from_w3c",
            .params = &.{.{ .index = 0, .go_name = "w3cCode" }},
        }),
        // ghostty's own encoders, reached through the `input` namespace.
        input.func("encodeFocus", .{ .params = &.{
            .{ .index = 0, .go_name = "writer" },
            .{ .index = 1, .go_name = "event" },
        } }),
        // A paste is checked for control bytes before it is framed, so what
        // goes in is whatever the clipboard held, not guaranteed text.
        input.func("isSafePaste", .{ .params = &.{
            .{ .index = 0, .go_name = "data", .semantic = .opaque_bytes },
        } }),
        api.func("encodePaste", .{ .params = &.{p.bytesArg(2)} }),
    },
});

pub const declarations = [_]zigo.Entry{ sys_package, input_package };
