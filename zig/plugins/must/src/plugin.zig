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
const naming = @import("naming");
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
    .validate = validateDocument,
    .method_hook = methodHook,
    .files = &.{.{ .pathAlloc = helperPath, .render = renderHelpers }},
};

fn methodHook(context: plugin_api.Context, writer: *std.Io.Writer, function: abi.AbiFn) !void {
    _ = try context.functionOptions(plugin, function.origin.*) orelse return;
    const method = context.method.?;

    // The checked method's own signature, as the generator spelled it a few
    // lines above this one.
    var rendered: std.Io.Writer.Allocating = .init(context.allocator);
    defer rendered.deinit();
    try context.writeSignature(&rendered.writer, function);
    const parts = splitSignature(rendered.written()) orelse return error.UnreadableSignature;
    const results = try withoutError(context.allocator, parts.result) orelse return error.UnreadableSignature;
    defer context.allocator.free(results.text);

    try writer.print(
        "\n// Must{0s} calls {0s} and panics with its typed error on failure.\n",
        .{method.go_name},
    );
    if (method.receiver) |receiver|
        try writer.print("func ({s} *{s}) Must{s}", .{ method.receiver_name.?, receiver, method.go_name })
    else
        try writer.print("func Must{s}", .{method.go_name});
    try writer.writeAll(parts.params);
    if (results.text.len != 0) try writer.print(" {s}", .{results.text});
    try writer.writeAll(" { ");
    try writer.writeAll(switch (results.count) {
        0 => "zigoMustSucceed(",
        1 => "return zigoMustValue(",
        else => "return zigoMustMatch(",
    });
    if (method.receiver_name) |receiver_name|
        try writer.print("{s}.{s}(", .{ receiver_name, method.go_name })
    else
        try writer.print("{s}(", .{method.go_name});
    try writeCallArguments(writer, function, method.param_names);
    try writer.writeAll(")) }\n");
}

const Signature = struct {
    /// The parameter list, parentheses included.
    params: []const u8,
    /// What follows it, with no leading space.
    result: []const u8,
};

/// Splits a rendered signature at the parameter list's closing parenthesis.
/// The scan counts depth rather than looking for the first `)`, because a
/// callback parameter's own type carries parentheses.
fn splitSignature(signature: []const u8) ?Signature {
    if (signature.len == 0 or signature[0] != '(') return null;
    var depth: usize = 0;
    for (signature, 0..) |character, index| {
        switch (character) {
            '(' => depth += 1,
            ')' => {
                depth -= 1;
                if (depth != 0) continue;
                const rest = std.mem.trim(u8, signature[index + 1 ..], " ");
                return .{ .params = signature[0 .. index + 1], .result = rest };
            },
            else => {},
        }
    }
    return null;
}

const Results = struct {
    /// The result list a `Must` variant is written with, empty when none is
    /// left. Owned by the caller.
    text: []u8,
    /// How many results remain, which is what picks the wrapper.
    count: usize,
};

/// The checked result list with its trailing `error` removed: `error` becomes
/// nothing, `(T, error)` becomes `T`, and `(T, bool, error)` becomes
/// `(T, bool)`. Null when the result does not end in an error, which
/// `validate` has already ruled out.
fn withoutError(allocator: std.mem.Allocator, result: []const u8) !?Results {
    if (std.mem.eql(u8, result, "error")) return .{ .text = try allocator.alloc(u8, 0), .count = 0 };
    if (result.len < 2 or result[0] != '(' or result[result.len - 1] != ')') return null;

    var remaining: std.ArrayList([]const u8) = .empty;
    defer remaining.deinit(allocator);
    var items = topLevelItems(result[1 .. result.len - 1]);
    while (try items.next()) |item| try remaining.append(allocator, item);
    if (remaining.items.len < 2) return null;
    if (!std.mem.eql(u8, remaining.items[remaining.items.len - 1], "error")) return null;
    const kept = remaining.items[0 .. remaining.items.len - 1];

    if (kept.len == 1) return .{ .text = try allocator.dupe(u8, kept[0]), .count = 1 };
    var joined: std.Io.Writer.Allocating = .init(allocator);
    errdefer joined.deinit();
    try joined.writer.writeByte('(');
    for (kept, 0..) |item, index| {
        if (index != 0) try joined.writer.writeAll(", ");
        try joined.writer.writeAll(item);
    }
    try joined.writer.writeByte(')');
    return .{ .text = try joined.toOwnedSlice(), .count = kept.len };
}

/// Splits a result list on its top-level commas. A `func(a, b int) bool`
/// result has commas of its own, so depth decides which ones separate items.
const topLevelItems = struct {
    fn init(list: []const u8) Iterator {
        return .{ .list = list };
    }
    const Iterator = struct {
        list: []const u8,
        index: usize = 0,

        fn next(self: *Iterator) !?[]const u8 {
            if (self.index >= self.list.len) return null;
            var depth: usize = 0;
            const start = self.index;
            while (self.index < self.list.len) : (self.index += 1) {
                switch (self.list[self.index]) {
                    '(', '[' => depth += 1,
                    ')', ']' => depth -= 1,
                    ',' => if (depth == 0) {
                        const item = self.list[start..self.index];
                        self.index += 1;
                        return std.mem.trim(u8, item, " ");
                    },
                    else => {},
                }
            }
            return std.mem.trim(u8, self.list[start..self.index], " ");
        }
    };
}.init;

/// The arguments the variant forwards, in the order the checked method takes
/// them. The parameters the generator does not spell in Go -- an injected
/// allocator, a callback's userdata token, the cancellation flag -- are the
/// ones it skips, and the names come from the method itself.
fn writeCallArguments(writer: *std.Io.Writer, function: abi.AbiFn, go_names: [][]u8) !void {
    var written: usize = 0;
    if (function.origin.cancel != null) {
        try writer.writeAll("ctx");
        written = 1;
    }
    for (function.origin.params, 0..) |parameter, index| {
        if (function.userdataFor(index) != null) continue;
        if (parameter.injected != null or parameter.type == .cancel_flag) continue;
        if (written != 0) try writer.writeAll(", ");
        try writer.writeAll(go_names[index]);
        written += 1;
    }
}

/// `<package dir>/zigo_must_gen.go`. The path a `files` emitter returns is
/// relative to the module root, not to the package being written, so a bare
/// file name would put every package's helpers on top of each other at the
/// root. The directory is derived the way the built-in files derive theirs:
/// the active package's path, with `.` meaning the module root itself.
fn helperPath(allocator: std.mem.Allocator, program: abi.Program, options: plugin_api.Options) ![]u8 {
    const directory = if (options.go_package_path.len != 0)
        try allocator.dupe(u8, options.go_package_path)
    else if (options.go_package.len != 0)
        try allocator.dupe(u8, options.go_package)
    else
        try naming.snakeAlloc(allocator, program.package);
    defer allocator.free(directory);
    if (std.mem.eql(u8, directory, ".")) return allocator.dupe(u8, helper_file);
    return std.fmt.allocPrint(allocator, "{s}/{s}", .{ directory, helper_file });
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
/// arguments this plugin does not know the names of.
fn validateDocument(allocator: std.mem.Allocator, document: semantic.Semantic) !?diagnostic.Diagnostic {
    for (document.functions) |function| {
        _ = try plugin_api.readOptions(plugin, .function, allocator, function.ext) orelse continue;
        // A method carries an error whatever its Zig result is, because the
        // generated Go checks the receiver's handle first. A free function
        // has no handle to check, so only an error union gives it one.
        if (function.receiver == null and function.@"return" != .error_union) return .{
            .severity = .@"error",
            .code = name ++ "002",
            .message = try std.fmt.allocPrint(
                allocator,
                "`{s}` cannot fail, so a Must variant of it would have nothing to panic on",
                .{function.name},
            ),
            .site = .{ .path = "semantic.json", .declaration = function.name },
            .hint = "extend a method, whose receiver is handle-checked, or a free function whose Zig result is an error union",
        };
        for (function.params) |parameter| {
            if (parameter.flatten == null) continue;
            return .{
                .severity = .@"error",
                .code = name ++ "003",
                .message = try std.fmt.allocPrint(
                    allocator,
                    "`{s}` flattens `{s}`, whose Go arguments this plugin cannot name",
                    .{ function.name, parameter.name },
                ),
                .site = .{ .path = "semantic.json", .declaration = function.name },
                .hint = "a flattened parameter becomes one Go argument per field, named by the generator; leave the Must variant off this function",
            };
        }
    }
    return null;
}
