//! Kitty graphics, snapshotted per frame the way `RenderState` snapshots cells.
//!
//! The protocol's state lives on the active screen's `ImageStorage`: images by
//! id, and placements saying where each is drawn. A renderer does not want that
//! shape. It wants, per frame, a flat list of "draw this image, this part of it,
//! here, this big", in stacking order -- and it needs to know when an image's
//! pixels changed so it can keep its own textures. `update` resolves every
//! placement against the current viewport, drops what is off-screen, sorts by z
//! and hands the result over in one array; the pixels are fetched by id, keyed
//! on the generation stamp ghostty already maintains.
const std = @import("std");
const vt = @import("ghostty_vt");
const common = @import("common.zig");

const Allocator = std.mem.Allocator;
const Terminal = common.Terminal;
const Screen = common.Screen;
const io = common.io;
const packColor = common.packColor;
const unpackColor = common.unpackColor;
const Underline = common.Underline;

const kitty = vt.kitty.graphics;

/// How an image's bytes are laid out.
///
/// Ours rather than ghostty's: ghostty's has a backing type chosen for
/// compactness (three bits, at the time of writing), which cannot appear in an
/// extern struct. The tags and their order are the same, and the conversion is
/// a `switch`, so a tag added upstream is a compile error here rather than a
/// silent renumbering.
pub const KittyFormat = enum(u8) {
    /// Three bytes per pixel.
    rgb,
    /// Four bytes per pixel.
    rgba,
    /// A PNG file. Never seen by Go: ghostty refuses PNG transmissions until
    /// a decoder is installed with `onPngDecodeRequest`, and with one the
    /// image is decoded on arrival and stored as `rgba`. Kept so the tags
    /// mirror ghostty's.
    png,
    /// Two bytes per pixel. Only reachable by decoding a PNG.
    gray_alpha,
    /// One byte per pixel. Only reachable by decoding a PNG.
    gray,
};

/// Whether the image data is still compressed.
pub const KittyCompression = enum(u8) {
    none,
    zlib_deflate,
};

fn kittyFormat(f: @FieldType(kitty.Image, "format")) KittyFormat {
    return switch (f) {
        .rgb => .rgb,
        .rgba => .rgba,
        .png => .png,
        .gray_alpha => .gray_alpha,
        .gray => .gray,
    };
}

fn kittyCompression(c: @FieldType(kitty.Image, "compression")) KittyCompression {
    return switch (c) {
        .none => .none,
        .zlib_deflate => .zlib_deflate,
    };
}

/// One image drawn at one place, flattened for the C ABI.
///
/// Everything a renderer needs for one draw call, in the coordinates it works
/// in: cells for the position, pixels for the size. The image's own bytes are
/// not here -- see `kittyImage` and `kittyImageData`.
pub const KittyPlacement = extern struct {
    /// The image to draw. Look it up with `kittyImage`.
    image_id: u32,
    /// Which placement of that image this is. Together with `image_id` it
    /// identifies the placement for as long as it exists.
    placement_id: u32,

    /// Where the top-left corner goes, in viewport cells. The row is negative
    /// when the image has scrolled partly above the viewport, and either can be
    /// negative for a placement positioned relative to another one.
    viewport_col: i32,
    viewport_row: i32,
    /// Offset within that cell, in pixels.
    x_offset: u32,
    y_offset: u32,

    /// How big to draw it, in pixels. This is the source rectangle scaled to
    /// whatever the program asked for, so it is what the image should be
    /// stretched to rather than its natural size.
    pixel_width: u32,
    pixel_height: u32,
    /// The same size in cells, which is what the placement occupies on the
    /// grid. Useful for clipping; the pixel size is what to draw.
    grid_cols: u32,
    grid_rows: u32,

    /// The part of the image to draw, in image pixels. Already clamped to the
    /// image, and a zero-sized request already turned into the full dimension.
    source_x: u32,
    source_y: u32,
    source_width: u32,
    source_height: u32,

    /// Stacking order. The snapshot is sorted by it, so drawing the array in
    /// order is correct; it is here for the one decision it still leaves, which
    /// is whether a placement goes under the text (negative) or over it.
    z: i32,
};

/// A snapshot of where the images on the active screen are drawn.
///
/// Rebuilt by `kittyUpdate` and read out in one crossing, the same way
/// `RenderState` hands over cells.
pub const KittyImages = struct {
    gpa: Allocator,
    items: std.ArrayList(KittyPlacement) = .empty,
    /// The storage's generation stamp as of the last `kittyUpdate`.
    ///
    /// Bumped whenever an image or a placement is added, replaced or removed,
    /// and not by scrolling or resizing. An unchanged stamp means every
    /// image's bytes are the ones already fetched, so it is what a texture
    /// cache should be keyed on to decide whether to look at all.
    generation: u64 = 0,
};

pub fn newKittyImages(gpa: Allocator) !*KittyImages {
    const self = try gpa.create(KittyImages);
    self.* = .{ .gpa = gpa };
    return self;
}

pub fn freeKittyImages(self: *KittyImages, gpa: Allocator) void {
    self.items.deinit(gpa);
    gpa.destroy(self);
}

/// Rebuild the snapshot from `term`'s active screen.
///
/// Placements that cannot be drawn are left out: the ones scrolled off the
/// viewport, the ones whose text has been pruned out of the scrollback, and the
/// virtual (unicode placeholder) ones, which have no position of their own
/// because they are laid out by the cells that reference them.
pub fn kittyUpdate(self: *KittyImages, term: *Terminal) !void {
    self.items.clearRetainingCapacity();

    const storage = &term.screens.active.kitty_images;
    self.generation = storage.generation;

    var it = storage.placements.iterator();
    while (it.next()) |entry| {
        const key = entry.key_ptr;
        const placement = entry.value_ptr;
        const image = storage.images.getPtr(key.image_id) orelse continue;

        const pos = kittyViewportPos(storage, placement, image, term) orelse continue;

        const size = placement.pixelSize(image.*, term);
        const grid = placement.gridSize(image.*, term);
        const source = placement.sourceRect(image.*);

        try self.items.append(self.gpa, .{
            .image_id = key.image_id,
            .placement_id = key.placement_id.id,
            .viewport_col = pos.col,
            .viewport_row = pos.row,
            .x_offset = placement.x_offset,
            .y_offset = placement.y_offset,
            .pixel_width = size.width,
            .pixel_height = size.height,
            .grid_cols = grid.cols,
            .grid_rows = grid.rows,
            .source_x = source.x,
            .source_y = source.y,
            .source_width = source.width,
            .source_height = source.height,
            .z = placement.z,
        });
    }

    // Back to front. A hash map has no order at all, so without this the same
    // two overlapping images could swap places from one frame to the next; the
    // ids break the tie so equal z is stable too.
    std.mem.sort(KittyPlacement, self.items.items, {}, struct {
        fn lessThan(_: void, a: KittyPlacement, b: KittyPlacement) bool {
            if (a.z != b.z) return a.z < b.z;
            if (a.image_id != b.image_id) return a.image_id < b.image_id;
            return a.placement_id < b.placement_id;
        }
    }.lessThan);
}

/// Where a placement's top-left corner is, relative to the viewport, or absent
/// if it is not on screen at all.
///
/// A placement is anchored to a pin -- a tracked position in the scrollback --
/// rather than to a row number, so it follows its text as the screen scrolls.
/// Turning that back into a viewport row means asking the page list where both
/// the pin and the viewport's top-left corner are on the screen-absolute axis
/// and subtracting.
fn kittyViewportPos(
    storage: *const kitty.ImageStorage,
    placement: *const kitty.ImageStorage.Placement,
    image: *const kitty.Image,
    term: *Terminal,
) ?struct { col: i32, row: i32 } {
    // A placement positioned relative to another one has no pin of its own: it
    // hangs off its parent's, at an accumulated offset. A chain that ends at a
    // virtual placement has no resolvable position, since only a renderer
    // scanning the cells can find the placeholders it is anchored to.
    var col_offset: i32 = 0;
    var row_offset: i32 = 0;
    const pin = switch (placement.location) {
        .pin => |p| p,
        .virtual => return null,
        .relative => |rel| pin: {
            const chain = storage.resolveChain(rel) orelse return null;
            col_offset = chain.horizontal_offset;
            row_offset = chain.vertical_offset;
            break :pin switch (chain.root.location) {
                .pin => |p| p,
                .virtual, .relative => return null,
            };
        },
    };
    if (pin.garbage) return null;

    const pages = &term.screens.active.pages;
    const pin_point = pages.pointFromPin(.screen, pin.*) orelse return null;
    const top_left = pages.pointFromPin(.screen, pages.getTopLeft(.viewport)) orelse return null;

    const row: i32 = (@as(i32, @intCast(pin_point.screen.y)) -
        @as(i32, @intCast(top_left.screen.y))) +| row_offset;
    const col: i32 = @as(i32, @intCast(pin_point.screen.x)) +| col_offset;

    // Off the top, off the bottom, or pushed off either side by a relative
    // placement's offsets. A placement measured in cells has no size at all
    // until the terminal has been told how big a cell is, and something with no
    // size is off every edge at once, so it counts as one cell until then --
    // otherwise forgetting `resizeCells` would silently hide every image.
    const grid = placement.gridSize(image.*, term);
    const height: i64 = @max(grid.rows, 1);
    const width: i64 = @max(grid.cols, 1);
    if (@as(i64, row) + height <= 0 or row >= @as(i32, term.rows)) return null;
    if (@as(i64, col) + width <= 0 or col >= @as(i32, term.cols)) return null;

    return .{ .col = col, .row = row };
}

/// How many placements `kittyPlacements` will write.
pub fn kittyPlacementCount(self: *KittyImages) usize {
    return self.items.items.len;
}

/// Copy the placements into `dst`, back to front, and report how many.
pub fn kittyPlacements(self: *KittyImages, dst: []KittyPlacement) !usize {
    if (dst.len < self.items.items.len) return error.NoSpaceLeft;
    @memcpy(dst[0..self.items.items.len], self.items.items);
    return self.items.items.len;
}

/// What an image is, without its bytes.
pub const KittyImage = extern struct {
    /// Changes whenever the bytes behind `kittyImageData` change, including
    /// when an animation advances a frame. Cache textures on it.
    generation: u64,
    /// The length `kittyImageData` will write.
    data_len: u64,
    /// The image's own size in pixels, which is what `source_*` on a placement
    /// indexes into. Not the size it is drawn at.
    width: u32,
    height: u32,
    format: KittyFormat,
    compression: KittyCompression,
    _pad: u16 = 0,
};

/// Look up an image on the active screen by id.
pub fn kittyImage(term: *Terminal, image_id: u32) ?KittyImage {
    const image = term.screens.active.kitty_images.images.getPtr(image_id) orelse return null;
    return .{
        .generation = image.generation,
        .data_len = image.renderData().len(),
        .width = image.width,
        .height = image.height,
        .format = kittyFormat(image.format),
        .compression = kittyCompression(image.compression),
    };
}

/// Copy an image's pixels into `dst` and report how many bytes were written,
/// or zero if there is no such image.
///
/// The bytes are as the program transmitted them, decompressed: a PNG is still
/// a PNG, and it is the renderer that decodes it. For an animated image these
/// are the current frame's, which is why the generation stamp moves when the
/// frame does.
pub fn kittyImageData(term: *Terminal, image_id: u32, dst: []u8) !usize {
    const image = term.screens.active.kitty_images.images.getPtr(image_id) orelse return 0;
    const bytes = image.renderData().bytes() orelse return 0;
    if (dst.len < bytes.len) return error.NoSpaceLeft;
    @memcpy(dst[0..bytes.len], bytes);
    return bytes.len;
}
