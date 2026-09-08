//! Build provenance, independent of configuration convenience APIs.
const std = @import("std");
const plugin_api = @import("plugin");
const abi = @import("abi");

pub const Options = struct {
    ghostty_revision: []const u8 = "",
    zigo_version: []const u8 = "",
    optimize: []const u8 = "",
    simd: bool = false,
    kitty_graphics: bool = false,
    tmux_control_mode: bool = false,
};

pub const plugin: plugin_api.Plugin = .{
    .name = "BUILD_INFO",
    .TypeOptions = Options,
    .targets = &.{.handle},
    .files = &.{.{ .pathAlloc = filePath, .render = renderFile }},
};

fn filePath(allocator: std.mem.Allocator, program: abi.Program, options: plugin_api.Options) ![]u8 {
    return plugin_api.publicFilePathAlloc(allocator, program, options, "zigo_build_info_gen.go");
}

fn renderFile(allocator: std.mem.Allocator, writer: *std.Io.Writer, program: abi.Program, output: plugin_api.Options) !void {
    if (output.active_package) |active| if (active.len != 0) return;
    var metadata: ?Options = null;
    for (program.types) |declaration| {
        const value = try plugin_api.readOptions(plugin, .type, allocator, declaration.ext) orelse continue;
        if (declaration.package != null) return error.InvalidBuildInfoAnchor;
        if (metadata != null) return error.DuplicateBuildInfo;
        metadata = value;
    }
    const options = metadata orelse return;
    try writer.writeAll(@embedFile("build_info.go.txt"));
    try writer.writeAll("// GetBuildInfo identifies the bundled native build. Values are fixed at generation time.\nfunc GetBuildInfo() BuildInfo { return BuildInfo{GhosttyRevision: ");
    try std.json.Stringify.value(options.ghostty_revision, .{}, writer);
    try writer.writeAll(", ZigoVersion: ");
    try std.json.Stringify.value(options.zigo_version, .{}, writer);
    try writer.writeAll(", Optimize: ");
    try std.json.Stringify.value(options.optimize, .{}, writer);
    try writer.print(", SIMD: {}, KittyGraphics: {}, TmuxControlMode: {}}} }}\n", .{ options.simd, options.kitty_graphics, options.tmux_control_mode });
}
