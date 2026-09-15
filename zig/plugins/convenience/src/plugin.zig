//! Repository-specific Go adapters. Explicit features select templates;
//! generation validates their required native API before writing Go output.
const std = @import("std");
const plugin_api = @import("plugin");
const semantic = @import("semantic");

pub const Feature = enum { terminal_config, clipboard_reply };

const MethodRequirement = struct {
    function: plugin_api.ref.Function,
    name: []const u8,
};
const MethodSet = struct {
    owner: plugin_api.ref.Type,
    methods: []const MethodRequirement,
};
const OptionRequirement = struct { constructor: plugin_api.ref.Function, field: []const u8 };
const SessionRequirement = struct {
    name: []const u8,
    primary: plugin_api.ref.Type,
    child: plugin_api.ref.Type,
};
const ImplementsRequirement = struct {
    function: plugin_api.ref.Function,
    kind: semantic.Implements,
};

pub const Options = struct {
    feature: Feature,
    target: plugin_api.ref.Type,
    method_sets: []const MethodSet = &.{},
    option_fields: []const OptionRequirement = &.{},
    sessions: []const SessionRequirement = &.{},
    implements: []const ImplementsRequirement = &.{},
};

/// The complete native contract behind the terminal convenience template.
/// The caller supplies checked zigo scopes; this helper keeps the dependency
/// list beside the template while every entry remains a declaration reference.
pub fn terminalConfig(comptime entry: anytype, comptime api: type, comptime terminal: type, comptime stream: type) @TypeOf(entry) {
    return entry.use(plugin, .{
        .feature = .terminal_config,
        .target = api.typeRef("Terminal"),
        .method_sets = &.{
            .{ .owner = api.typeRef("Terminal"), .methods = &.{
                .{ .function = terminal.ref("init"), .name = "NewTerminal" },
                .{ .function = api.ref("newStream"), .name = "NewStream" },
                .{ .function = api.ref("setDefaultBackgroundColor"), .name = "SetDefaultBackgroundColor" },
                .{ .function = api.ref("setDefaultForegroundColor"), .name = "SetDefaultForegroundColor" },
                .{ .function = api.ref("setDefaultCursorColor"), .name = "SetDefaultCursorColor" },
                .{ .function = api.ref("setDefaultMode"), .name = "SetDefaultMode" },
                .{ .function = api.ref("formatTerminal"), .name = "Format" },
            } },
            .{ .owner = api.typeRef("Screen"), .methods = &.{
                .{ .function = api.ref("screenFormat"), .name = "Format" },
            } },
            .{ .owner = api.typeRef("Stream"), .methods = &.{
                .{ .function = stream.ref("setUnknownMaxBytes"), .name = "SetUnknownMaxBytes" },
                .{ .function = stream.ref("setVersionReport"), .name = "SetVersionReport" },
                .{ .function = stream.ref("setEnquiryResponse"), .name = "SetEnquiryResponse" },
                .{ .function = stream.ref("colorSchemeChanged"), .name = "ColorSchemeChanged" },
                .{ .function = stream.ref("onClipboardReadRequest"), .name = "OnClipboardReadRequest" },
                .{ .function = stream.ref("onClipboardWriteRequest"), .name = "OnClipboardWriteRequest" },
            } },
        },
        .option_fields = &.{
            .{ .constructor = terminal.ref("init"), .field = "max_scrollback_bytes" },
            .{ .constructor = terminal.ref("init"), .field = "max_scrollback_lines" },
            .{ .constructor = terminal.ref("init"), .field = "default_cursor_blink" },
        },
        .sessions = &.{.{
            .name = "Session",
            .primary = api.typeRef("Terminal"),
            .child = api.typeRef("Stream"),
        }},
        .implements = &.{
            .{ .function = stream.ref("feed"), .kind = .writer },
            .{ .function = stream.ref("feed"), .kind = .string_writer },
        },
    });
}

/// The complete native contract behind the clipboard reply template.
pub fn clipboardReply(comptime entry: anytype, comptime api: type, comptime clipboard: type) @TypeOf(entry) {
    return entry.use(plugin, .{
        .feature = .clipboard_reply,
        .target = api.typeRef("ClipboardRequest"),
        .method_sets = &.{.{ .owner = api.typeRef("ClipboardRequest"), .methods = &.{
            .{ .function = clipboard.ref("clearReplyContents"), .name = "ClearReplyContents" },
            .{ .function = clipboard.ref("addContent"), .name = "AddContent" },
            .{ .function = clipboard.ref("replyContents"), .name = "ReplyContents" },
        } }},
    });
}

pub const plugin: plugin_api.Plugin = .{
    .name = "CONVENIENCE",
    .TypeOptions = Options,
    .subjects = &.{.handle},
    .uses = &.{plugin_api.capabilities.implements_wrappers},
    .validate = validateDocument,
    .go = .{ .visit = visit },
};

/// The template goes after the handle it extends, in the file that declares
/// it. A template is hand-written Go kept beside generated code, which is what
/// the builder's `raw` declaration is for.
fn visit(context: plugin_api.GoContext, node: plugin_api.Node, b: *plugin_api.Builder) !void {
    if (node != .type) return;
    const options = try context.optionsOf(plugin, .type, node) orelse return;
    try b.emit(&.{.{ .raw = switch (options.feature) {
        .terminal_config => @embedFile("terminal.go.txt"),
        .clipboard_reply => @embedFile("clipboard.go.txt"),
    } }}, .{});
}

/// Verify the declarations referenced by a convenience template still lower
/// to the public Go contract that the handwritten template calls.
fn validateDocument(context: plugin_api.ValidateContext) !void {
    const allocator = context.allocator;
    const document = context.document;
    for (document.types) |declaration| {
        const options = try context.optionsOf(plugin, .type, declaration.ext) orelse continue;
        const expected = try context.resolveType(options.target) orelse continue;
        if (!sameType(declaration, expected.*) or declaration.package != null) {
            try context.diagnose(.{
                .severity = .@"error",
                .code = "CONVENIENCE002",
                .message = try std.fmt.allocPrint(allocator, "{s} requires root-package {s}", .{ @tagName(options.feature), expected.name }),
                .site = plugin_api.site.typeSite(declaration),
                .hint = "Attach this feature to its required handle.",
            });
        }
        for (options.option_fields) |required| {
            const constructor = try context.resolveFunction(required.constructor) orelse continue;
            if (hasOptionField(constructor.*, required.field)) continue;
            try context.diagnose(.{
                .severity = .@"error",
                .code = "CONVENIENCE005",
                .message = try std.fmt.allocPrint(allocator, "{s} requires {s} to expose option field `{s}`", .{ @tagName(options.feature), constructor.name, required.field }),
                .site = plugin_api.site.typeSite(declaration),
                .hint = "List the field in the constructor's zigo.param.options, or drop the adapter that wraps its With* option.",
            });
        }
        for (options.sessions) |required| {
            const primary = try context.resolveType(required.primary) orelse continue;
            const child = try context.resolveType(required.child) orelse continue;
            if (hasSession(document, required.name, primary.name, child.name)) continue;
            try context.diagnose(.{
                .severity = .@"error",
                .code = "CONVENIENCE004",
                .message = try std.fmt.allocPrint(allocator, "{s} requires session {s} over {s} with child {s}", .{ @tagName(options.feature), required.name, primary.name, child.name }),
                .site = plugin_api.site.typeSite(declaration),
                .hint = "Declare the session with zigo.session, or drop the adapter methods that extend it.",
            });
        }
        for (options.implements) |required| {
            const function = try context.resolveFunction(required.function) orelse continue;
            if (try hasImplements(allocator, function.*, required.kind)) continue;
            const owner = function.receiver orelse function.goOwner() orelse "";
            try context.diagnose(.{
                .severity = .@"error",
                .code = "CONVENIENCE003",
                .message = try std.fmt.allocPrint(allocator, "{s} requires a {s}.{s} wrapper", .{ @tagName(options.feature), owner, required.kind.interfaceName() }),
                .site = plugin_api.site.typeSite(declaration),
                .hint = "Keep the `implements` kind on the method the wrapper is written beside, or update the adapter template.",
            });
        }
        for (options.method_sets) |set| {
            const owner = try context.resolveType(set.owner) orelse continue;
            for (set.methods) |required| {
                const function = try context.resolveFunction(required.function) orelse continue;
                if (try isMethod(allocator, document, context.target, function.*, owner.name, required.name)) continue;
                try context.diagnose(.{
                    .severity = .@"error",
                    .code = "CONVENIENCE003",
                    .message = try std.fmt.allocPrint(allocator, "{s} requires {s}.{s}", .{ @tagName(options.feature), owner.name, required.name }),
                    .site = plugin_api.site.typeSite(declaration),
                    .hint = "Restore the required method or update the adapter template and its requirements together.",
                });
            }
        }
    }
}

fn sameType(actual: semantic.TypeDecl, expected: semantic.TypeDecl) bool {
    const actual_path = actual.zig_path orelse return false;
    const expected_path = expected.zig_path orelse return false;
    return std.mem.eql(u8, actual_path, expected_path);
}

fn isMethod(allocator: std.mem.Allocator, document: semantic.Semantic, target: anytype, function: semantic.SemanticFn, owner: []const u8, expected_name: []const u8) !bool {
    if (function.package != null) return false;
    if (!std.mem.eql(u8, function.receiver orelse function.goOwner() orelse "", owner)) return false;
    const name = try target.publicFunctionNameAlloc(allocator, document, function);
    defer allocator.free(name);
    return std.mem.eql(u8, name, expected_name);
}

fn hasImplements(allocator: std.mem.Allocator, function: semantic.SemanticFn, expected: semantic.Implements) !bool {
    const options = try plugin_api.builtins.implements.read(allocator, function.ext) orelse return false;
    for (options.kinds) |kind| if (kind == expected) return true;
    return false;
}

/// The template's session methods compile only against a session of exactly
/// this shape: the primary supplies the receiver `Terminal()` returns, and the
/// child supplies the slice `Streams()` returns.
fn hasSession(document: semantic.Semantic, name: []const u8, primary: []const u8, child_type: []const u8) bool {
    for (document.sessions orelse &.{}) |session| {
        if (!std.mem.eql(u8, session.name, name)) continue;
        if (session.package != null) continue;
        if (!std.mem.eql(u8, session.primary, primary)) continue;
        for (session.children) |child| {
            if (std.mem.eql(u8, child.base(), child_type)) return true;
        }
    }
    return false;
}

/// The field has to be both listed for lowering and carried by an `.options`
/// parameter: `.flatten` alone lowers it as a positional argument and emits no
/// `With*`, and a field with no Zig default becomes positional even under
/// `.options`, so the default is what decides an option exists.
fn hasOptionField(function: semantic.SemanticFn, field_name: []const u8) bool {
    if (function.package != null) return false;
    for (function.params) |parameter| {
        if (parameter.goOptions() == null) continue;
        for (parameter.flatten orelse &.{}) |field| {
            if (!std.mem.eql(u8, field.name, field_name)) continue;
            return field.default != null;
        }
    }
    return false;
}
