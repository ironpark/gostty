//! Repository-specific Go adapters. Explicit features select templates;
//! generation validates their required native API before writing Go output.
const std = @import("std");
const plugin_api = @import("plugin");
const semantic = @import("semantic");

pub const Feature = enum { terminal_config, clipboard_reply };
pub const Options = struct { feature: Feature };

pub const plugin: plugin_api.Plugin = .{
    .name = "CONVENIENCE",
    .TypeOptions = Options,
    .subjects = &.{.handle},
    .validate = validateDocument,
    .visit = visit,
};

/// The template goes after the handle it extends, in the file that declares
/// it. A template is hand-written Go kept beside generated code, which is what
/// the builder's `raw` declaration is for.
fn visit(context: plugin_api.Context, node: plugin_api.Node, b: *plugin_api.Builder) !void {
    if (node != .type) return;
    const options = try context.optionsOf(plugin, .type, node) orelse return;
    try b.emit(&.{.{ .raw = switch (options.feature) {
        .terminal_config => @embedFile("terminal.go.txt"),
        .clipboard_reply => @embedFile("clipboard.go.txt"),
    } }}, .{});
}

/// A session the template writes methods on. `requirements` covers the
/// functions a template calls; this covers the generated lifecycle type it
/// hangs `Stream`, `Write` and `PlainText` off, which is not a function and so
/// is not reachable through `document.functions`.
const SessionRequirement = struct { name: []const u8, primary: []const u8, child: []const u8 };
fn sessionRequirements(feature: Feature) []const SessionRequirement {
    return switch (feature) {
        .terminal_config => &.{.{ .name = "Session", .primary = "Terminal", .child = "Stream" }},
        .clipboard_reply => &.{},
    };
}

/// A constructor option the template wraps. `requirements` covers methods and
/// `sessionRequirements` the generated session; this covers the `With*` the
/// generator emits from an `.options` parameter. A field dropped from that list
/// in the binding would take the wrapper's `With*` with it, and the template
/// would fail as a Go compile error inside generated code -- which is the
/// report a plugin diagnostic exists to replace.
const OptionRequirement = struct { constructor: []const u8, field: []const u8 };
fn optionRequirements(feature: Feature) []const OptionRequirement {
    return switch (feature) {
        .terminal_config => &.{
            .{ .constructor = "newTerminal", .field = "max_scrollback_bytes" },
            .{ .constructor = "newTerminal", .field = "max_scrollback_lines" },
            .{ .constructor = "newTerminal", .field = "default_cursor_blink" },
        },
        .clipboard_reply => &.{},
    };
}

/// An `io` wrapper the template calls. `Stream.Write` and `WriteString` are
/// not functions of the document but the wrappers `implements` writes beside
/// the bound `feed`, so they are found through that attachment rather than by
/// public name.
const ImplementsRequirement = struct { owner: []const u8, kind: semantic.Implements };
fn implementsRequirements(feature: Feature) []const ImplementsRequirement {
    return switch (feature) {
        .terminal_config => &.{
            .{ .owner = "Stream", .kind = .writer },
            .{ .owner = "Stream", .kind = .string_writer },
        },
        .clipboard_reply => &.{},
    };
}

const Requirement = struct { owner: []const u8, method: []const u8 };
fn requirements(feature: Feature) []const Requirement {
    return switch (feature) {
        .terminal_config => &.{
            .{ .owner = "Terminal", .method = "NewTerminal" },
            .{ .owner = "Terminal", .method = "NewStream" },
            .{ .owner = "Terminal", .method = "SetDefaultBackgroundColor" },
            .{ .owner = "Terminal", .method = "SetDefaultForegroundColor" },
            .{ .owner = "Terminal", .method = "SetDefaultCursorColor" },
            .{ .owner = "Terminal", .method = "SetDefaultMode" },
            .{ .owner = "Terminal", .method = "Format" },
            .{ .owner = "Screen", .method = "Format" },
            .{ .owner = "Stream", .method = "SetUnknownMaxBytes" },
            .{ .owner = "Stream", .method = "SetVersionReport" },
            .{ .owner = "Stream", .method = "SetEnquiryResponse" },
            .{ .owner = "Stream", .method = "ColorSchemeChanged" },
            .{ .owner = "Stream", .method = "OnClipboardReadRequest" },
            .{ .owner = "Stream", .method = "OnClipboardWriteRequest" },
        },
        .clipboard_reply => &.{
            .{ .owner = "ClipboardRequest", .method = "ClearReplyContents" },
            .{ .owner = "ClipboardRequest", .method = "AddContent" },
            .{ .owner = "ClipboardRequest", .method = "ReplyContents" },
        },
    };
}

fn validateDocument(context: plugin_api.ValidateContext) !void {
    const allocator = context.allocator;
    const document = context.document;
    for (document.types) |declaration| {
        const options = try context.optionsOf(plugin, .type, declaration.ext) orelse continue;
        const expected = switch (options.feature) {
            .terminal_config => "Terminal",
            .clipboard_reply => "ClipboardRequest",
        };
        if (!std.mem.eql(u8, declaration.name, expected) or declaration.package != null) {
            try context.diagnose(.{
                .severity = .@"error",
                .code = "CONVENIENCE002",
                .message = try std.fmt.allocPrint(allocator, "{s} requires root-package {s}", .{ @tagName(options.feature), expected }),
                .site = plugin_api.site.typeSite(declaration),
                .hint = "Attach this feature to its required handle.",
            });
        }
        for (optionRequirements(options.feature)) |required| {
            if (hasOptionField(document, required)) continue;
            try context.diagnose(.{
                .severity = .@"error",
                .code = "CONVENIENCE005",
                .message = try std.fmt.allocPrint(allocator, "{s} requires {s} to expose option field `{s}`", .{ @tagName(options.feature), required.constructor, required.field }),
                .site = plugin_api.site.typeSite(declaration),
                .hint = "List the field in the constructor's zigo.param.options, or drop the adapter that wraps its With* option.",
            });
        }
        for (sessionRequirements(options.feature)) |required| {
            if (hasSession(document, required)) continue;
            try context.diagnose(.{
                .severity = .@"error",
                .code = "CONVENIENCE004",
                .message = try std.fmt.allocPrint(allocator, "{s} requires session {s} over {s} with child {s}", .{ @tagName(options.feature), required.name, required.primary, required.child }),
                .site = plugin_api.site.typeSite(declaration),
                .hint = "Declare the session with zigo.session, or drop the adapter methods that extend it.",
            });
        }
        for (implementsRequirements(options.feature)) |required| {
            if (try hasImplements(allocator, document, required)) continue;
            try context.diagnose(.{
                .severity = .@"error",
                .code = "CONVENIENCE003",
                .message = try std.fmt.allocPrint(allocator, "{s} requires a {s}.{s} wrapper", .{ @tagName(options.feature), required.owner, required.kind.interfaceName() }),
                .site = plugin_api.site.typeSite(declaration),
                .hint = "Keep the `implements` kind on the method the wrapper is written beside, or update the adapter template.",
            });
        }
        for (requirements(options.feature)) |required| {
            if (try hasMethod(allocator, document, context.target, required)) continue;
            try context.diagnose(.{
                .severity = .@"error",
                .code = "CONVENIENCE003",
                .message = try std.fmt.allocPrint(allocator, "{s} requires {s}.{s}", .{ @tagName(options.feature), required.owner, required.method }),
                .site = plugin_api.site.typeSite(declaration),
                .hint = "Restore the required method or update the adapter template and its requirements together.",
            });
        }
    }
}

fn hasMethod(allocator: std.mem.Allocator, document: semantic.Semantic, target: anytype, required: Requirement) !bool {
    for (document.functions) |function| {
        if (function.package != null) continue;
        if (!std.mem.eql(u8, function.receiver orelse function.goOwner() orelse "", required.owner)) continue;
        const name = try target.publicFunctionNameAlloc(allocator, document, function);
        defer allocator.free(name);
        if (std.mem.eql(u8, name, required.method)) return true;
    }
    return false;
}

fn hasImplements(allocator: std.mem.Allocator, document: semantic.Semantic, required: ImplementsRequirement) !bool {
    for (document.functions) |function| {
        if (function.package != null) continue;
        if (!std.mem.eql(u8, function.receiver orelse function.goOwner() orelse "", required.owner)) continue;
        const options = try plugin_api.builtins.implements.read(allocator, function.ext) orelse continue;
        for (options.kinds) |kind| if (kind == required.kind) return true;
    }
    return false;
}

/// The template's session methods compile only against a session of exactly
/// this shape: the primary supplies the receiver `Terminal()` returns, and the
/// child supplies the slice `Streams()` returns.
fn hasSession(document: semantic.Semantic, required: SessionRequirement) bool {
    for (document.sessions orelse &.{}) |session| {
        if (!std.mem.eql(u8, session.name, required.name)) continue;
        if (session.package != null) continue;
        if (!std.mem.eql(u8, session.primary, required.primary)) continue;
        for (session.children) |child| {
            if (std.mem.eql(u8, child.base(), required.child)) return true;
        }
    }
    return false;
}

/// The field has to be both listed for lowering and carried by an `.options`
/// parameter: `.flatten` alone lowers it as a positional argument and emits no
/// `With*`, and a field with no Zig default becomes positional even under
/// `.options`, so the default is what decides an option exists.
fn hasOptionField(document: semantic.Semantic, required: OptionRequirement) bool {
    for (document.functions) |function| {
        if (function.package != null) continue;
        if (!std.mem.eql(u8, function.name, required.constructor)) continue;
        for (function.params) |parameter| {
            if (parameter.goOptions() == null) continue;
            for (parameter.flatten orelse &.{}) |field| {
                if (!std.mem.eql(u8, field.name, required.field)) continue;
                return field.default != null;
            }
        }
    }
    return false;
}
