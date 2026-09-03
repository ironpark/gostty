const std = @import("std");
const zigo = @import("zigo");

// Where the native archives and the C header are installed. The install prefix
// is the repository root (see the Makefile), so this is `libs/` beside the Go
// package rather than a `zig-out` buried under the Zig tree: the archives are
// what the Go build links, so they belong somewhere a Go developer would look.
const libs_dir: std.Build.InstallDir = .{ .custom = "libs" };

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

    // A Debug build compiles ghostty's vendored C/C++ with full UBSan, whose
    // handlers live in Zig's ubsan_rt. Zig links that runtime when it produces
    // a final binary, but the binding library is a static archive, which Zig
    // does not link -- so nothing pulls the runtime in, and cgo is left with
    // undefined `__ubsan_handle_*` symbols. zigo forwards the module's own
    // static link inputs to cgo, but ubsan_rt is the compiler's, not the
    // module's, so it has to be built here and added to the link line.
    //
    // ghostty guards the same problem for Windows only
    // (`src/build/SharedDeps.zig:986`); consuming the archive from cgo hits it
    // on every platform.
    //
    // Only in Debug: the release modes do not reference a single one of those
    // symbols, so shipping a three megabyte sanitizer runtime with them would
    // be dead weight on disk and a puzzle for whoever found it there.
    const debug_ubsan = optimize == .Debug;
    const ubsan_rt = if (debug_ubsan) rt: {
        const lib = b.addLibrary(.{
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
        lib.bundle_compiler_rt = true;
        break :rt lib;
    } else null;

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
        // The archives are installed together under the repository's `libs`,
        // which is where the generated cgo directives point.
        .install = .{
            .library_dir = libs_dir,
            .header_dir = .{ .custom = "libs/include" },
        },
        // Written into the generated cgo directives, so it follows the
        // optimize mode: see the Makefile, which builds through the generator
        // for that reason.
        .cgo_flags = if (debug_ubsan) .{ .extra_ldflags = &.{
            "${SRCDIR}/../../libs/libubsan_rt.a",
        } } else null,
    });

    // A static archive is linked later by cgo, so Zig does not get a final
    // executable link at which to add compiler-rt. Some targets (notably
    // x86_64) call helpers such as __zig_probe_stack from otherwise ordinary
    // ReleaseSafe code; bundle those helpers into the binding archive so every
    // cross target is self-contained.
    bindings.lib.bundle_compiler_rt = true;

    const steps = bindings.addStandardSteps(b, .{});
    if (ubsan_rt) |lib| {
        const install_ubsan = b.addInstallArtifact(lib, .{
            .dest_dir = .{ .override = libs_dir },
        });
        b.getInstallStep().dependOn(&install_ubsan.step);
        // The binding library is useless to cgo without the runtime beside it,
        // so whatever step produces one produces the other. That includes
        // generating, which builds the library to read its link inputs and
        // would otherwise leave `libs/` one archive short.
        steps.library.dependOn(&install_ubsan.step);
        steps.update.dependOn(&install_ubsan.step);
        steps.verify.dependOn(&install_ubsan.step);
    }
}
