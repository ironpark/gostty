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
    .targets = &.{.handle},
    .min_contract = .{ .major = 2, .minor = 0 },
    .validate = validateDocument,
    .type_hook = typeHook,
};

fn typeHook(context: plugin_api.Context, writer: *std.Io.Writer, declaration: semantic.TypeDecl) !void {
    const options = try context.typeOptions(plugin, declaration) orelse return;
    try writer.writeAll(switch (options.feature) {
        .terminal_config => @embedFile("terminal.go.txt"),
        .clipboard_reply => @embedFile("clipboard.go.txt"),
    });
}

const Requirement = struct { owner: []const u8, method: []const u8 };
fn requirements(feature: Feature) []const Requirement {
    return switch (feature) {
        .terminal_config => &.{
            .{ .owner = "Terminal", .method = "NewTerminal" },
            .{ .owner = "Terminal", .method = "NewStream" },
            .{ .owner = "Terminal", .method = "SetScrollbackMaxBytes" },
            .{ .owner = "Terminal", .method = "SetScrollbackMaxLines" },
            .{ .owner = "Terminal", .method = "SetDefaultBackgroundColor" },
            .{ .owner = "Terminal", .method = "SetDefaultForegroundColor" },
            .{ .owner = "Terminal", .method = "SetDefaultCursorColor" },
            .{ .owner = "Terminal", .method = "SetDefaultCursorBlink" },
            .{ .owner = "Terminal", .method = "SetDefaultMode" },
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
        const options = try plugin_api.readOptions(plugin, .type, allocator, declaration.ext) orelse continue;
        const expected = switch (options.feature) {
            .terminal_config => "Terminal",
            .clipboard_reply => "ClipboardRequest",
        };
        if (!std.mem.eql(u8, declaration.name, expected) or declaration.package != null) {
            try context.diagnose(.{
                .severity = .@"error",
                .code = "CONVENIENCE002",
                .message = try std.fmt.allocPrint(allocator, "{s} requires root-package {s}", .{ @tagName(options.feature), expected }),
                .site = .{ .path = "semantic.json", .declaration = declaration.name },
                .hint = "Attach this feature to its required handle.",
            });
        }
        for (requirements(options.feature)) |required| {
            if (try hasMethod(allocator, document, required)) continue;
            try context.diagnose(.{
                .severity = .@"error",
                .code = "CONVENIENCE003",
                .message = try std.fmt.allocPrint(allocator, "{s} requires {s}.{s}", .{ @tagName(options.feature), required.owner, required.method }),
                .site = .{ .path = "semantic.json", .declaration = declaration.name },
                .hint = "Restore the required method or update the adapter template and its requirements together.",
            });
        }
    }
}

fn hasMethod(allocator: std.mem.Allocator, document: semantic.Semantic, required: Requirement) !bool {
    for (document.functions) |function| {
        if (function.package != null) continue;
        if (!std.mem.eql(u8, function.receiver orelse function.goOwner() orelse "", required.owner)) continue;
        const name = try semantic.publicFunctionNameAlloc(allocator, document, function);
        defer allocator.free(name);
        if (std.mem.eql(u8, name, required.method)) return true;
    }
    return false;
}
