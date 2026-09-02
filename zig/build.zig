const std = @import("std");
const zigo = @import("zigo");

pub fn build(b: *std.Build) void {
    const target = b.standardTargetOptions(.{});
    const optimize = b.standardOptimizeOption(.{});

    const ghostty = b.dependency("ghostty", .{
        .target = target,
        .optimize = optimize,
    });

    const ghostty_vt = ghostty.module("ghostty-vt");

    const gostty = b.addModule("gostty", .{
        .root_source_file = b.path("src/root.zig"),
        .target = target,
        .optimize = optimize,
    });
    gostty.addImport("ghostty_vt", ghostty_vt);

    // zigo forwards static link inputs to the cgo link line, but it reads them
    // off the module it was handed and does not walk imports. simdutf and
    // highway hang off ghostty-vt, so re-link them here where zigo will see
    // them. See docs/zigo-findings.md.
    var seen: std.AutoHashMapUnmanaged(*std.Build.Module, void) = .empty;
    forwardStaticLibraries(b, ghostty_vt, gostty, &seen);

    // ghostty's vendored C/C++ is compiled with full UBSan, and its handlers
    // live in Zig's ubsan_rt. Zig links that runtime when it produces a final
    // binary, but the binding library is a static archive, which Zig does not
    // link -- so nothing pulls the runtime in. zigo forwards the module's own
    // static link inputs to cgo, but ubsan_rt is the compiler's, not the
    // module's, so build it here and add it to the link line.
    //
    // ghostty guards the same problem for Windows only
    // (`src/build/SharedDeps.zig:986`); consuming the archive from cgo hits it
    // on every platform.
    const ubsan_rt = b.addLibrary(.{
        .name = "ubsan_rt",
        .linkage = .static,
        .root_module = b.createModule(.{
            .root_source_file = .{
                .cwd_relative = b.pathJoin(&.{ b.graph.zig_lib_directory.path.?, "ubsan_rt.zig" }),
            },
            .target = target,
            .optimize = optimize,
        }),
    });
    // ubsan_rt itself calls into compiler_rt for soft-float conversions.
    ubsan_rt.bundle_compiler_rt = true;

    const bindings = zigo.addGoBindings(b, .{
        .name = "gostty",
        .module = gostty,
        .bindings = b.path("src/bindings.zig"),
        .go_dir = b.path(".."),
        .go_module = "github.com/ironpark/gostty",
        .go_package = "gostty",
        // Publish at the module root, so the import path is the module itself.
        .go_package_path = ".",
        .target = target,
        .optimize = optimize,
        .cgo_flags = .{ .extra_ldflags = &.{
            "${SRCDIR}/../../zig/zig-out/lib/libubsan_rt.a",
        } },
    });
    b.getInstallStep().dependOn(&b.addInstallArtifact(ubsan_rt, .{}).step);

    _ = bindings.addStandardSteps(b, .{});
}

/// Re-links every static library reachable from `from` onto `to`, so a caller
/// that only inspects `to` still sees them.
fn forwardStaticLibraries(
    b: *std.Build,
    from: *std.Build.Module,
    to: *std.Build.Module,
    seen: *std.AutoHashMapUnmanaged(*std.Build.Module, void),
) void {
    if (seen.contains(from)) return;
    seen.put(b.allocator, from, {}) catch @panic("OOM");
    for (from.link_objects.items) |object| switch (object) {
        .other_step => |compile| if (compile.isStaticLibrary()) to.linkLibrary(compile),
        else => {},
    };
    for (from.import_table.values()) |import| forwardStaticLibraries(b, import, to, seen);
}
