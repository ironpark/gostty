//! `stringer`: `String()` for the generated value structs.
//!
//! zigo already writes `String()` for every enum, so `fmt` prints a mode or a
//! key by name. The value structs have nothing, and the ones that hurt are the
//! packed structs: `CellFlags` is twelve fields wide, so a cell's attributes
//! print as `{true false false false false false false false 1 0 true 0}` --
//! twelve bools with no names against a layout the reader has to hold in their
//! head. Writing the method by hand means keeping a file beside generated
//! code, which is what a visit of the type node is for.
//!
//! Two shapes, because the two kinds of struct read differently. A flag set
//! wants only what is set (`Bold|Underline:Single`); a coordinate or a
//! geometry wants every field (`GridPoint{X:3, Y:4}`).
const std = @import("std");
const plugin_api = @import("plugin");
const semantic = @import("semantic");

/// The plugin's name: the `ext` key its options travel under and the prefix
/// of the diagnostics it reports.
pub const name = "STRINGER";

/// What a declaration says with `use(stringer.plugin, .{ ... })`.
pub const Options = struct {
    /// How the fields are joined.
    style: Style = .flags,
    /// Zig field names left out of the rendering. Padding is the reason it
    /// exists: a packed struct names its slack so the bits add up, and that
    /// is never something to print.
    omit: []const []const u8 = &.{},

    pub const Style = enum {
        /// Only what is set, joined by `|`: `Bold|Underline:Single`. A bool
        /// field contributes its name, anything else `Name:value`, and both
        /// only when non-zero. The zero value prints as `none`.
        flags,
        /// Every field, after the type name: `GridPoint{X:3, Y:4}`.
        fields,
    };
};

pub const plugin: plugin_api.Plugin = .{
    .name = name,
    .TypeOptions = Options,
    // A `String()` needs fields to name, which only a value struct has here;
    // the generator already writes one for every enum.
    .subjects = &.{.value},
    .validate = validateDocument,
    .go = .{
        .visit = visit,
        // Written by the renderings below. The frame adds an import only to a
        // file whose body really spells the qualifier, so a package of pure bool
        // flags never grows an unused `fmt`.
        .imports = &.{
            .{ .qualifier = "fmt", .path = "fmt" },
            .{ .qualifier = "strings", .path = "strings" },
        },
    },
};

fn visit(context: plugin_api.GoContext, node: plugin_api.Node, b: *plugin_api.Builder) !void {
    if (node != .type) return;
    const declaration = node.type;
    const options = try context.optionsOf(plugin, .type, node) orelse return;
    const method = switch (options.style) {
        .flags => try renderFlags(context, b, declaration, options),
        .fields => try renderFields(context, b, declaration, options),
    };
    try b.emit(&.{ method, try assertStringer(b, declaration) }, .{ .blank_after = true });
}

/// The assertion belongs here rather than to a general interface plugin,
/// because what has to hold is narrower than "the type implements Stringer":
/// the receiver has to be the *value*, since that is the form `%v` is handed
/// and a pointer's method set would satisfy `(*T)(nil)` either way. Written
/// next to the method it is about, so the two move together.
fn assertStringer(b: *plugin_api.Builder, declaration: semantic.TypeDecl) !plugin_api.gobuild.Decl {
    const doc = try std.fmt.allocPrint(
        b.allocator,
        "{0s} satisfies fmt.Stringer as a value, which is the form `%v` is\nhanded; a pointer receiver would stop this compiling.",
        .{declaration.name},
    );
    return b.assertImplements(.{
        .doc = .{ .text = doc },
        .interface = try b.selName("fmt", "Stringer"),
        .type_name = declaration.name,
        .form = .value,
    });
}

/// `func (value T) String() string { body }`, the one header both styles share.
fn stringMethod(b: *plugin_api.Builder, declaration: semantic.TypeDecl, doc: []const u8, body: []const plugin_api.gobuild.Stmt) !plugin_api.gobuild.Decl {
    return b.func(.{
        .doc = .{ .text = doc },
        .receiver = .{ .name = "value", .type = declaration.name },
        .name = "String",
        .signature = .{ .explicit = .{ .results = &.{b.ident("string")} } },
        .body = body,
    });
}

/// `parts = append(parts, <element>)`.
fn appendPart(b: *plugin_api.Builder, element: plugin_api.gobuild.Expr) !plugin_api.gobuild.Stmt {
    return b.assign(&.{b.ident("parts")}, "=", &.{try b.callName("append", &.{ b.ident("parts"), element })});
}

/// `Bold|Underline:Single|Selected`. The test a field has to pass to appear is
/// its own zero value, which is the one thing every field here has in common:
/// a bool is `false`, an int is `0`, and an enum's zero tag is the "unset" one
/// in every packed struct ghostty declares.
fn renderFlags(
    context: plugin_api.GoContext,
    b: *plugin_api.Builder,
    declaration: semantic.TypeDecl,
    options: Options,
) !plugin_api.gobuild.Decl {
    const allocator = context.allocator;
    var body: std.ArrayList(plugin_api.gobuild.Stmt) = .empty;
    defer body.deinit(allocator);
    try body.append(allocator, b.declare("parts", try b.sliceOf(b.ident("string")), null));
    for (declaration.fields) |field| {
        if (omits(options, field.name)) continue;
        const member = try context.identifierAlloc(allocator, field.name, .pascal);
        const read = try b.sel(b.ident("value"), member);
        if (field.type.? == .bool) {
            try body.append(allocator, try b.ifStmt(.{
                .cond = read,
                .body = &.{try appendPart(b, b.string(member))},
            }));
            continue;
        }
        // An enum's Go type is an integer, so the zero comparison is the same
        // one an int gets and the `%v` picks up the enum's own `String`.
        const format = try std.fmt.allocPrint(allocator, "{s}:%v", .{member});
        try body.append(allocator, try b.ifStmt(.{
            .cond = try b.bin("!=", read, b.int(0)),
            .body = &.{try appendPart(b, try b.call(try b.selName("fmt", "Sprintf"), &.{ b.string(format), read }))},
        }));
    }
    try body.append(allocator, try b.ifStmt(.{
        .cond = try b.bin("==", try b.callName("len", &.{b.ident("parts")}), b.int(0)),
        .body = &.{try b.ret(&.{b.string("none")})},
    }));
    try body.append(allocator, try b.ret(&.{try b.call(try b.selName("strings", "Join"), &.{ b.ident("parts"), b.string("|") })}));

    const doc = try std.fmt.allocPrint(
        allocator,
        "String names the set members of {0s}, joined by \"|\". A member left at\nits zero value is not named, so the zero {0s} is \"none\".",
        .{declaration.name},
    );
    return stringMethod(b, declaration, doc, body.items);
}

/// `GridPoint{X:3, Y:4}`. Every field, named, in declaration order -- the
/// difference from Go's own `%v` on a struct being that the names are there.
fn renderFields(
    context: plugin_api.GoContext,
    b: *plugin_api.Builder,
    declaration: semantic.TypeDecl,
    options: Options,
) !plugin_api.gobuild.Decl {
    const allocator = context.allocator;
    var format: std.Io.Writer.Allocating = .init(allocator);
    defer format.deinit();
    var arguments: std.ArrayList(plugin_api.gobuild.Expr) = .empty;
    defer arguments.deinit(allocator);

    try format.writer.print("{s}{{", .{declaration.name});
    for (declaration.fields) |field| {
        if (omits(options, field.name)) continue;
        const member = try context.identifierAlloc(allocator, field.name, .pascal);
        if (arguments.items.len != 0) try format.writer.writeAll(", ");
        // A codepoint is a `rune`, and a rune printed as a number is the one
        // spelling nobody wants to read; `%q` gives it back as `'a'`.
        const verb = if (semantic.isCodepoint(field.type.?, field.semantic)) "%q" else "%v";
        try format.writer.print("{s}:{s}", .{ member, verb });
        try arguments.append(allocator, try b.sel(b.ident("value"), member));
    }
    try format.writer.writeAll("}");

    var call_args: std.ArrayList(plugin_api.gobuild.Expr) = .empty;
    defer call_args.deinit(allocator);
    try call_args.append(allocator, b.string(try allocator.dupe(u8, format.written())));
    try call_args.appendSlice(allocator, arguments.items);

    const doc = try std.fmt.allocPrint(allocator, "String renders {0s} as its type name and its fields.", .{declaration.name});
    return stringMethod(b, declaration, doc, &.{
        try b.ret(&.{try b.call(try b.selName("fmt", "Sprintf"), call_args.items)}),
    });
}

fn omits(options: Options, field_name: []const u8) bool {
    for (options.omit) |entry| {
        if (std.mem.eql(u8, entry, field_name)) return true;
    }
    return false;
}

/// The rules exist because every one of them would otherwise reach the user as
/// a compile error in generated code, or -- worse -- as a `String` that
/// silently stopped naming a field the binding renamed. Every offending
/// declaration is reported, so one run names them all.
fn validateDocument(context: plugin_api.ValidateContext) !void {
    const allocator = context.allocator;
    const document = context.document;
    for (document.types) |declaration| {
        // A declaration of the wrong kind never reaches here: `.targets` says
        // this plugin takes a value struct, so asking an enum or a handle for
        // a `String` is a Zig compile error on the line that asked.
        const options = try context.optionsOf(plugin, .type, declaration.ext) orelse continue;
        // A name that matches nothing is how this method quietly stops naming
        // a field: the binding renames it, the omission goes on matching
        // nothing, and the field appears in the output without anyone asking.
        for (options.omit) |omitted| {
            if (fieldNamed(declaration, omitted) != null) continue;
            try context.diagnose(.{
                .severity = .@"error",
                .code = name ++ "003",
                .message = try std.fmt.allocPrint(
                    allocator,
                    "`{s}` omits `{s}`, which is not one of its fields",
                    .{ declaration.name, omitted },
                ),
                .site = plugin_api.site.typeSite(declaration),
                .hint = "`omit` names Zig fields, in the Zig spelling; check the field still exists under that name",
            });
        }
        if (options.style != .flags) continue;
        for (declaration.fields) |field| {
            if (omits(options, field.name)) continue;
            if (flagRenderable(field.type.?)) continue;
            try context.diagnose(.{
                .severity = .@"error",
                .code = name ++ "004",
                .message = try std.fmt.allocPrint(
                    allocator,
                    "`{s}.{s}` has no zero value to test, so the `flags` style cannot leave it out",
                    .{ declaration.name, field.name },
                ),
                .site = plugin_api.site.typeSite(declaration),
                .hint = "the `flags` style takes bool, integer and enum fields; use `.style = .fields`, which names every field unconditionally",
            });
        }
    }
}

fn fieldNamed(declaration: semantic.TypeDecl, field_name: []const u8) ?semantic.TypeField {
    for (declaration.fields) |field| {
        if (std.mem.eql(u8, field.name, field_name)) return field;
    }
    return null;
}

/// Whether `!= 0` (or, for a bool, the value itself) is the right test for a
/// field being set. Everything else has no zero the plugin may assume.
fn flagRenderable(node: semantic.TypeNode) bool {
    return node == .bool or node == .int or node == .@"enum";
}
