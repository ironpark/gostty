//! Shared declaration policies. Native ownership stays explicit at call sites.
pub const zigo = @import("zigo");

pub const gostty = @import("gostty");

// Generator plugins, wired in `build.zig`. They add to the generated Go and
// nothing else, so what they are asked for here cannot move the ABI. Their
// option types and their legal targets are both checked at the declaration:
// asking `stringer` for an enum is a Zig compile error on the line that asked.
pub const build_info = @import("build_info");
pub const convenience = @import("convenience");

pub const enumkit = @import("enumkit");

pub const must = @import("must");

pub const satisfies = @import("satisfies");

pub const stringer = @import("stringer");

pub const trim = @import("trim");

/// Every declaration below is selected through this scope, so a path is a real
/// reference rather than a string: renaming a Zig function breaks the build
/// here instead of producing a Go API missing a method.
pub const api = zigo.scope(gostty);

/// A value struct that prints as its set members, `Bold|Underline:Single`.
/// For the packed structs: a flag set is read one name at a time, and the
/// backing integer's slack (`_pad`) is not one of the names. `stringer` writes
/// the `fmt.Stringer` assertion beside the method, in the value form `%v`
/// actually uses.
pub fn flags(comptime name: []const u8, comptime pad: []const u8) zigo.Entry {
    return api.val(name, .{}).use(stringer.plugin, .{ .style = .flags, .omit = &.{pad} });
}

/// A value struct that prints as all of its fields, `GridPoint{X:3, Y:4}`.
/// For the coordinate-like ones, where every field is part of the answer and
/// Go's own `%v` would give the numbers with no names on them.
pub fn printed(comptime name: []const u8) zigo.Entry {
    return api.val(name, .{}).use(stringer.plugin, .{ .style = .fields });
}

/// Adds `Must<Name>`, which panics with the error the checked call would have
/// returned. It is asked for one declaration at a time rather than switched on
/// for the package, because it is a policy about how a caller wants to spend
/// an error, not a claim that the call cannot fail: the generated wrapper can
/// still report a closed handle, a parent already gone, or a handle poisoned
/// by an earlier native panic. These are the calls where none of that is a
/// condition to branch on, so a panic is the honest response.
pub fn withMust(comptime entry: zigo.Entry) zigo.Entry {
    return entry.use(must.plugin, .{});
}

/// The same policy for a field accessor, and taken further: `.replace` gives
/// the panicking form the plain name and does not export the checked one, so
/// `Cols()` returns `uint16` rather than `(uint16, error)`.
///
/// A field has no body to fail in. The only error is a dead or poisoned
/// handle, which a caller holding that handle cannot act on and would only
/// propagate, so `if err != nil` on every read is noise around a defect. Go
/// answers the same way where the shape is the same -- `reflect.Value.Int`
/// panics on misuse rather than offering a checked twin. Keeping both names
/// would have doubled the surface to no end: that is the reason the blanket
/// `Must*` was turned down in 0.8.0, and it applies to `MustCols` beside
/// `Cols` just as well.
///
/// This is for field reads only. The calls that *do* something keep their
/// `error`, even the ones that cannot fail in Zig, because there a returned
/// error is the ordinary Go shape.
pub fn mustField(comptime field: zigo.HandleField) zigo.HandleField {
    return field.extend(must.plugin, .{ .replace = true });
}

/// A type whose methods drop the prefix naming the type. ghostty declares them
/// flat -- `searchTick`, `screenSelectAll` -- because at the root the prefix is
/// the only thing saying what they belong to; in Go the receiver says it.
pub fn trimmed(comptime entry: zigo.Entry) zigo.Entry {
    return entry.use(trim.plugin, .{});
}

/// The same, for the types whose declarations are not prefixed with the type's
/// own name. Spelling the prefix is what says so.
pub fn trimmedAs(comptime entry: zigo.Entry, comptime prefix: []const u8) zigo.Entry {
    return entry.use(trim.plugin, .{ .prefix = prefix });
}

/// One Go enum.
///
/// `.text` asks for `Parse<Enum>`, `MarshalText` and `UnmarshalText`, so the
/// enum can live in a config file or a flag; it is on for the enums a consumer
/// is likely to name in text. `.open` carries through an enum ghostty leaves
/// non-exhaustive so that a number arriving from a pty never fails
/// `@enumFromInt`. `.kit` adds `<Enum>Values()` and `IsKnown()`: on for the
/// two long enough to want listing, and for every open one, where `IsKnown` is
/// the only way to tell a tag from a number the pty made up. A false there
/// means "not a name this binding knows", not "not a value ghostty accepts".
/// `.docs` writes the Go doc of individual members. Most of these enums are
/// ghostty's, re-exported by alias, so there is no line in this repository to
/// hang a `///` on; the binding is the only place the sentence can live. A
/// member left out of the list still takes ghostty's `///` when it has one.
pub fn enumeration(comptime name: []const u8, comptime opts: struct {
    text: bool = false,
    open: bool = false,
    kit: bool = false,
    docs: []const zigo.EnumField = &.{},
}) zigo.Entry {
    const declared = api.enumType(name, .{ .exhaustive = !opts.open, .fields = opts.docs });
    const texted = if (opts.text) declared.use(zigo.features.text, .{}) else declared;
    return if (opts.open or opts.kit) texted.use(enumkit.plugin, .{}) else texted;
}

/// One Go callback type: retained by whatever took it, free to re-enter the
/// bindings for request replies, and run on the thread that called in.
/// Transport reentrancy does not permit recursive Feed or terminal mutation.
/// Who holds the pointer and
/// what releases it differs per callback and is documented where each one is
/// declared.
pub fn callback(comptime decl: []const u8, comptime go_name: []const u8) zigo.Entry {
    return api.callback(decl, .{
        .retention = .retained,
        .reentrancy = .allowed,
        .thread = .caller,
    }).named(go_name);
}

/// A caller-provided buffer the function fills and reports the length of.
/// The index is the original Zig one, including receiver and injected arguments.
pub fn out(comptime index: usize) zigo.Param {
    return zigo.param.output(index, .result);
}

/// A caller-provided codepoint buffer: the grapheme reads fill one and report
/// how much of it they used.
pub fn outCodepoints(comptime index: usize) zigo.Param {
    var slot = zigo.param.output(index, .result);
    slot.semantic = .codepoint;
    return slot;
}

/// A parameter that is bytes rather than text, which inference cannot tell
/// apart on its own. Index is zero-based in the native Zig signature, including
/// the receiver and injected allocator/Io parameters; it is not a Go argument index.
pub fn bytesArg(comptime index: usize) zigo.Param {
    return .{ .index = index, .semantic = .opaque_bytes };
}

/// A constructor returning a child whose lifetime depends on its receiver.
/// Kept distinct from ordinary constructors so parent ownership is visible.
pub fn childConstructor(comptime name: []const u8, comptime child: anytype) zigo.Entry {
    return api.func(name, .{ .role = .{ .constructor = .{
        .type = child.typeRef(),
        .receiver = .member,
        .parent = .receiver,
    } } });
}
