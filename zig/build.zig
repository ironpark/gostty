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
    const install_ubsan = b.addInstallArtifact(ubsan_rt, .{});
    b.getInstallStep().dependOn(&install_ubsan.step);

    // The binding library is useless to cgo without the runtime beside it, so
    // whatever step produces one produces the other.
    const steps = bindings.addStandardSteps(b, .{});
    steps.library.dependOn(&install_ubsan.step);
}
