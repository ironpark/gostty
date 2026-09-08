//! `must`: `Must<Name>` variants that panic instead of returning an error,
//! chosen one declaration at a time.
//!
//! zigo has this built in, as `.go_must_variants`, but it is one boolean for
//! the whole package: turning it on gives every bound function a second
//! spelling, and doubling the public surface is why gostty left it off. A
//! plugin reads its options off the declaration, so `extend(must.plugin, .{})`
//! puts the variant only where failure is not a condition to branch on, and
//! nowhere else.
//!
//! The variant never spells its own signature. It renders the one the
//! generator just wrote for the checked method and takes `error` off the end,
//! so the two cannot disagree about a parameter name, a package qualifier or
//! a borrowed-view result. Everything this plugin decides for itself is which
//! of three wrappers to call, and that follows from how many results are left.
const std = @import("std");
const abi = @import("abi");
const diagnostic = @import("diagnostic");
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
    .validateAll = validateDocument,
    .method_hook = methodHook,
    .files = &.{.{ .pathAlloc = helperPath, .render = renderHelpers }},
};

fn methodHook(context: plugin_api.Context, writer: *std.Io.Writer, function: abi.AbiFn) !void {
    _ = try context.functionOptions(plugin, function.origin.*) orelse return;
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
        0 => "zigoMustSucceed(",
        1 => "return zigoMustValue(",
        else => "return zigoMustMatch(",
    });
    if (method.receiver_name) |receiver_name|
        try writer.print("{s}.{s}(", .{ receiver_name, method.go_name })
    else
        try writer.print("{s}(", .{method.go_name});
    try context.writeCallArguments(writer, function);
    try writer.writeAll(")) }\n");
}

/// The generator resolves this path relative to the active public package.
/// zigo 0.19.5 wires the naming import for this helper on both the declaration
/// and generation sides, so no local path implementation is needed.
fn helperPath(allocator: std.mem.Allocator, program: abi.Program, options: plugin_api.Options) ![]u8 {
    return plugin_api.publicFilePathAlloc(allocator, program, options, helper_file);
}

const helper_file = "zigo_must_gen.go";

/// The three wrappers the variants delegate to. They are the plugin's own
/// rather than the generator's `zigoMust`, which is written only when the
/// built-in variants or a tagged-union projection ask for it -- a plugin that
/// borrowed it would compile or not depending on an unrelated declaration.
///
/// A package with no `Must` variant writes nothing, and the frame drops a file
/// whose body came out empty.
fn renderHelpers(_: std.mem.Allocator, writer: *std.Io.Writer, program: abi.Program, options: plugin_api.Options) !void {
    if (!packageHasVariant(program, options)) return;
    try writer.writeAll(
        "// zigoMustSucceed panics with a typed error, for a Must variant of a\n" ++
            "// function whose only result is the error.\n" ++
            "func zigoMustSucceed(err error) {\n" ++
            "\tif err != nil {\n\t\tpanic(err)\n\t}\n" ++
            "}\n\n" ++
            "// zigoMustValue panics with a typed error, for a Must variant with one result.\n" ++
            "func zigoMustValue[T any](value T, err error) T {\n" ++
            "\tif err != nil {\n\t\tpanic(err)\n\t}\n" ++
            "\treturn value\n" ++
            "}\n\n" ++
            "// zigoMustMatch panics with a typed error, for a Must variant whose result\n" ++
            "// carries a presence flag beside the value.\n" ++
            "func zigoMustMatch[T any](value T, matched bool, err error) (T, bool) {\n" ++
            "\tif err != nil {\n\t\tpanic(err)\n\t}\n" ++
            "\treturn value, matched\n" ++
            "}\n",
    );
}

/// Whether the package being written has a `Must` variant in it. The helpers
/// are unexported, so each public package needs its own copy and only the
/// packages that call them may have one.
fn packageHasVariant(program: abi.Program, options: plugin_api.Options) bool {
    for (program.functions) |function| {
        const ext = function.origin.ext orelse continue;
        if (ext.get(name) == null) continue;
        if (inActivePackage(function.origin.*, options)) return true;
    }
    return false;
}

fn inActivePackage(function: semantic.SemanticFn, options: plugin_api.Options) bool {
    // A document that was never split renders one package, which holds
    // everything.
    const active = options.active_package orelse return true;
    // The empty selection is the split document's default package, whose
    // functions are the ones that named no package of their own.
    const declared = function.package orelse return active.len == 0;
    return std.mem.eql(u8, declared, active);
}

/// Both rules exist because the alternative is a compile error in generated
/// code: a variant of a function that cannot fail has nothing to panic on and
/// would not compile, and a flattened parameter is spelled as several Go
/// arguments this plugin does not know the names of. Every offending
/// declaration is reported, so one run names them all.
fn validateDocument(allocator: std.mem.Allocator, document: semantic.Semantic) ![]const diagnostic.Diagnostic {
    var issues: std.ArrayList(diagnostic.Diagnostic) = .empty;
    errdefer issues.deinit(allocator);
    for (document.functions) |function| {
        _ = try plugin_api.readOptions(plugin, .function, allocator, function.ext) orelse continue;
        // A method carries an error whatever its Zig result is, because the
        // generated Go checks the receiver's handle first. A free function
        // has no handle to check, so only an error union gives it one.
        if (function.receiver == null and function.@"return" != .error_union) try issues.append(allocator, .{
            .severity = .@"error",
            .code = name ++ "002",
            .message = try std.fmt.allocPrint(
                allocator,
                "`{s}` cannot fail, so a Must variant of it would have nothing to panic on",
                .{function.name},
            ),
            .site = .{ .path = "semantic.json", .declaration = function.name },
            .hint = "extend a method, whose receiver is handle-checked, or a free function whose Zig result is an error union",
        });
        for (function.params) |parameter| {
            if (parameter.flatten == null) continue;
            try issues.append(allocator, .{
                .severity = .@"error",
                .code = name ++ "003",
                .message = try std.fmt.allocPrint(
                    allocator,
                    "`{s}` flattens `{s}`, whose Go arguments this plugin cannot name",
                    .{ function.name, parameter.name },
                ),
                .site = .{ .path = "semantic.json", .declaration = function.name },
                .hint = "a flattened parameter becomes one Go argument per field, named by the generator; leave the Must variant off this function",
            });
        }
    }
    return issues.toOwnedSlice(allocator);
}
