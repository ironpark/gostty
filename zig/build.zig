const std = @import("std");
const zigo = @import("zigo");

// Where the native libraries and the C header are installed. The install
// prefix is the repository root (see the Makefile), so this is `libs/` beside
// the Go package rather than a `zig-out` buried under the Zig tree: the
// libraries are what the Go package embeds, so they belong somewhere a Go
// developer would look.
const libs_dir: std.Build.InstallDir = .{ .custom = "libs" };

/// One platform the binding library ships for. The Go names are the `libs/`
/// subdirectory the platform's library installs under.
const Platform = struct {
    triple: []const u8,
    goos: []const u8,
    goarch: []const u8,
};

// The release matrix. Every platform is built from the same source tree in
// one `zig build go`, so the committed libraries always come from the same
// run and never depend on the host that ran the generator. Keep the order
// stable so the generated files do not churn.
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

    // Zig 0.17's clang 22 no longer knows the `evex512` target feature, and
    // the Highway ghostty vendors still spells it into the target string of
    // every AVX-512 and AVX10 target whenever clang is 18 or newer. The
    // attribute is then ignored and ghostty's SIMD sources fail to compile
    // for x86_64. Leave those targets out of Highway's dynamic dispatch, so
    // x86_64 tops out at AVX2, until the vendored Highway catches up. The
    // macro is set on the module that compiles the SIMD sources, so it
    // survives zigo's clone for every platform; it means nothing off x86.
    ghostty_vt.addCMacro(
        "HWY_DISABLED_TARGETS",
        "(HWY_AVX3|HWY_AVX3_DL|HWY_AVX3_ZEN4|HWY_AVX3_SPR|HWY_AVX10_2|HWY_AVX10_2_512)",
    );

    const gostty = b.addModule("gostty", .{
        .root_source_file = b.path("src/root.zig"),
        .target = target,
        .optimize = optimize,
    });
    gostty.addImport("ghostty_vt", ghostty_vt);
    gostty.addImport("terminal_options", ghostty_vt.import_table.get("terminal_options").?);
    const manifest = @import("build.zig.zon");
    const ghostty_url = manifest.dependencies.ghostty.url;
    // The release tarball is named for its tag, so the version the generator
    // ran at comes from the manifest rather than a second constant that has to
    // be remembered on every bump. Read from the URL rather than the package
    // hash: the hash spells `<name>-<version>-<digest>`, but the digest is
    // base64url and carries `-` of its own, so neither end of it can be found
    // by scanning for one.
    const zigo_url = manifest.dependencies.zigo.url;
    const zigo_version = std.fs.path.stem(std.fs.path.stem(std.fs.path.basename(zigo_url)));
    const build_info_config = zigo.configJson(b, .{
        .ghostty_revision = ghostty_url[std.mem.lastIndexOfScalar(u8, ghostty_url, '#').? + 1 ..],
        .zigo_version = zigo_version,
        // Spelled the way `-Doptimize` takes it. Zig 0.17 renamed the enum
        // tags (`safe` for `ReleaseSafe`), but the published value should not
        // move with the toolchain.
        .optimize = switch (optimize) {
            .debug => "Debug",
            .safe => "ReleaseSafe",
            .fast => "ReleaseFast",
            .small => "ReleaseSmall",
        },
    });

    // Compile declarations and generators against the same public plugin contract.
    // Shared plugins come directly from the local zigo dependency.
    const zigo_dep = b.dependency("zigo", .{});
    const plugins = [_]zigo.PluginModule{
        // First, so every plugin that reads a public Go name sees the final
        // spelling.
        .{ .name = "spelling", .root_source_file = b.path("plugins/spelling/src/plugin.zig") },
        .{ .name = "build_info", .root_source_file = b.path("plugins/build_info/src/plugin.zig"), .config = build_info_config },
        .{ .name = "convenience", .root_source_file = b.path("plugins/convenience/src/plugin.zig") },
        .{ .name = "stringer", .root_source_file = b.path("plugins/stringer/src/plugin.zig") },
        .{ .name = "trim", .root_source_file = b.path("plugins/trim/src/plugin.zig") },
        .{ .name = "must", .root_source_file = b.path("plugins/must/src/plugin.zig"), .config = zigo.configJson(b, .{ .replace_field_getters = false }) },
        .{ .name = "satisfies", .root_source_file = zigo_dep.path("plugins/satisfies/src/plugin.zig") },
        .{ .name = "enumkit", .root_source_file = zigo_dep.path("plugins/enumkit/src/plugin.zig") },
    };

    const plugin_tests = b.step("test-plugins", "Test binding plugin policies");
    for (plugins) |entry| {
        const module = b.createModule(.{
            .root_source_file = entry.root_source_file.?,
            .target = b.graph.host,
            .optimize = optimize,
        });
        inline for (.{ "plugin", "semantic", "abi", "naming", "diagnostic", "targets" }) |name|
            module.addImport(name, zigo_dep.module(name));
        plugin_tests.dependOn(&b.addRunArtifact(b.addTest(.{ .root_module = module })).step);
    }

    // `bindings` is left at its default, `src/bindings.zig` beside the
    // module root, and `addStandardSteps` runs as part of this call, so the
    // `go`, `go-check`, `go-abi-check`, ... steps need no second line.
    const bindings = zigo.addGoBindings(b, .{
        .name = "gostty",
        .module = gostty,
        .plugins = &plugins,
        // Parameter names and doc comments are read from source. Naming the
        // root here lets zigo walk the imported module graph too, so ghostty's
        // own declarations arrive with their real parameter names rather than
        // `p0`.
        .source_root = b.path("src/root.zig"),
        .go_dir = b.path(".."),
        .layout = .{
            .go_module = "github.com/ironpark/gostty",
            .go_package = "gostty",
            // Publish at the module root, so the import path is the module
            // itself. The raw package keeps its default, `internal/raw`.
            .go_package_path = ".",
        },
        .target = target,
        .targets = resolved[1..],
        .optimize = optimize,
        // Called through purego, without cgo: the Go build needs no C
        // toolchain, and the shared library is loaded at run time. The
        // public package embeds it and loads it itself (embed_*.go), so the
        // loader stays explicit.
        .link = .{ .purego = .{} },
        // The libraries are installed together under the repository's
        // `libs`, one `<goos>_<goarch>` subdirectory per platform, which is
        // where the embed directives point.
        .install = .{
            .library_dir = libs_dir,
            .header_dir = .{ .custom = "libs/include" },
        },
    });

    // Bundle Zig's runtimes into the binding library, as ghostty does for its
    // own libghostty-vt (`src/build/GhosttyLibVt.zig`), so every platform's
    // library is self-contained:
    //
    // - compiler_rt: some targets (notably x86_64) call helpers such as
    //   __zig_probe_stack from otherwise ordinary ReleaseSafe code.
    // - ubsan_rt: a Debug build compiles ghostty's vendored C/C++ with full
    //   UBSan, whose `__ubsan_handle_*` handlers live here. Only in Debug:
    //   the release modes reference none of them, and Zig bundles the whole
    //   runtime whether or not anything does, so it would be three megabytes
    //   of dead weight in every release library.
    //
    // ghostty skips ubsan_rt on Windows because the MSVC linker rejects the
    // directives it carries; Zig links the DLL with lld, which accepts them,
    // so Windows is bundled like the rest.
    for (bindings.native_libraries) |native| {
        native.lib.bundle_compiler_rt = true;
        native.lib.bundle_ubsan_rt = optimize == .debug;
    }
}
