//! Key, mouse, focus and paste encoding in the input Go package.
const p = @import("policy.zig");
const input = api.in("input");
const zigo = p.zigo;
const api = p.api;
const flags = p.flags;
const enumeration = p.enumeration;
const bytesArg = p.bytesArg;

const Key = enumeration("Key", .{ .text = true, .kit = true }).context();

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
        Key.define(Key.funcs(.{ .names = &.{
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
        enumeration("KeyAction", .{ .text = true }),
        api.val("KeyEvent", .{ .fields = &.{.{ .name = "unshifted_codepoint", .semantic = .codepoint }} }),
        flags("KeyMods", "_padding"),
        enumeration("FocusEvent", .{ .text = true }),
        enumeration("MouseAction", .{ .text = true }),
        enumeration("MouseButton", .{ .text = true }),
        api.val("MouseEvent", .{}),
        api.val("RenderSize", .{}),

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
        api.func("encodePaste", .{ .params = &.{bytesArg(2)} }),
    },
});

pub const declarations = [_]zigo.Entry{
    input_package,
};
