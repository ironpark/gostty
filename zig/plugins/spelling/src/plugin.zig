//! `spelling`: the Go spelling of names and counts that the Zig side decided
//! for Zig's own reasons.
//!
//! Two rules, each about one thing a Go reader would trip on:
//!
//! - A parameter named `count_req` or `needle_unowned` is a Zig name with a Zig
//!   reason on it: the `Req` says the value is a request the terminal clamps,
//!   the `Unowned` says who frees the memory. Neither is something a Go
//!   caller does anything with, and both land in godoc and in every IDE hint.
//!   The suffix comes off.
//! - Go writes initialisms in one case: `URI`, `GL`, `ASCII`, `PNG`. zigo's
//!   own table has `ID`, `URL` and `UTF8`; the rest of what this library uses
//!   is added here, as an exact name override on functions and types, so
//!   `HyperlinkURI` and `HyperlinkID` read as a pair.
//!
//! Neither moves the ABI: a parameter or function name is not part of the C
//! signature.
//!
//! What is deliberately not here is `usize` -> `int`. Counts and indices stay
//! `uint` because the type then refuses a negative at compile time, where
//! `int` would wrap it into a huge value on the way to the native side.
const std = @import("std");
const plugin_api = @import("plugin");
const semantic = @import("semantic");

pub const name = "SPELLING";

pub const plugin: plugin_api.Plugin = .{
    .name = name,
    .transform = transform,
    .name_type = nameType,
    .name_function = nameFunction,
};

// -- parameter suffixes ----------------------------------------------------

/// The Zig spellings; the Go name is derived from what is left.
const suffixes = [_][]const u8{ "_req", "_unowned" };

/// `count_req` -> `count`; a name that is only the suffix stays as it is.
fn strippedAlloc(allocator: std.mem.Allocator, zig_name: []const u8) !?[]const u8 {
    for (suffixes) |suffix| {
        if (zig_name.len <= suffix.len or !std.mem.endsWith(u8, zig_name, suffix)) continue;
        return try allocator.dupe(u8, zig_name[0 .. zig_name.len - suffix.len]);
    }
    return null;
}

fn transform(context: plugin_api.TransformContext) !semantic.Semantic {
    const allocator = context.allocator;
    var document = context.document;
    const functions = try allocator.dupe(semantic.SemanticFn, document.functions);
    document.functions = functions;
    for (functions) |*function| {
        var renamed = false;
        const params = try allocator.dupe(semantic.Parameter, function.params);
        for (params) |*parameter| {
            const stripped = try strippedAlloc(allocator, parameter.name) orelse continue;
            parameter.name = stripped;
            renamed = true;
        }
        if (renamed) function.params = params;
    }
    return document;
}

// -- initialisms -----------------------------------------------------------

/// A word as zigo's title-casing spells it, and the spelling Go wants.
const Initialism = struct { titled: []const u8, canonical: []const u8 };
const initialisms = [_]Initialism{
    .{ .titled = "Uri", .canonical = "URI" },
    .{ .titled = "Gl", .canonical = "GL" },
    .{ .titled = "Gr", .canonical = "GR" },
    .{ .titled = "Ascii", .canonical = "ASCII" },
    .{ .titled = "Png", .canonical = "PNG" },
    .{ .titled = "W3c", .canonical = "W3C" },
};

/// Whether the word at `at` ends where a new word begins: the end of the
/// name, an upper-case letter or a digit. `Gr` in `Graphemes` is not a word.
fn wordEndsAt(text: []const u8, at: usize) bool {
    return at == text.len or std.ascii.isUpper(text[at]) or std.ascii.isDigit(text[at]);
}

/// The name with every titled initialism in canonical case, or null when it
/// is already right.
fn respelledAlloc(allocator: std.mem.Allocator, public_name: []const u8) !?[]u8 {
    var out: std.ArrayList(u8) = .empty;
    defer out.deinit(allocator);
    var changed = false;
    var i: usize = 0;
    scan: while (i < public_name.len) {
        for (initialisms) |entry| {
            if (!std.mem.startsWith(u8, public_name[i..], entry.titled)) continue;
            if (!wordEndsAt(public_name, i + entry.titled.len)) continue;
            try out.appendSlice(allocator, entry.canonical);
            i += entry.titled.len;
            changed = true;
            continue :scan;
        }
        try out.append(allocator, public_name[i]);
        i += 1;
    }
    return if (changed) try out.toOwnedSlice(allocator) else null;
}

fn nameType(context: plugin_api.TransformContext, declaration: semantic.TypeDecl) !?[]const u8 {
    return respelledAlloc(context.allocator, declaration.name);
}

fn nameFunction(context: plugin_api.TransformContext, function: semantic.SemanticFn) !?[]const u8 {
    const current = try context.target.publicFunctionNameAlloc(context.allocator, context.document, function);
    return respelledAlloc(context.allocator, current);
}

test "initialisms are respelled only as whole words" {
    const allocator = std.testing.allocator;
    const cases = [_]struct { in: []const u8, out: ?[]const u8 }{
        .{ .in = "HyperlinkUri", .out = "HyperlinkURI" },
        .{ .in = "CharsetGl", .out = "CharsetGL" },
        .{ .in = "KeyFromAscii", .out = "KeyFromASCII" },
        .{ .in = "OnPngDecodeRequest", .out = "OnPNGDecodeRequest" },
        .{ .in = "Graphemes", .out = null },
        .{ .in = "Granted", .out = null },
        .{ .in = "GridRefs", .out = null },
        .{ .in = "HyperlinkID", .out = null },
    };
    for (cases) |case| {
        const got = try respelledAlloc(allocator, case.in);
        defer if (got) |text| allocator.free(text);
        if (case.out) |want| try std.testing.expectEqualStrings(want, got.?) else try std.testing.expect(got == null);
    }
}

test "parameter suffixes come off but a bare suffix stays" {
    const allocator = std.testing.allocator;
    const count = (try strippedAlloc(allocator, "count_req")).?;
    defer allocator.free(count);
    try std.testing.expectEqualStrings("count", count);
    const needle = (try strippedAlloc(allocator, "needle_unowned")).?;
    defer allocator.free(needle);
    try std.testing.expectEqualStrings("needle", needle);
    try std.testing.expect(try strippedAlloc(allocator, "_req") == null);
    try std.testing.expect(try strippedAlloc(allocator, "request") == null);
}
