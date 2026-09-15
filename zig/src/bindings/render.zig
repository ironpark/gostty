//! Frame snapshots, Kitty placements and binary snapshot restoration.
const p = @import("policy.zig");
const Terminal = @import("terminal.zig").Terminal;
const zigo = p.zigo;
const api = p.api;

const Snapshot = p.trimmed(api.handle("Snapshot", .{})).context();

const SnapshotDecoder = p.trimmed(api.handle("SnapshotDecoder", .{})).context();

const RenderState = p.trimmedAs(api.handle("RenderState", .{ .fields = &.{
    .{ .path = "rows", .doc = "The number of rows the last update covered." },
    .{ .path = "cols", .doc = "The number of columns the last update covered." },
    .{ .path = "cursor.visible", .name = "cursorVisible" },
    .{ .path = "cursor.visual_style", .name = "cursorStyle" },
    .{ .path = "cursor.blinking", .name = "cursorBlinking" },
    .{ .path = "cursor.password_input", .name = "cursorPasswordInput" },
} }).with(.{ .doc =
    \\RenderState owns a render snapshot. Update requires exclusive access to both
    \\the terminal and this state. After Update, reads do not touch the terminal,
    \\but must be serialized with Update, Clean and Close on this state. Copied
    \\cell, cursor and color values can be retained across updates. Call Clean
    \\only after successfully drawing the frame.
}), "render").context();

const KittyImages = p.trimmedAs(api.handle("KittyImages", .{ .fields = &.{
    .{ .path = "generation" },
} }), "kitty").context();

const snapshot_group = Snapshot.members(p.functions(api, .{
    // `decodeSnapshot` reads the serialized form off an `io.Reader`, so there
    // is no buffer here to call text or bytes.
    api.func("decodeSnapshot", .{
        .name = "DecodeSnapshot",
        .role = .{ .constructor = .{ .type = Snapshot.typeRef() } },
    }),
    Snapshot.func("deinit", .{ .role = .{ .destructor = Snapshot.typeRef() } }),
    "snapshotRestoreInto",
    // The second constructor for `Terminal`. 0.26.0 pairs each `.constructs`
    // claim with the type's one destructor, so a snapshot can hand back a
    // handle of its own instead of only filling one the caller already made.
    api.func("snapshotTerminal", .{
        .role = .{ .constructor = .{ .type = Terminal.typeRef(), .receiver = .member } },
    }),
    // The bytes a stream had half-parsed when the snapshot was taken: a
    // fragment of a VT sequence, which is not text.
    api.func("snapshotContinuation", .{ .returns = .{ .semantic = .opaque_bytes } }),
}));

// The incremental form: `ready` hands over a drawable terminal, then `next`
// prepends the scrollback a page at a time.
const snapshot_decoder_group = SnapshotDecoder.members(p.functions(api, .{
    api.func("newSnapshotDecoder", .{
        .role = .{ .constructor = .{ .type = SnapshotDecoder.typeRef() } },
        // The serialized snapshot format, not text.
        .params = &.{p.bytesArg(1)},
    }),
    api.func("freeSnapshotDecoder", .{ .role = .{ .destructor = SnapshotDecoder.typeRef() } }),
    "snapshotDecoderReady",
    "snapshotDecoderRestoreInto",
    api.func("snapshotDecoderContinuation", .{ .returns = .{ .semantic = .opaque_bytes } }),
    "snapshotDecoderNext",
}));

// `RenderState` is ghostty's own renderer-facing snapshot; a frame is one
// `update` plus one `cells` crossing.
const render_state_group = RenderState.members(p.functions(api, .{
    api.func("newRenderState", .{ .role = .{ .constructor = .{ .type = RenderState.typeRef() } } }),
    RenderState.func("deinit", .{ .role = .{ .destructor = RenderState.typeRef() } }),
    // ghostty's `update` wraps `beginUpdate`/`endUpdate`; the terminal
    // argument is named here because the reflector reads the wrapper's name
    // from the wrong side of that call.
    RenderState.func("update", .{
        .params = &.{.{ .index = 2, .go_name = "t" }},
        .covers = &.{ RenderState.ref("beginUpdate"), RenderState.ref("endUpdate") },
    }),
    RenderState.func("clean", .{}),
    "renderCursor",
    "renderColors",
    "renderCellCount",
    api.func("renderCells", .{ .params = &.{p.out(1)} }),
    // Partial redraw: which rows changed, one row's cells, and marking them
    // drawn.
    "renderDirty",
    api.func("renderDirtyRows", .{ .params = &.{p.out(1)} }),
    api.func("renderRowCells", .{ .params = &.{p.out(2)} }),
    api.func("renderGraphemes", .{ .params = &.{p.outCodepoints(3)} }),
    api.func("renderHyperlinkAt", .{
        .returns = zigo.result.owned(),
        .covers = &.{RenderState.ref("linkCells")},
    }),
}));

// The same kind of snapshot as `RenderState`, for the images rather than the
// cells: one `update` and one `placements` crossing per frame, with the pixels
// fetched by id only when their generation says they changed.
const kitty_images_group = KittyImages.members(p.functions(api, .{
    api.func("newKittyImages", .{ .role = .{ .constructor = .{ .type = KittyImages.typeRef() } } }),
    api.func("freeKittyImages", .{ .role = .{ .destructor = KittyImages.typeRef() } }),
    "kittyUpdate",
    "kittyPlacementCount",
    api.func("kittyPlacements", .{ .params = &.{p.out(1)} }),
}));

// The two ways a snapshot comes back. `Snapshot` restores the whole thing in
// one call; `SnapshotDecoder` hands over a drawable terminal first and prepends
// the scrollback a page at a time. Which one a program wants depends on how big
// the scrollback is and whether it can afford to block, but the two calls that
// finish the job are the same on both, so code that only restores need not know
// which it was given. `Next` and `Ready` stay off the interface: they are what
// makes the incremental one different.
const snapshot_source = zigo.interface(.{
    .name = "SnapshotSource",
    .methods = &.{ "restoreInto", "continuation" },
    .types = &.{ Snapshot.typeRef(), SnapshotDecoder.typeRef() },
    .doc =
    \\SnapshotSource is a decoded snapshot: either a Snapshot, restored in one
    \\call, or a SnapshotDecoder, which restores the screen first and the
    \\scrollback a page at a time. Close it when the restore is done.
    ,
});

pub const declarations = [_]zigo.Entry{
    snapshot_group,
    snapshot_decoder_group,
    snapshot_source,
    render_state_group,
    kitty_images_group,
    api.value("SnapshotProgress", .{}),
    api.value("RenderCursor", .{}),
    api.value("RenderColors", .{}),
    api.value("RenderCell", .{ .fields = &.{.{ .name = "codepoint", .semantic = .codepoint }} }),
    p.flags("CellFlags", "_pad"),
    p.enumeration("CellWidth", .{}),
    p.enumeration("RenderDirty", .{}),
    p.enumeration("KittyLayer", .{ .text = true }),
    api.value("KittyPlacement", .{}),
    api.value("KittyImage", .{}),
    p.enumeration("KittyFormat", .{}),
    p.enumeration("KittyCompression", .{}),
};
