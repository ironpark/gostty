//! Build provenance shared by the Go API and machine-readable artifact.
const std = @import("std");
const plugin_api = @import("plugin");

pub const Config = struct {
    ghostty_revision: []const u8 = "",
    zigo_version: []const u8 = "",
    optimize: []const u8 = "",
};

/// Feature values come from the native module's reflected build options.
pub const Options = struct {
    simd: bool = false,
    kitty_graphics: bool = false,
    tmux_control_mode: bool = false,
};

pub const Metadata = struct {
    ghostty_revision: []const u8,
    zigo_version: []const u8,
    optimize: []const u8,
    simd: bool,
    kitty_graphics: bool,
    tmux_control_mode: bool,
};
const metadata_id: plugin_api.DeclarationId = .{ .kind = .document, .name = "build" };

pub const plugin: plugin_api.Plugin = .{
    .name = "BUILD_INFO",
    .min_contract = .{ .major = 2, .minor = 0 },
    .Config = Config,
    .TypeOptions = Options,
    .Facts = Metadata,
    .targets = &.{.handle},
    .validate = validate,
    .go_files = &.{.{ .scope = .document, .pathAlloc = filePath, .render = renderFile }},
    .artifacts = &.{.{ .pathAlloc = artifactPath, .render = renderArtifact }},
};

fn validate(context: plugin_api.ValidateContext) !void {
    const config = try context.config(plugin);
    var found = false;
    for (context.document.types) |declaration| {
        const features = try context.optionsOf(plugin, .type, declaration.ext) orelse continue;
        if (found or declaration.package != null) {
            try context.diagnose(.{
                .severity = .@"error",
                .code = "BUILD_INFO002",
                .message = "build information needs exactly one root-package handle",
                .site = .{ .path = "semantic.json", .declaration = declaration.name },
                .hint = "Attach native feature options only to Terminal.",
            });
            continue;
        }
        found = true;
        try context.facts.put(context.allocator, plugin, metadata_id, .{
            .ghostty_revision = config.ghostty_revision,
            .zigo_version = config.zigo_version,
            .optimize = config.optimize,
            .simd = features.simd,
            .kitty_graphics = features.kitty_graphics,
            .tmux_control_mode = features.tmux_control_mode,
        });
    }
    if (!found) try context.diagnose(.{
        .severity = .@"error",
        .code = "BUILD_INFO003",
        .message = "native build feature information is missing",
        .site = .{ .path = "semantic.json", .declaration = "BUILD_INFO" },
        .hint = "Attach BUILD_INFO feature options to Terminal.",
    });
}

fn filePath(context: plugin_api.Context) ![]u8 {
    return context.goFilePathAlloc("zigo_build_info_gen.go");
}

fn renderFile(context: plugin_api.Context, writer: *std.Io.Writer) !void {
    const options = (try context.options.facts.get(plugin, metadata_id)).?;
    try writer.writeAll(@embedFile("build_info.go.txt"));
    try writer.writeAll("// GetBuildInfo identifies the bundled native build. Values are fixed at generation time.\nfunc GetBuildInfo() BuildInfo { return BuildInfo{GhosttyRevision: ");
    try std.json.Stringify.value(options.ghostty_revision, .{}, writer);
    try writer.writeAll(", ZigoVersion: ");
    try std.json.Stringify.value(options.zigo_version, .{}, writer);
    try writer.writeAll(", Optimize: ");
    try std.json.Stringify.value(options.optimize, .{}, writer);
    try writer.print(", SIMD: {}, KittyGraphics: {}, TmuxControlMode: {}}} }}\n", .{ options.simd, options.kitty_graphics, options.tmux_control_mode });
}

fn artifactPath(context: plugin_api.ArtifactContext) ![]u8 {
    return context.allocator.dupe(u8, "build-info.json");
}

fn renderArtifact(context: plugin_api.ArtifactContext, writer: *std.Io.Writer) !void {
    const metadata = (try context.options.facts.get(plugin, metadata_id)).?;
    try std.json.Stringify.value(metadata, .{ .whitespace = .indent_2 }, writer);
    try writer.writeByte('\n');
}
