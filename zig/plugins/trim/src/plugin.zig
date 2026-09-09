//! `trim`: a type's methods drop the prefix that names the type.
//!
//! ghostty's declarations are flat. A search's methods are root functions
//! called `searchTick`, `searchFeed`, `searchSelectedIndex`, because at the
//! root that prefix is the only thing saying which type they belong to. In Go
//! the receiver says it, so `Search.SearchTick()` stutters and every one of
//! these needed a `.name` beside it -- sixty-eight of them, each a second
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
//! `zg_search_tick`, not `zg_search_search_tick`. Renaming the declaration
//! leaves the symbol where it was, because `semantic.functionSymbolAlloc`
//! derives it from the owner and the final name; `zig_path` is what keeps the
//! shim pointed at the Zig function the name moved out from under.
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
    // The rule is about how a group of declarations is spelled, which is not
    // a property of the kind of type they hang off: `ColorName` is an enum
    // whose one wrapper is `colorNameDefault`.
    .targets = &.{ .handle, .enumeration, .value },
    .transform = transform,
};

/// The prefix a type asks for: what it wrote, or its own name in the lowerCamel
/// spelling the rest of zigo's naming pipeline uses.
fn prefixOf(allocator: std.mem.Allocator, declaration: semantic.TypeDecl, options: Options) ![]const u8 {
    return options.prefix orelse try naming.camelAlloc(allocator, declaration.name);
}

/// A prefix only ends where the next word begins, so the remainder has to
/// start with a capital: `Screen`'s `screenSelectAll` gives `selectAll`, and a
/// hypothetical `screenshot` gives nothing rather than `hot`.
fn stripAlloc(allocator: std.mem.Allocator, zig_name: []const u8, prefix: []const u8) !?[]const u8 {
    if (prefix.len == 0 or zig_name.len <= prefix.len) return null;
    if (!std.mem.startsWith(u8, zig_name, prefix)) return null;
    if (!std.ascii.isUpper(zig_name[prefix.len])) return null;
    return try naming.camelAlloc(allocator, zig_name[prefix.len..]);
}

/// A constructor and a destructor are named by the protocol in Go -- `Close`,
/// `New<Type>` -- so the prefix is not theirs to drop.
fn isLifecycle(document: semantic.Semantic, function: semantic.SemanticFn) bool {
    if (semantic.constructorForInit(document.constructors, function) != null) return true;
    for (document.constructors) |pair| {
        if (std.mem.eql(u8, pair.deinit, function.name)) return true;
    }
    return false;
}

/// The type a function is grouped under in Go, which is the one whose prefix it
/// carries. A declaration beside the type rather than inside it has no
/// receiver, so `goOwner` answers for it.
fn ownerOf(function: semantic.SemanticFn) ?[]const u8 {
    return function.receiver orelse function.goOwner();
}

/// One pass per type that asked, so the prefix is resolved once and a type that
/// trimmed nothing is known by the time the loop over its methods ends.
///
/// Renaming the declaration moves the Zig function out from under the name, so
/// `zig_path` has to say where it went before the name changes. The typo check
/// belongs here rather than in `validate`, which only ever sees the document
/// this returns: by then a prefix that did nothing and a prefix that did its
/// job look the same.
fn transform(context: plugin_api.TransformContext) !semantic.Semantic {
    const allocator = context.allocator;
    var document = context.document;
    const functions = try allocator.dupe(semantic.SemanticFn, document.functions);
    document.functions = functions;
    for (document.types) |declaration| {
        const options = try context.optionsOf(plugin, .type, declaration.ext) orelse continue;
        const prefix = try prefixOf(allocator, declaration, options);
        var trimmed_any = false;
        for (functions) |*function| {
            if (function.go_name != null) continue;
            if (!std.mem.eql(u8, ownerOf(function.*) orelse continue, declaration.name)) continue;
            if (isLifecycle(document, function.*)) continue;
            const trimmed = try stripAlloc(allocator, function.name, prefix) orelse continue;
            function.zig_path = try semantic.zigCallPathAlloc(allocator, function.*);
            function.name = trimmed;
            trimmed_any = true;
        }
        // A prefix that strips nothing is a typo, and a silent one: the methods
        // keep their stuttering names and the binding looks like it asked for
        // that.
        if (!trimmed_any) try context.diagnose(.{
            .severity = .@"error",
            .code = name ++ "002",
            .message = try std.fmt.allocPrint(allocator, "`{s}` trims `{s}`, which no method of it starts with", .{ declaration.name, prefix }),
            .site = .{ .path = "semantic.json", .declaration = declaration.name },
            .hint = "write the prefix as the Zig declarations spell it, or drop the plugin from this type",
        });
    }
    return document;
}
