//! Build provenance shared by the Go API and machine-readable artifact.
const std = @import("std");
const abi = @import("abi");
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

pub const plugin: plugin_api.Plugin = .{
    .name = "BUILD_INFO",
    .Config = Config,
    .TypeOptions = Options,
    .subjects = &.{.handle},
    .validate = validate,
    .go = .{ .source_files = &.{.{ .scope = .document, .pathAlloc = filePath, .render = renderFile }} },
    .artifacts = &.{.{ .pathAlloc = artifactPath, .render = renderArtifact }},
};

/// The one handle carrying the feature options, checked here so the two
/// renderings below can take it for granted.
fn validate(context: plugin_api.ValidateContext) !void {
    var found = false;
    for (context.document.types) |declaration| {
        _ = try context.optionsOf(plugin, .type, declaration.ext) orelse continue;
        if (found or declaration.package != null) {
            try context.diagnose(.{
                .severity = .@"error",
                .code = "BUILD_INFO002",
                .message = "build information needs exactly one root-package handle",
                .site = plugin_api.site.typeSite(declaration),
                .hint = "Attach native feature options only to Terminal.",
            });
            continue;
        }
        found = true;
    }
    if (!found) try context.diagnose(.{
        .severity = .@"error",
        .code = "BUILD_INFO003",
        .message = "native build feature information is missing",
        .site = plugin_api.site.documentSite("BUILD_INFO"),
        .hint = "Attach BUILD_INFO feature options to Terminal.",
    });
}

/// The build configuration joined with the feature options off the handle
/// that carries them. Read from the program rather than from facts because
/// an artifact renders without a fact store, and the source file has no
/// reason to read the same thing another way.
fn metadata(allocator: std.mem.Allocator, program: abi.Program, config: Config) !Metadata {
    for (program.types) |declaration| {
        if (declaration.package != null) continue;
        const features = try plugin_api.optionsOn(plugin, .type, allocator, declaration.ext) orelse continue;
        return .{
            .ghostty_revision = config.ghostty_revision,
            .zigo_version = config.zigo_version,
            .optimize = config.optimize,
            .simd = features.simd,
            .kitty_graphics = features.kitty_graphics,
            .tmux_control_mode = features.tmux_control_mode,
        };
    }
    // `validate` already refused a document without the handle.
    return error.MissingBuildInfo;
}

fn filePath(context: plugin_api.GoContext) ![]u8 {
    return context.sourceFilePathAlloc("zigo_build_info_gen.go");
}

fn renderFile(context: plugin_api.GoContext, writer: *std.Io.Writer) !void {
    const info = try metadata(context.allocator, context.program, try context.config(plugin));
    const b = context.builder();
    try b.render(writer, &.{
        .{ .raw = @embedFile("build_info.go.txt") },
        try b.func(.{
            .doc = .{ .text = "GetBuildInfo identifies the bundled native build. Values are fixed at generation time." },
            .name = "GetBuildInfo",
            .signature = .{ .explicit = .{ .results = &.{b.ident("BuildInfo")} } },
            .body = &.{try b.ret(&.{try b.composite(b.ident("BuildInfo"), &.{
                .{ .key = "GhosttyRevision", .value = b.string(info.ghostty_revision) },
                .{ .key = "ZigoVersion", .value = b.string(info.zigo_version) },
                .{ .key = "Optimize", .value = b.string(info.optimize) },
                .{ .key = "SIMD", .value = b.boolean(info.simd) },
                .{ .key = "KittyGraphics", .value = b.boolean(info.kitty_graphics) },
                .{ .key = "TmuxControlMode", .value = b.boolean(info.tmux_control_mode) },
            })})},
            .single_line = true,
        }),
    }, .{});
}

fn artifactPath(context: plugin_api.ArtifactContext) ![]u8 {
    return context.allocator.dupe(u8, "build-info.json");
}

fn renderArtifact(context: plugin_api.ArtifactContext, writer: *std.Io.Writer) !void {
    const info = try metadata(context.allocator, context.program, try context.config(plugin));
    try std.json.Stringify.value(info, .{ .whitespace = .indent_2 }, writer);
    try writer.writeByte('\n');
}
