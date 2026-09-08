//! `trim`: a type's methods drop the prefix that names the type.
//!
//! ghostty's declarations are flat. A search's methods are root functions
//! called `searchTick`, `searchFeed`, `searchSelectedIndex`, because at the
//! root that prefix is the only thing saying which type they belong to. In Go
//! the receiver says it, so `Search.SearchTick()` stutters and every one of
//! these needed a `.name` beside it -- sixty-seven of them, each a second
//! spelling of a name the Zig side had already decided, and each free to drift
//! the day the Zig declaration was renamed.
//!
//! The type asks once and every method under it loses the prefix. It is asked
//! for per type rather than switched on for the package, because it is a claim
//! about how one type's declarations are spelled, and the two types whose
//! prefix is not their own name (`RenderState` writes `render`, `KittyImages`
//! writes `kitty`) say so where they are declared.
//!
//! This is a `transform` rather than the `name_function` hook that renames Go
//! alone, because the prefix is not in the C ABI either: `searchTick` is
//! `zg_search_tick`, not `zg_search_search_tick`. Renaming the declaration and
//! pointing `zig_path` back at the Zig function keeps the symbol, the Go name
//! and the raw binding all spelled the one way.
const std = @import("std");
const naming = @import("naming");
const plugin_api = @import("plugin");
const semantic = @import("semantic");

pub const name = "TRIM";

/// What a declaration says with `.use(trim.plugin, .{ ... })`.
pub const Options = struct {
    /// The prefix to drop, in the lowerCamel spelling the Zig declarations
    /// use. Absent means the type's own name in that spelling, which is the
    /// case for every type here but two.
    prefix: ?[]const u8 = null,
};

pub const plugin: plugin_api.Plugin = .{
    .name = name,
    .min_contract = .{ .major = 2, .minor = 0 },
    .TypeOptions = Options,
    .targets = &.{.handle},
    .transform = transform,
};

/// The prefix a type asks for: what it wrote, or its own name with the first
/// letter lowered.
fn prefixAlloc(allocator: std.mem.Allocator, declaration: semantic.TypeDecl, options: Options) ![]const u8 {
    if (options.prefix) |explicit| return explicit;
    const owned = try allocator.dupe(u8, declaration.name);
    owned[0] = std.ascii.toLower(owned[0]);
    return owned;
}

/// A prefix only ends where the next word begins, so the remainder has to
/// start with a capital: `Screen`'s `screenSelectAll` gives `selectAll`, and a
/// hypothetical `screenshot` gives nothing rather than `hot`.
fn stripAlloc(allocator: std.mem.Allocator, zig_name: []const u8, prefix: []const u8) !?[]const u8 {
    if (prefix.len == 0 or zig_name.len <= prefix.len) return null;
    if (!std.mem.startsWith(u8, zig_name, prefix)) return null;
    const rest = zig_name[prefix.len..];
    if (!std.ascii.isUpper(rest[0])) return null;
    const owned = try allocator.dupe(u8, rest);
    owned[0] = std.ascii.toLower(owned[0]);
    return owned;
}

/// The prefix a type asks for, or null when it asked for nothing. The lookup
/// is by the name the function's receiver carries, so `snapshotDecoderNext`
/// answers to `SnapshotDecoder` and never to `Snapshot`.
fn prefixFor(
    context: plugin_api.TransformContext,
    function: semantic.SemanticFn,
) !?struct { prefix: []const u8, index: usize } {
    const owner = function.receiver orelse function.goOwner() orelse return null;
    for (context.document.types, 0..) |declaration, index| {
        if (!std.mem.eql(u8, declaration.name, owner)) continue;
        const options = try context.optionsOf(plugin, .type, declaration.ext) orelse return null;
        return .{ .prefix = try prefixAlloc(context.allocator, declaration, options), .index = index };
    }
    return null;
}

/// A constructor and a destructor are matched by their Zig name, and they are
/// named by the protocol in Go anyway -- `Close`, `New<Type>` -- so the prefix
/// is not theirs to drop.
fn isLifecycle(document: semantic.Semantic, function: semantic.SemanticFn) bool {
    for (document.constructors) |pair| {
        if (std.mem.eql(u8, pair.init, function.name)) return true;
        if (std.mem.eql(u8, pair.deinit, function.name)) return true;
    }
    return false;
}

/// The symbol was derived from the old name, so it ends in that name's snake
/// spelling; the new one replaces exactly that tail. Rebuilding it from the
/// owner instead would have to guess which owner the reflector used, and this
/// cannot disagree with it.
fn symbolAlloc(
    allocator: std.mem.Allocator,
    function: semantic.SemanticFn,
    trimmed: []const u8,
) !?[]const u8 {
    if (function.custom_symbol orelse false) return null;
    const old_tail = try naming.snakeAlloc(allocator, function.name);
    defer allocator.free(old_tail);
    if (!std.mem.endsWith(u8, function.symbol, old_tail)) return null;
    const new_tail = try naming.snakeAlloc(allocator, trimmed);
    defer allocator.free(new_tail);
    return try std.mem.concat(allocator, u8, &.{ function.symbol[0 .. function.symbol.len - old_tail.len], new_tail });
}

/// Renaming the declaration moves the Zig function out from under the name, so
/// `zig_path` has to say where it went before the name changes.
///
/// The typo check belongs here rather than in `validate`, which only ever sees
/// the document this returns: by then a prefix that did nothing and a prefix
/// that did its job look the same.
fn transform(context: plugin_api.TransformContext) !semantic.Semantic {
    const allocator = context.allocator;
    var document = context.document;
    var used = try allocator.alloc(bool, document.types.len);
    @memset(used, false);
    const functions = try allocator.dupe(semantic.SemanticFn, document.functions);
    for (functions) |*function| {
        if (function.go_name != null) continue;
        if (isLifecycle(document, function.*)) continue;
        const found = try prefixFor(context, function.*) orelse continue;
        const trimmed = try stripAlloc(allocator, function.name, found.prefix) orelse continue;
        used[found.index] = true;
        const zig_path = try semantic.zigCallPathAlloc(allocator, function.*);
        if (try symbolAlloc(allocator, function.*, trimmed)) |symbol| function.symbol = symbol;
        function.zig_path = zig_path;
        function.name = trimmed;
    }
    document.functions = functions;

    // A prefix that strips nothing is a typo, and a silent one: the methods
    // keep their stuttering names and the binding looks like it asked for that.
    for (document.types, used) |declaration, trimmed_any| {
        if (trimmed_any) continue;
        const options = try context.optionsOf(plugin, .type, declaration.ext) orelse continue;
        const prefix = try prefixAlloc(allocator, declaration, options);
        try context.diagnose(.{
            .severity = .@"error",
            .code = name ++ "002",
            .message = try std.fmt.allocPrint(allocator, "`{s}` trims `{s}`, which no method of it starts with", .{ declaration.name, prefix }),
            .site = .{ .path = "semantic.json", .declaration = declaration.name },
            .hint = "write the prefix as the Zig declarations spell it, or drop the plugin from this type",
        });
    }
    return document;
}
