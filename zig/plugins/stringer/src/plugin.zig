//! `stringer`: `String()` for the generated value structs.
//!
//! zigo already writes `String()` for every enum, so `fmt` prints a mode or a
//! key by name. The value structs have nothing, and the ones that hurt are the
//! packed structs: `CellFlags` is twelve fields wide, so a cell's attributes
//! print as `{true false false false false false false false 1 0 true 0}` --
//! twelve bools with no names against a layout the reader has to hold in their
//! head. Writing the method by hand means keeping a file beside generated
//! code, which is what a `type_hook` is for.
//!
//! Two shapes, because the two kinds of struct read differently. A flag set
//! wants only what is set (`Bold|Underline:Single`); a coordinate or a
//! geometry wants every field (`GridPoint{X:3, Y:4}`).
const std = @import("std");
const diagnostic = @import("diagnostic");
const naming = @import("naming");
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
    .targets = &.{.value},
    .validate = validateDocument,
    .type_hook = typeHook,
    // Written by the renderings below. The frame adds an import only to a
    // file whose body really spells the qualifier, so a package of pure bool
    // flags never grows an unused `fmt`.
    .imports = &.{
        .{ .qualifier = "fmt", .path = "fmt" },
        .{ .qualifier = "strings", .path = "strings" },
    },
};

fn typeHook(context: plugin_api.Context, writer: *std.Io.Writer, declaration: semantic.TypeDecl) !void {
    const options = try context.typeOptions(plugin, declaration) orelse return;
    switch (options.style) {
        .flags => try renderFlags(context, writer, declaration, options),
        .fields => try renderFields(context, writer, declaration, options),
    }
    try assertStringer(writer, declaration);
}

/// The assertion belongs here rather than to a general interface plugin,
/// because what has to hold is narrower than "the type implements Stringer":
/// the receiver has to be the *value*, since that is the form `%v` is handed
/// and a pointer's method set would satisfy `(*T)(nil)` either way. Written
/// next to the method it is about, so the two move together.
fn assertStringer(writer: *std.Io.Writer, declaration: semantic.TypeDecl) !void {
    try writer.print(
        "// {0s} satisfies fmt.Stringer as a value, which is the form `%v` is\n" ++
            "// handed; a pointer receiver would stop this compiling.\n" ++
            "var _ fmt.Stringer = {0s}{{}}\n\n",
        .{declaration.name},
    );
}

/// `Bold|Underline:Single|Selected`. The test a field has to pass to appear is
/// its own zero value, which is the one thing every field here has in common:
/// a bool is `false`, an int is `0`, and an enum's zero tag is the "unset" one
/// in every packed struct ghostty declares.
fn renderFlags(
    context: plugin_api.Context,
    writer: *std.Io.Writer,
    declaration: semantic.TypeDecl,
    options: Options,
) !void {
    try writer.print(
        "// String names the set members of {0s}, joined by \"|\". A member left at\n" ++
            "// its zero value is not named, so the zero {0s} is \"none\".\n" ++
            "func (value {0s}) String() string {{\n" ++
            "\tvar parts []string\n",
        .{declaration.name},
    );
    for (declaration.fields) |field| {
        if (omits(options, field.name)) continue;
        const member = try naming.pascalAlloc(context.allocator, field.name);
        defer context.allocator.free(member);
        if (field.type.? == .bool) {
            try writer.print(
                "\tif value.{0s} {{\n\t\tparts = append(parts, \"{0s}\")\n\t}}\n",
                .{member},
            );
            continue;
        }
        // An enum's Go type is an integer, so the zero comparison is the same
        // one an int gets and the `%v` picks up the enum's own `String`.
        try writer.print(
            "\tif value.{0s} != 0 {{\n\t\tparts = append(parts, fmt.Sprintf(\"{0s}:%v\", value.{0s}))\n\t}}\n",
            .{member},
        );
    }
    try writer.writeAll(
        "\tif len(parts) == 0 {\n\t\treturn \"none\"\n\t}\n" ++
            "\treturn strings.Join(parts, \"|\")\n}\n\n",
    );
}

/// `GridPoint{X:3, Y:4}`. Every field, named, in declaration order -- the
/// difference from Go's own `%v` on a struct being that the names are there.
fn renderFields(
    context: plugin_api.Context,
    writer: *std.Io.Writer,
    declaration: semantic.TypeDecl,
    options: Options,
) !void {
    var format: std.Io.Writer.Allocating = .init(context.allocator);
    defer format.deinit();
    var arguments: std.Io.Writer.Allocating = .init(context.allocator);
    defer arguments.deinit();

    try format.writer.print("{s}{{", .{declaration.name});
    var written: usize = 0;
    for (declaration.fields) |field| {
        if (omits(options, field.name)) continue;
        const member = try naming.pascalAlloc(context.allocator, field.name);
        defer context.allocator.free(member);
        if (written != 0) try format.writer.writeAll(", ");
        // A codepoint is a `rune`, and a rune printed as a number is the one
        // spelling nobody wants to read; `%q` gives it back as `'a'`.
        const verb = if (semantic.isCodepoint(field.type.?, field.semantic)) "%q" else "%v";
        try format.writer.print("{s}:{s}", .{ member, verb });
        try arguments.writer.print(", value.{s}", .{member});
        written += 1;
    }
    try format.writer.writeAll("}");

    try writer.print(
        "// String renders {0s} as its type name and its fields.\n" ++
            "func (value {0s}) String() string {{\n" ++
            "\treturn fmt.Sprintf(\"{1s}\"{2s})\n}}\n\n",
        .{ declaration.name, format.written(), arguments.written() },
    );
}

fn omits(options: Options, field_name: []const u8) bool {
    for (options.omit) |entry| {
        if (std.mem.eql(u8, entry, field_name)) return true;
    }
    return false;
}

/// The rules exist because every one of them would otherwise reach the user as
/// a compile error in generated code, or -- worse -- as a `String` that
/// silently stopped naming a field the binding renamed.
fn validateDocument(allocator: std.mem.Allocator, document: semantic.Semantic) !?diagnostic.Diagnostic {
    for (document.types) |declaration| {
        // A declaration of the wrong kind never reaches here: `.targets` says
        // this plugin takes a value struct, so asking an enum or a handle for
        // a `String` is a Zig compile error on the line that asked.
        const options = try plugin_api.readOptions(plugin, .type, allocator, declaration.ext) orelse continue;
        // A name that matches nothing is how this method quietly stops naming
        // a field: the binding renames it, the omission goes on matching
        // nothing, and the field appears in the output without anyone asking.
        for (options.omit) |omitted| {
            if (fieldNamed(declaration, omitted) != null) continue;
            return .{
                .severity = .@"error",
                .code = name ++ "003",
                .message = try std.fmt.allocPrint(
                    allocator,
                    "`{s}` omits `{s}`, which is not one of its fields",
                    .{ declaration.name, omitted },
                ),
                .site = .{ .path = "semantic.json", .declaration = declaration.name },
                .hint = "`omit` names Zig fields, in the Zig spelling; check the field still exists under that name",
            };
        }
        if (options.style != .flags) continue;
        for (declaration.fields) |field| {
            if (omits(options, field.name)) continue;
            if (flagRenderable(field.type.?)) continue;
            return .{
                .severity = .@"error",
                .code = name ++ "004",
                .message = try std.fmt.allocPrint(
                    allocator,
                    "`{s}.{s}` has no zero value to test, so the `flags` style cannot leave it out",
                    .{ declaration.name, field.name },
                ),
                .site = .{ .path = "semantic.json", .declaration = declaration.name },
                .hint = "the `flags` style takes bool, integer and enum fields; use `.style = .fields`, which names every field unconditionally",
            };
        }
    }
    return null;
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
