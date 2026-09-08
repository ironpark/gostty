//! Go binding entry point. Each domain owns its handles, members and values.
//! Shared policies live in bindings/policy.zig; ownership exceptions stay beside
//! the declarations they affect. Go package paths are independent of this layout.
const zigo = @import("zigo");
const gostty = @import("gostty");
const api = @import("bindings/policy.zig").api;
const unicode = api.in("unicode");

pub const bindings = zigo.define(.{
    .root = gostty,
    .allocator = .smp_allocator,
    .io = .{ .path = "io" },
    .defaults = .{
        // ghostty spells codepoints as `u21`; every such parameter and return
        // is a Go `rune`. The `u32` ones that are also codepoints opt in.
        .codepoints = .infer_u21,
        // Every plain `[]const u8` this library hands over or takes back is
        // text: titles, URIs, needles, VT sequences. Inferring it keeps the
        // spelling out of every entry below; `.semantic = .opaque_bytes` opts
        // a byte buffer out, and every one of those is marked where it is
        // declared with what makes it bytes.
        .strings = .infer_utf8,
    },
    // Every caller-owned string here is allocator memory `freeString` hands
    // back, so it is the default rather than a spelling per entry.
    .string_release = api.ref("freeString"),
    .declarations = &declarations,
});

const declarations = list: {
    @setEvalBranchQuota(1_000_000);
    break :list @import("bindings/sys.zig").declarations ++
        @import("bindings/input.zig").declarations ++
        @import("bindings/terminal.zig").declarations ++
        @import("bindings/stream.zig").declarations ++
        @import("bindings/render.zig").declarations ++ [_]zigo.Entry{
        unicode.func("codepointWidth", .{}),
        api.func("graphemeWidth", .{ .params = &.{.{ .index = 0, .semantic = .codepoint }} }),
        api.func("freeBuffer", .{ .params = &.{.{ .index = 1, .semantic = .opaque_bytes }} }),
        api.func("freeString", .{}),
        api.func("sgrAttributeCount", .{}),
        api.func("sgrAttributeAt", .{}),
    };
};
