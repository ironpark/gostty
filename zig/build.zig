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
    gostty.addImport("terminal_options", ghostty_vt.import_table.get("terminal_options").?);
    const manifest = @import("build.zig.zon");
    const ghostty_url = manifest.dependencies.ghostty.url;
    const build_info_config = zigo.configJson(b, .{
        .ghostty_revision = ghostty_url[std.mem.lastIndexOfScalar(u8, ghostty_url, '#').? + 1 ..],
        .zigo_version = "local",
        .optimize = @tagName(optimize),
    });

    // Compile declarations and generators against the same public plugin contract.
    // Shared plugins come directly from the local zigo dependency.
    const zigo_dep = b.dependency("zigo", .{});
    const plugins = [_]zigo.PluginModule{
        .{ .name = "build_info", .root_source_file = b.path("plugins/build_info/src/plugin.zig"), .config = build_info_config },
        .{ .name = "convenience", .root_source_file = b.path("plugins/convenience/src/plugin.zig") },
        .{ .name = "stringer", .root_source_file = b.path("plugins/stringer/src/plugin.zig") },
        .{ .name = "must", .root_source_file = b.path("plugins/must/src/plugin.zig") },
        .{ .name = "satisfies", .root_source_file = zigo_dep.path("plugins/satisfies/src/plugin.zig") },
        .{ .name = "enumkit", .root_source_file = zigo_dep.path("plugins/enumkit/src/plugin.zig") },
    };

    const bindings = zigo.addGoBindings(b, .{
        .name = "gostty",
        .module = gostty,
        .bindings = b.path("src/bindings.zig"),
        .plugins = &plugins,
        // Parameter names and doc comments are read from source. Naming the
        // root here lets zigo walk the imported module graph too, so ghostty's
        // own declarations arrive with their real parameter names rather than
        // `p0`.
        .source_root = b.path("src/root.zig"),
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
    // executable link at which to add its runtimes. Bundle them into the
    // binding archive, as ghostty does for its own libghostty-vt
    // (`src/build/GhosttyLibVt.zig`), so every platform is self-contained:
    //
    // - compiler_rt: some targets (notably x86_64) call helpers such as
    //   __zig_probe_stack from otherwise ordinary ReleaseSafe code.
    // - ubsan_rt: a Debug build compiles ghostty's vendored C/C++ with full
    //   UBSan, whose `__ubsan_handle_*` handlers live here. Only in Debug:
    //   the release modes reference none of them, and Zig bundles the whole
    //   runtime whether or not anything does, so it would be three megabytes
    //   of dead weight in every release archive.
    //
    // ghostty skips ubsan_rt on Windows because the MSVC linker rejects the
    // directives it carries; cgo links these archives with mingw's ld or
    // lld, which accept them, so Windows is bundled like the rest.
    for (bindings.native_libraries) |native| {
        native.lib.bundle_compiler_rt = true;
        native.lib.bundle_ubsan_rt = optimize == .Debug;
    }
}
