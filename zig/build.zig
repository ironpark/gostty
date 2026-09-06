const std = @import("std");
const zigo = @import("zigo");

// Where the native archives and the C header are installed. The install prefix
// is the repository root (see the Makefile), so this is `libs/` beside the Go
// package rather than a `zig-out` buried under the Zig tree: the archives are
// what the Go build links, so they belong somewhere a Go developer would look.
const libs_dir: std.Build.InstallDir = .{ .custom = "libs" };

/// One platform the binding library ships for. The Go names are what zigo
/// writes into the `#cgo <goos>,<goarch>` constraint and the `libs/`
/// subdirectory the platform's archives install under.
const Platform = struct {
    triple: []const u8,
    goos: []const u8,
    goarch: []const u8,
};

// The release matrix. Every platform is built from the same source tree in
// one `zig build go`, so the generated link directives always describe all
// of them and never depend on the host that ran the generator. The order is
// the order of the `#cgo` lines in the generated file; keep it stable so the
// committed file does not churn.
//
// Linux comes first on purpose. zigo configures the ghostty dependency for
// the first platform and clones its module graph for the rest, and ghostty
// adds the Apple SDK include paths and libc++ macros to its vendored C++
// (simdutf, highway) whenever the platform it is configured for is Darwin.
// Those settings survive the clone and break every non-Darwin platform's
// compile. Configured for Linux, the graph carries nothing platform-specific
// and the macOS and Windows clones build with Zig's bundled headers.
const platforms = [_]Platform{
    .{ .triple = "aarch64-linux-gnu", .goos = "linux", .goarch = "arm64" },
    .{ .triple = "x86_64-linux-gnu", .goos = "linux", .goarch = "amd64" },
    .{ .triple = "aarch64-macos", .goos = "darwin", .goarch = "arm64" },
    .{ .triple = "x86_64-macos", .goos = "darwin", .goarch = "amd64" },
    .{ .triple = "aarch64-windows-gnu", .goos = "windows", .goarch = "arm64" },
    .{ .triple = "x86_64-windows-gnu", .goos = "windows", .goarch = "amd64" },
};

pub fn build(b: *std.Build) void {
    const optimize = b.standardOptimizeOption(.{});

    var resolved: [platforms.len]std.Build.ResolvedTarget = undefined;
    for (&resolved, platforms) |*slot, platform| {
        slot.* = b.resolveTargetQuery(std.Target.Query.parse(.{
            .arch_os_abi = platform.triple,
        }) catch @panic("invalid platform triple"));
    }
    // zigo builds `target` from the module as given and rebuilds the module
    // graph for every entry of `targets`, so the first platform is the one the
    // ghostty dependency is configured for.
    const target = resolved[0];

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
    // undefined `__ubsan_handle_*` symbols. Linking it into the module makes
    // it one of the module's static link inputs, which zigo installs beside
    // the binding archive for every platform and names on that platform's
    // cgo line after the archive, where ld needs it.
    //
    // ghostty guards the same problem for Windows only
    // (`src/build/SharedDeps.zig:986`); consuming the archive from cgo hits it
    // on every platform.
    //
    // Only in Debug: the release modes do not reference a single one of those
    // symbols, so shipping a three megabyte sanitizer runtime with them would
    // be dead weight on disk and a puzzle for whoever found it there.
    if (optimize == .Debug) {
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
        gostty.linkLibrary(ubsan_rt);
    }

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
        .targets = resolved[1..],
        .optimize = optimize,
        // The archives are installed together under the repository's `libs`,
        // one `<goos>_<goarch>` subdirectory per platform, which is where the
        // generated cgo directives point.
        .install = .{
            .library_dir = libs_dir,
            .header_dir = .{ .custom = "libs/include" },
        },
    });

    _ = bindings.addStandardSteps(b, .{});
    // A static archive is linked later by cgo, so Zig does not get a final
    // executable link at which to add compiler-rt. Some targets (notably
    // x86_64) call helpers such as __zig_probe_stack from otherwise ordinary
    // ReleaseSafe code, and ubsan_rt calls into it for soft-float conversions;
    // bundle those helpers into the binding archive so every platform is
    // self-contained.
    for (bindings.native_libraries) |native| native.lib.bundle_compiler_rt = true;
}
