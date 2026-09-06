//! Configuration of a terminal from the embedder's side.
//!
//! Everything here is state a program on the pty can also change through an
//! escape sequence, but that the embedder owns first: the palette a program
//! sees before it sends any OSC 4, the mode values a `fullReset` returns to,
//! where the tabstops start out, and which Kitty graphics transmission mediums
//! are allowed at all.
//!
//! ghostty exposes all of this, but never as a method on `Terminal` taking C
//! ABI scalars, so each one is wrapped here.
const std = @import("std");
const vt = @import("ghostty_vt");

const Allocator = std.mem.Allocator;
const Terminal = vt.Terminal;
const Mode = vt.Mode;

/// Unpack a `0xRRGGBB` color, the convention every color parameter here uses.
fn rgbFromInt(v: u32) vt.color.RGB {
    return .{
        .r = @truncate(v >> 16),
        .g = @truncate(v >> 8),
        .b = @truncate(v),
    };
}

fn intFromRgb(c: vt.color.RGB) u32 {
    return (@as(u32, c.r) << 16) | (@as(u32, c.g) << 8) | c.b;
}

/// Read one entry of the 256 color palette as `0xRRGGBB`.
pub fn paletteColor(self: *Terminal, idx: u8) u32 {
    return intFromRgb(self.colors.palette.current[idx]);
}

/// Override one palette entry, as OSC 4 does.
pub fn setPaletteColor(self: *Terminal, idx: u8, rgb: u32) void {
    self.colors.palette.set(idx, rgbFromInt(rgb));
}

/// Drop the override on one palette entry, restoring its default.
pub fn resetPaletteColor(self: *Terminal, idx: u8) void {
    self.colors.palette.reset(idx);
}

/// Drop every palette override.
pub fn resetPalette(self: *Terminal) void {
    self.colors.palette.resetAll();
}

/// Change what one palette entry resets *to*, which is the embedder's
/// configured theme rather than anything a program asked for.
///
/// An entry a program has already overridden keeps that override; it is the
/// value `resetPaletteColor` will later restore that moves. ghostty only
/// offers this a whole palette at a time, so the single entry is edited into a
/// copy of the current defaults.
pub fn setDefaultPaletteColor(self: *Terminal, gpa: Allocator, idx: u8, rgb: u32) !void {
    var def = self.colors.palette.original.*;
    def[idx] = rgbFromInt(rgb);
    try self.colors.palette.changeDefault(gpa, def);
}

/// Restore the built-in xterm palette as the default, preserving overrides.
pub fn resetDefaultPalette(self: *Terminal, gpa: Allocator) void {
    self.colors.palette.resetDefault(gpa);
}

/// Set a mode and make that value the one `resetModes` and `fullReset` return
/// to.
pub fn setDefaultMode(self: *Terminal, mode: Mode, value: bool) void {
    self.modes.setDefault(mode, value);
}

/// Return every mode to its default and discard the XTSAVE slots.
pub fn resetModes(self: *Terminal) void {
    self.modes.reset();
}

/// Save a mode's current value, as XTSAVE (CSI ? Pm s) does.
///
/// There is one slot per mode, so saving twice without restoring loses the
/// first value.
pub fn saveMode(self: *Terminal, mode: Mode) void {
    self.modes.save(mode);
}

/// Restore a mode from its XTSAVE slot and report the value restored.
///
/// A mode that was never saved restores to false, which is the slot's initial
/// state rather than the mode's default.
pub fn restoreMode(self: *Terminal, mode: Mode) bool {
    return self.modes.restore(mode);
}

/// Put a tabstop at an absolute column.
///
/// Unlike `tabSet`, which is HTS and acts on the cursor's column, this does not
/// move or read the cursor.
pub fn setTabstop(self: *Terminal, col: usize) void {
    self.tabstops.set(col);
}

/// Remove the tabstop at an absolute column, if there is one.
pub fn unsetTabstop(self: *Terminal, col: usize) void {
    self.tabstops.unset(col);
}

/// Clear every tabstop and put one every `interval` columns.
///
/// An interval of zero just clears them all. The terminal's own default is 8.
pub fn resetTabstops(self: *Terminal, interval: usize) void {
    self.tabstops.reset(interval);
}

/// Choose which mediums a program may transmit a Kitty image over.
///
/// The default is direct transmission only: the pixels arrive base64-encoded in
/// the escape sequence itself. The other three mediums make the terminal read
/// something the program names -- an arbitrary path, a path under `temp_dir`,
/// or a POSIX shared memory object -- so each is a decision about how much a
/// program on the pty is trusted with the embedder's filesystem. An empty
/// `temp_dir` disables the temporary file medium.
///
/// ghostty borrows `temp_dir` rather than copying it, so a copy is made here
/// and the previous one released. The last copy is released by nothing: it
/// lives as long as the terminal, and ghostty's `deinit` does not know it is
/// owned.
pub fn setKittyGraphicsLoadingLimits(
    self: *Terminal,
    gpa: Allocator,
    file: bool,
    temp_dir: []const u8,
    shared_memory: bool,
) !void {
    const previous = self.screens.active.kitty_images.image_limits;
    const owned: []const u8 = if (temp_dir.len > 0) try gpa.dupe(u8, temp_dir) else "";
    errdefer if (owned.len > 0) gpa.free(owned);

    self.setKittyGraphicsLoadingLimits(.{
        .file = file,
        .temporary_file = if (owned.len > 0)
            .{ .enabled = .{ .directory = owned } }
        else
            .disabled,
        .shared_memory = shared_memory,
    });

    switch (previous.temporary_file) {
        .enabled => |v| gpa.free(v.directory),
        .disabled => {},
    }
}
