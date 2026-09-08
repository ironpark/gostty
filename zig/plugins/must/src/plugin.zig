//! `must`: `Must<Name>` variants that panic instead of returning an error,
//! chosen one declaration at a time.
//!
//! Declaration options select the variants; typed analysis records the decision
//! once and verifies the final public signature and name before emission.
//!
//! The variant never spells its own signature. It renders the one the
//! generator just wrote for the checked method and takes `error` off the end,
//! so the two cannot disagree about a parameter name, a package qualifier or
//! a borrowed-view result. Everything this plugin decides for itself is which
//! of three wrappers to call, and that follows from how many results are left.
const std = @import("std");
const abi = @import("abi");
const plugin_api = @import("plugin");
const semantic = @import("semantic");

/// The plugin's name: the `ext` key its options travel under and the prefix
/// of the diagnostics it reports. Not `MUST`, which is the built-in variant
/// generator: two plugins of one name would report `MUST002` from either and
/// leave `go-doctor` naming the same thing twice.
pub const name = "MUSTOPT";

/// A declaration says `extend(must.plugin, .{})`. There is nothing to
/// configure: asking for the variant is the whole decision.
pub const Options = struct {};

pub const plugin: plugin_api.Plugin = .{
    .name = name,
    .FunctionOptions = Options,
    .targets = &.{.function},
    .min_contract = .{ .major = 2, .minor = 0 },
    .Facts = struct { enabled: bool },
    .analyze = analyze,
    .method_hook = methodHook,
    .go_files = &.{.{ .enabled = packageHasVariant, .pathAlloc = helperPath, .render = renderHelpers }},
};

fn methodHook(context: plugin_api.Context, writer: *std.Io.Writer, function: abi.AbiFn) !void {
    const fact = try context.options.facts.get(plugin, .function(function.origin.*)) orelse return;
    if (!fact.enabled) return;
    const method = context.method.?;

    try writer.print(
        "\n// Must{0s} calls {0s} and panics with its typed error on failure.\n",
        .{method.go_name},
    );
    if (method.receiver) |receiver|
        try writer.print("func ({s} *{s}) Must{s}", .{ method.receiver_name.?, receiver, method.go_name })
    else
        try writer.print("func Must{s}", .{method.go_name});
    // The parameter list and the results are the generator's own, with only
    // the trailing error taken off; what is left is counted rather than
    // parsed, and the count picks the wrapper.
    try context.writeParameters(writer, function);
    const results = try context.writeResultType(writer, function, .{ .omit_error = true });
    try writer.writeAll(" { ");
    try writer.writeAll(switch (results) {
        0 => "gosttyMustSucceed(",
        1 => "return gosttyMustValue(",
        else => "return gosttyMustMatch(",
    });
    if (method.receiver_name) |receiver_name|
        try writer.print("{s}.{s}(", .{ receiver_name, method.go_name })
    else
        try writer.print("{s}(", .{method.go_name});
    try context.writeCallArguments(writer, function);
    try writer.writeAll(")) }\n");
}

fn helperPath(context: plugin_api.Context) ![]u8 {
    return context.goFilePathAlloc(helper_file);
}

const helper_file = "zigo_must_gen.go";

/// The three wrappers the variants delegate to. They are the plugin's own
/// rather than the generator's `zigoMust`, which is written only when the
/// built-in variants or a tagged-union projection ask for it -- a plugin that
/// borrowed it would compile or not depending on an unrelated declaration.
///
/// A package with no `Must` variant writes nothing, and the frame drops a file
/// whose body came out empty.
fn renderHelpers(_: plugin_api.Context, writer: *std.Io.Writer) !void {
    try writer.writeAll(
        "// gosttyMustSucceed panics with a typed error, for a Must variant of a\n" ++
            "// function whose only result is the error.\n" ++
            "func gosttyMustSucceed(err error) {\n" ++
            "\tif err != nil {\n\t\tpanic(err)\n\t}\n" ++
            "}\n\n" ++
            "// gosttyMustValue panics with a typed error, for a Must variant with one result.\n" ++
            "func gosttyMustValue[T any](value T, err error) T {\n" ++
            "\tif err != nil {\n\t\tpanic(err)\n\t}\n" ++
            "\treturn value\n" ++
            "}\n\n" ++
            "// gosttyMustMatch panics with a typed error, for a Must variant whose result\n" ++
            "// carries a presence flag beside the value.\n" ++
            "func gosttyMustMatch[T any](value T, matched bool, err error) (T, bool) {\n" ++
            "\tif err != nil {\n\t\tpanic(err)\n\t}\n" ++
            "\treturn value, matched\n" ++
            "}\n",
    );
}

fn packageHasVariant(context: plugin_api.Context) !bool {
    for (context.program.functions) |function| {
        if (!plugin_api.packageMatches(function.origin.package, context.options.active_package)) continue;
        if (try context.options.facts.get(plugin, .function(function.origin.*))) |fact| {
            if (fact.enabled) return true;
        }
    }
    return false;
}

fn analyze(context: plugin_api.AnalyzeContext) !void {
    const render = context.render;
    const allocator = render.allocator;
    const functions = render.program.functions;
    const info = try allocator.alloc(plugin_api.FunctionInfo, functions.len);
    for (functions, info) |function, *entry| entry.* = try render.functionInfo(function);
    for (functions, info) |function, entry| {
        _ = try render.functionOptions(plugin, function.origin.*) orelse continue;
        if (!entry.is_public or !entry.has_error) {
            try context.diagnose(.{
                .severity = .@"error",
                .code = name ++ "002",
                .message = try std.fmt.allocPrint(allocator, "`{s}` needs a public checked Go signature for a Must variant", .{entry.go_name}),
                .site = plugin_api.site.functionSite(function.origin.*),
                .hint = "Select a public method or a free function that can return an error.",
            });
        }
        const enabled = entry.is_public and entry.has_error;
        try context.facts.put(allocator, plugin, .function(function.origin.*), .{ .enabled = enabled });
        if (!enabled) continue;
        const must_name = try std.fmt.allocPrint(allocator, "Must{s}", .{entry.go_name});
        const origin = function.origin.*;
        const path = try plugin_api.site.functionDeclarationAlloc(allocator, origin);
        if (origin.receiver == null) for (render.program.types) |declaration| {
            if (!semantic.optionalStringEqual(declaration.package, origin.package) or !std.mem.eql(u8, declaration.name, must_name)) continue;
            try context.diagnose(.{
                .severity = .@"error",
                .code = name ++ "004",
                .message = try std.fmt.allocPrint(allocator, "public Go name `{s}` collides between type `{s}` and generated Must variant for `{s}`", .{ must_name, declaration.zig_path orelse declaration.name, path }),
                .site = plugin_api.site.functionSiteFor(origin, path),
                .hint = "rename the function or conflicting type so the generated Must name is unique",
            });
        };
        for (functions, info) |other, other_info| {
            if (!other_info.is_public or !semantic.optionalStringEqual(origin.receiver, other.origin.receiver) or !semantic.optionalStringEqual(origin.package, other.origin.package) or !std.mem.eql(u8, must_name, other_info.go_name)) continue;
            const other_path = try plugin_api.site.functionDeclarationAlloc(allocator, other.origin.*);
            try context.diagnose(.{
                .severity = .@"error",
                .code = name ++ "004",
                .message = try std.fmt.allocPrint(allocator, "public Go name `{s}` collides between `{s}` and generated Must variant for `{s}`", .{ must_name, other_path, path }),
                .site = plugin_api.site.functionSiteFor(origin, path),
                .hint = "rename one declaration so the generated Must name is unique",
            });
        }
    }
}
