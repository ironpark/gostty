//! Frame snapshots, Kitty placements and binary snapshot restoration.
const p = @import("policy.zig");
const zigo = p.zigo;
const api = p.api;
const flags = p.flags;
const enumeration = p.enumeration;
const out = p.out;
const outCodepoints = p.outCodepoints;
const bytesArg = p.bytesArg;

const Snapshot = api.handle("Snapshot", .{}).context();

const SnapshotDecoder = api.handle("SnapshotDecoder", .{}).context();

const RenderState = api.handle("RenderState", .{ .fields = &.{
    .{ .path = "rows", .doc = "The number of rows the last update covered." },
    .{ .path = "cols", .doc = "The number of columns the last update covered." },
    .{ .path = "cursor.visible", .name = "cursorVisible" },
    .{ .path = "cursor.visual_style", .name = "cursorStyle" },
    .{ .path = "cursor.blinking", .name = "cursorBlinking" },
    .{ .path = "cursor.password_input", .name = "cursorPasswordInput" },
} }).documented(
    \\RenderState owns a render snapshot. Update requires exclusive access to both
    \\the terminal and this state. After Update, reads do not touch the terminal,
    \\but must be serialized with Update, Clean and Close on this state. Copied
    \\cell, cursor and color values can be retained across updates. Call Clean
    \\only after successfully drawing the frame.
).context();

const KittyImages = api.handle("KittyImages", .{ .fields = &.{
    .{ .path = "generation" },
} }).context();

const snapshot_group = Snapshot.define(&.{
    // `decodeSnapshot` reads the serialized form off an `io.Reader`, so there
    // is no buffer here to call text or bytes.
    api.func("decodeSnapshot", .{
        .name = "DecodeSnapshot",
        .role = .{ .constructor = .{ .type = Snapshot.typeRef() } },
    }),
    Snapshot.func("deinit", .{ .role = .{ .destructor = Snapshot.typeRef() } }),
    api.func("snapshotRestoreInto", .{ .name = "restoreInto" }),
    // The bytes a stream had half-parsed when the snapshot was taken: a
    // fragment of a VT sequence, which is not text.
    api.func("snapshotContinuation", .{ .name = "continuation", .returns = .{ .semantic = .opaque_bytes } }),
});

// The incremental form: `ready` hands over a drawable terminal, then `next`
// prepends the scrollback a page at a time.
const snapshot_decoder_group = SnapshotDecoder.define(&.{
    api.func("newSnapshotDecoder", .{
        .role = .{ .constructor = .{ .type = SnapshotDecoder.typeRef() } },
        // The serialized snapshot format, not text.
        .params = &.{bytesArg(1)},
    }),
    api.func("freeSnapshotDecoder", .{ .role = .{ .destructor = SnapshotDecoder.typeRef() } }),
    api.func("snapshotDecoderReady", .{ .name = "ready" }),
    api.func("snapshotDecoderRestoreInto", .{ .name = "restoreInto" }),
    api.func("snapshotDecoderContinuation", .{ .name = "continuation", .returns = .{ .semantic = .opaque_bytes } }),
    api.func("snapshotDecoderNext", .{ .name = "next" }),
});

// `RenderState` is ghostty's own renderer-facing snapshot; a frame is one
// `update` plus one `cells` crossing.
const render_state_group = RenderState.define(&.{
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
    api.func("renderCursor", .{ .name = "cursor" }),
    api.func("renderColors", .{ .name = "colors" }),
    api.func("renderCellCount", .{ .name = "cellCount" }),
    api.func("renderCells", .{ .name = "cells", .params = &.{out(1)} }),
    api.func("renderBackground", .{ .name = "background" }),
    api.func("renderForeground", .{ .name = "foreground" }),
    api.func("renderCursorX", .{ .name = "cursorX" }),
    api.func("renderCursorY", .{ .name = "cursorY" }),
    api.func("renderCursorWideTail", .{ .name = "cursorWideTail" }),
    api.func("renderCursorColor", .{ .name = "cursorColor" }),
    // Partial redraw: which rows changed, one row's cells, and marking them
    // drawn.
    api.func("renderDirty", .{ .name = "dirty" }),
    api.func("renderDirtyRows", .{ .name = "dirtyRows", .params = &.{out(1)} }),
    api.func("renderRowCells", .{ .name = "rowCells", .params = &.{out(2)} }),
    api.func("renderGraphemes", .{ .name = "graphemes", .params = &.{outCodepoints(3)} }),
    api.func("renderHyperlinkAt", .{
        .name = "hyperlinkAt",
        .returns = zigo.result.owned(),
        .covers = &.{RenderState.ref("linkCells")},
    }),
});

// The same kind of snapshot as `RenderState`, for the images rather than the
// cells: one `update` and one `placements` crossing per frame, with the pixels
// fetched by id only when their generation says they changed.
const kitty_images_group = KittyImages.define(&.{
    api.func("newKittyImages", .{ .role = .{ .constructor = .{ .type = KittyImages.typeRef() } } }),
    api.func("freeKittyImages", .{ .role = .{ .destructor = KittyImages.typeRef() } }),
    api.func("kittyUpdate", .{ .name = "update" }),
    api.func("kittyPlacementCount", .{ .name = "placementCount" }),
    api.func("kittyPlacements", .{ .name = "placements", .params = &.{out(1)} }),
});

pub const declarations = [_]zigo.Entry{
    snapshot_group,
    snapshot_decoder_group,
    render_state_group,
    kitty_images_group,
    api.val("SnapshotProgress", .{}),
    api.val("RenderCursor", .{}),
    api.val("RenderColors", .{}),
    api.val("RenderCell", .{ .fields = &.{.{ .name = "codepoint", .semantic = .codepoint }} }),
    flags("CellFlags", "_pad"),
    enumeration("CellWidth", .{}),
    enumeration("RenderDirty", .{}),
    enumeration("KittyLayer", .{ .text = true }),
    api.val("KittyPlacement", .{}),
    api.val("KittyImage", .{}),
    enumeration("KittyFormat", .{}),
    enumeration("KittyCompression", .{}),
};
