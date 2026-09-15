//! `must`: `Must<Name>` variants that panic instead of returning an error,
//! chosen one declaration at a time.
//!
//! Declaration options select the variants; typed analysis records the decision
//! once and verifies the final public signature and name before emission.
//!
//! The variant never spells its own signature. It takes the one the
//! generator just wrote for the checked method and drops `error` off the end,
//! so the two cannot disagree about a parameter name, a package qualifier or
//! a borrowed-view result. Everything this plugin decides for itself is which
//! of three wrappers to call, and that follows from how many results are left.
const std = @import("std");
const plugin_api = @import("plugin");
const semantic = @import("semantic");

/// The plugin's name: the `ext` key its options travel under and the prefix
/// of the diagnostics it reports. Not `MUST`, which is the built-in variant
/// generator: two plugins of one name would report `MUST002` from either and
/// leave `go-doctor` naming the same thing twice.
pub const name = "MUSTOPT";

/// A declaration says `extend(must.plugin, .{})` for the variant beside the
/// checked method, or `.{ .replace = true }` for the variant instead of it.
pub const Options = struct {
    /// Take the declaration's whole public surface. The panicking form is
    /// written under the plain name and the checked form is not exported at
    /// all, so one declaration is one Go method rather than two.
    ///
    /// For the reads where an error is not a condition to branch on -- a field
    /// off a live handle -- this is what Go does elsewhere: `reflect.Value.Int`
    /// panics on misuse rather than offering `MustInt`. It is the right choice
    /// only where a caller has nothing to do with the error, which is the same
    /// judgement asking for the variant at all already requires.
    replace: bool = false,
};

pub const plugin: plugin_api.Plugin = .{
    .name = name,
    .FunctionOptions = Options,
    .subjects = &.{.function},
    .Facts = struct { enabled: bool, replace: bool },
    .analyze = analyze,
    .go = .{
        .visit = visit,
        .claims = claims,
        .source_files = &.{.{ .enabled = packageHasVariant, .pathAlloc = helperPath, .render = renderHelpers }},
    },
};

/// The variant, written after the checked method it mirrors.
fn visit(context: plugin_api.GoContext, node: plugin_api.Node, b: *plugin_api.Builder) !void {
    const function = switch (node) {
        .function => |value| value,
        else => return,
    };
    const fact = try context.facts.get(plugin, .function(function.origin.*)) orelse return;
    if (!fact.enabled) return;
    const method = context.method.?;
    const allocator = context.allocator;

    // Claimed or not, the wrapper is the same shape: the generator's own
    // signature with the trailing error removed, calling the body it wrote.
    // What differs is the name it is written under and the name it calls --
    // `checked_name` is the unexported one on a claimed declaration.
    const public_name = if (fact.replace)
        method.public_name
    else
        try std.fmt.allocPrint(allocator, "Must{s}", .{method.public_name});
    const doc = if (fact.replace)
        try std.fmt.allocPrint(
            allocator,
            "{0s} panics with its typed error on failure. The error is a dead or\npoisoned handle, which is a defect rather than a condition to branch on.",
            .{method.public_name},
        )
    else
        try std.fmt.allocPrint(allocator, "{0s} calls {1s} and panics with its typed error on failure.", .{ public_name, method.public_name });

    const callee = if (method.receiver_name) |receiver|
        try b.selName(receiver, method.checked_name)
    else
        b.ident(method.checked_name);
    const forwarded = try b.callForwarding(callee, function);
    // The parameter list and the results are the generator's own, with only
    // the trailing error taken off; what is left is counted rather than
    // parsed, and the count picks the wrapper.
    const body: plugin_api.gobuild.Stmt = switch (try b.resultCount(function, .{ .omit_error = true })) {
        0 => b.exprStmt(try b.callName("gosttyMustSucceed", &.{forwarded})),
        1 => try b.ret(&.{try b.callName("gosttyMustValue", &.{forwarded})}),
        else => try b.ret(&.{try b.callName("gosttyMustMatch", &.{forwarded})}),
    };
    try b.emit(&.{try b.func(.{
        .doc = .{ .text = doc },
        .receiver = if (method.receiver) |receiver| .{ .name = method.receiver_name.?, .type = receiver, .pointer = true } else null,
        .name = public_name,
        .signature = .{ .function = .{ .function = function, .options = .{ .omit_error = true } } },
        .body = &.{body},
        .single_line = true,
    })}, .{ .blank_before = true });
}

fn helperPath(context: plugin_api.GoContext) ![]u8 {
    return context.sourceFilePathAlloc(helper_file);
}

const helper_file = "zigo_must_gen.go";

/// The three wrappers the variants delegate to. They are the plugin's own
/// rather than the generator's `zigoMust`, which is written only when the
/// built-in variants or a tagged-union projection ask for it -- a plugin that
/// borrowed it would compile or not depending on an unrelated declaration.
///
/// A package with no `Must` variant writes nothing, and the frame drops a file
/// whose body came out empty.
fn renderHelpers(_: plugin_api.GoContext, writer: *std.Io.Writer) !void {
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

fn packageHasVariant(context: plugin_api.GoContext) !bool {
    for (context.program.functions) |function| {
        if (!plugin_api.packageMatches(function.origin.package, context.options.active_package)) continue;
        if (try context.facts.get(plugin, .function(function.origin.*))) |fact| {
            if (fact.enabled) return true;
        }
    }
    return false;
}

fn analyze(context: plugin_api.AnalyzeContext) !void {
    const render = context.go orelse return;
    const allocator = render.allocator;
    const functions = render.program.functions;
    const info = try allocator.alloc(plugin_api.FunctionInfo, functions.len);
    for (functions, info) |function, *entry| entry.* = try render.functionInfo(function);
    for (functions, info) |function, entry| {
        const options = try render.optionsOf(plugin, .function, function.origin.ext) orelse continue;
        if (!entry.is_public or !entry.has_error) {
            try context.diagnose(.{
                .severity = .@"error",
                .code = name ++ "002",
                .message = try std.fmt.allocPrint(allocator, "`{s}` needs a public checked Go signature for a Must variant", .{entry.public_name}),
                .site = plugin_api.site.functionSite(function.origin.*),
                .hint = "Select a public method or a free function that can return an error.",
            });
        }
        const enabled = entry.is_public and entry.has_error;
        try context.facts.put(allocator, plugin, .function(function.origin.*), .{
            .enabled = enabled,
            .replace = enabled and options.replace,
        });
        if (!enabled or options.replace) continue;
        const must_name = try std.fmt.allocPrint(allocator, "Must{s}", .{entry.public_name});
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
            if (!other_info.is_public or !semantic.optionalStringEqual(origin.receiver, other.origin.receiver) or !semantic.optionalStringEqual(origin.package, other.origin.package) or !std.mem.eql(u8, must_name, other_info.public_name)) continue;
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

/// Claim the declaration when the binding asked to replace rather than add.
/// The generator still writes the whole checked body, under the unexported
/// name the visit above calls. Only a function has a public surface to claim.
fn claims(context: plugin_api.GoContext, node: plugin_api.Node) !bool {
    const function = switch (node) {
        .function => |value| value,
        else => return false,
    };
    const fact = try context.facts.get(plugin, .function(function.origin.*)) orelse return false;
    return fact.replace;
}
