//! The libghostty-vt surface exposed to Go.
//!
//! `Terminal` is ghostty's own type, bound directly: its methods become Go
//! methods without any wrapper. This module only adds what ghostty does not
//! provide and zigo needs:
//!
//!   - an `std.Io` value for `.io` injection (ghostty ships `TinyIo` as a type,
//!     not as a ready-made `Io` declaration);
//!   - a constructor and destructor, because `Terminal.init` takes a
//!     `Terminal.Options` whose `Colors` field holds optionals and so cannot
//!     cross the C ABI, and because it returns by value;
//!   - accessors for fields, which zigo does not bind;
//!   - a release function for the string `plainString` hands out.
//!
//! Every `gpa` / `io_impl` parameter below is filled in by zigo's `.allocator`
//! and `.io` injection and never appears in the C or Go signatures.
const std = @import("std");
const vt = @import("ghostty_vt");

const Allocator = std.mem.Allocator;

/// The `std.Io` injected into every bound call. TinyIo is ghostty's own
/// blocking implementation, recommended for embedders.
pub const io: std.Io = (vt.TinyIo.init).io();

pub const Terminal = vt.Terminal;

/// The visual style of the cursor.
pub const CursorStyle = vt.CursorStyle;

/// The DECSCUSR cursor style request.
///
/// Not taken from `vt.CursorStyleReq`: that alias points at
/// `terminal.CursorStyle` (the 4-tag screen cursor style) rather than
/// `terminal.CursorStyleReq`, so the 7-tag request enum is unreachable by name
/// from libghostty-vt's public API. Reported upstream; read it off the method
/// signature until that is fixed.
pub const CursorStyleReq = @typeInfo(@TypeOf(Terminal.setCursorStyle)).@"fn".params[1].type.?;

/// A VT stream: parses escape sequences and applies them to a `Terminal`.
///
/// A stream borrows its terminal and must be closed before it: ghostty's
/// `Handler.deinit` reaches through the terminal for its allocator. The binding
/// declares the stream a child of its terminal, so closing the terminal first is
/// refused rather than left to the caller to get right.
pub const Stream = vt.TerminalStream;

pub const ProtectedMode = vt.ProtectedMode;

pub const Charset = vt.Charset;
pub const CharsetSlot = vt.CharsetSlot;
pub const CharsetActiveSlot = vt.CharsetActiveSlot;
pub const DeccolmMode = Terminal.DeccolmMode;
pub const ScrollViewport = Terminal.ScrollViewport;
pub const EraseDisplay = vt.EraseDisplay;
pub const EraseLine = vt.EraseLine;
pub const TabClear = vt.TabClear;

/// Change the viewport size.
///
/// Wrapped because `vt.Terminal.Resize` carries a nested optional struct for
/// the cell size in pixels, which has no C representation.
pub fn resize(self: *Terminal, gpa: Allocator, width: u16, height: u16) !void {
    try self.resize(gpa, .{ .cols = width, .rows = height });
}

/// Which of a terminal's screens is active.
pub const ScreenKey = vt.ScreenSet.Key;

/// Switch between the primary and alternate screens.
///
/// Wrapped because ghostty returns the screen being left, and a handle borrowed
/// from its receiver has no representation in zigo -- only tagged-union
/// projections produce one.
pub fn switchScreen(self: *Terminal, key: ScreenKey) !void {
    _ = try self.switchScreen(key);
}

/// Which screen is currently active.
pub fn activeScreenKey(self: *Terminal) ScreenKey {
    return self.screens.active_key;
}

/// Write the cursor's current SGR attributes into `dst` as a DECRPSS response
/// body, and report how many bytes were written.
///
/// Wrapped because ghostty returns a slice into the caller's buffer, and zigo
/// reports a written count instead.
pub fn printAttributesInto(self: *Terminal, dst: []u8) !usize {
    const written = try self.printAttributes(dst);
    return written.len;
}

/// The scrollback contents, oldest row first, newline separated.
///
/// Wrapped because the region is chosen with `point.Point`, a tagged union
/// carrying a coordinate, which zigo cannot take by value.
pub fn historyString(self: *Terminal, gpa: Allocator) ![]const u8 {
    return try self.screens.active.dumpStringAlloc(gpa, .{ .history = .{} });
}

/// The full screen: scrollback followed by the active area.
pub fn screenString(self: *Terminal, gpa: Allocator) ![]const u8 {
    return try self.screens.active.dumpStringAlloc(gpa, .{ .screen = .{} });
}

/// The display width of a grapheme cluster given as codepoints.
///
/// Wrapped because `vt.unicode.graphemeWidth` is generic over the codepoint
/// integer type, and a generic function has no signature to bind.
pub fn graphemeWidth(cps: []const u32) u8 {
    return @intCast(vt.unicode.graphemeWidth(u32, cps).width);
}

/// Unicode helpers, re-exported as-is.
pub const unicode = vt.unicode;

/// Input encoding: turning key, mouse and focus events into the bytes a
/// program reading the pty expects.
pub const input = vt.input;

pub const Key = vt.input.Key;
pub const KeyAction = vt.input.KeyAction;
pub const FocusEvent = vt.input.FocusEvent;

/// A single keyboard modifier.
///
/// ghostty stores modifiers as a `packed struct(u16)`, which has no C
/// representation, so they are set one at a time instead of passed as a mask.
pub const KeyMod = enum(u8) {
    shift,
    ctrl,
    alt,
    super,
    caps_lock,
    num_lock,
};

/// A key event being assembled for encoding.
///
/// ghostty's `KeyEvent` holds a borrowed `utf8` slice, so a Go caller could not
/// keep one alive across calls. This owns the text instead, which is also the
/// shape ghostty's own C API uses.
pub const KeyEvent = struct {
    /// Long enough for any single key event's text: a grapheme cluster with
    /// combining marks, not arbitrary input.
    const utf8_capacity = 64;

    inner: vt.input.KeyEvent = .{},
    utf8_buf: [utf8_capacity]u8 = undefined,

    fn applyMod(mods: *vt.input.KeyMods, mod: KeyMod, value: bool) void {
        switch (mod) {
            .shift => mods.shift = value,
            .ctrl => mods.ctrl = value,
            .alt => mods.alt = value,
            .super => mods.super = value,
            .caps_lock => mods.caps_lock = value,
            .num_lock => mods.num_lock = value,
        }
    }

    /// Return the event to its defaults so one handle can encode many keys.
    pub fn reset(self: *KeyEvent) void {
        self.inner = .{};
    }

    pub fn setAction(self: *KeyEvent, action: KeyAction) void {
        self.inner.action = action;
    }

    pub fn setKey(self: *KeyEvent, key: Key) void {
        self.inner.key = key;
    }

    pub fn setMod(self: *KeyEvent, mod: KeyMod, value: bool) void {
        applyMod(&self.inner.mods, mod, value);
    }

    /// Mark a modifier as consumed producing the event text. Effective
    /// modifiers are the set modifiers minus the consumed ones.
    pub fn setConsumedMod(self: *KeyEvent, mod: KeyMod, value: bool) void {
        applyMod(&self.inner.consumed_mods, mod, value);
    }

    /// True while the event is part of an unfinished dead-key composition.
    pub fn setComposing(self: *KeyEvent, composing: bool) void {
        self.inner.composing = composing;
    }

    /// The text this key produced, if any. Copied into the event.
    pub fn setUtf8(self: *KeyEvent, text: []const u8) error{NoSpaceLeft}!void {
        if (text.len > utf8_capacity) return error.NoSpaceLeft;
        @memcpy(self.utf8_buf[0..text.len], text);
        self.inner.utf8 = self.utf8_buf[0..text.len];
    }

    /// The codepoint this key produces unshifted, or zero for none.
    pub fn setUnshiftedCodepoint(self: *KeyEvent, cp: u32) void {
        self.inner.unshifted_codepoint = @intCast(cp);
    }
};

pub fn newKeyEvent(gpa: Allocator) !*KeyEvent {
    const self = try gpa.create(KeyEvent);
    self.* = .{};
    return self;
}

pub fn freeKeyEvent(self: *KeyEvent, gpa: Allocator) void {
    gpa.destroy(self);
}

/// Encode a key event for `terminal`, whose modes decide the encoding.
pub fn encodeKey(
    writer: *std.Io.Writer,
    terminal: *const Terminal,
    event: *const KeyEvent,
) !void {
    try vt.input.encodeKey(writer, event.inner, .fromTerminal(terminal));
}

pub const MouseAction = vt.input.MouseAction;
pub const MouseButton = vt.input.MouseButton;

/// The renderer geometry mouse encoding needs to turn a pixel position into a
/// grid cell. All values are in already-DPI-scaled pixels.
///
/// ghostty's own `renderer.Size` nests three extern structs inside a plain one,
/// which has no C representation, so this is the flattened equivalent.
pub const RenderSize = extern struct {
    /// The size of the area the grid is drawn into, padding included.
    screen_width: u32,
    screen_height: u32,
    /// The size of one cell.
    cell_width: u32,
    cell_height: u32,
    padding_top: u32 = 0,
    padding_bottom: u32 = 0,
    padding_right: u32 = 0,
    padding_left: u32 = 0,

    const Size = @FieldType(vt.input.MouseEncodeOptions, "size");

    fn toRenderer(self: RenderSize) Size {
        return .{
            .screen = .{ .width = self.screen_width, .height = self.screen_height },
            .cell = .{ .width = self.cell_width, .height = self.cell_height },
            .padding = .{
                .top = self.padding_top,
                .bottom = self.padding_bottom,
                .right = self.padding_right,
                .left = self.padding_left,
            },
        };
    }
};

/// A mouse event being assembled for encoding.
///
/// A handle rather than a value because ghostty's event carries an optional
/// button and packed modifiers, neither of which crosses as a struct field.
pub const MouseEvent = struct {
    inner: vt.input.MouseEncodeEvent = .{},

    /// Return the event to its defaults so one handle can encode many events.
    pub fn reset(self: *MouseEvent) void {
        self.inner = .{};
    }

    pub fn setAction(self: *MouseEvent, action: MouseAction) void {
        self.inner.action = action;
    }

    /// The button involved. Motion with no button held uses `clearButton`.
    pub fn setButton(self: *MouseEvent, button: MouseButton) void {
        self.inner.button = button;
    }

    pub fn clearButton(self: *MouseEvent) void {
        self.inner.button = null;
    }

    pub fn setMod(self: *MouseEvent, mod: KeyMod, value: bool) void {
        KeyEvent.applyMod(&self.inner.mods, mod, value);
    }

    /// The position in surface-space pixels, (0, 0) at the top left.
    pub fn setPosition(self: *MouseEvent, x: f32, y: f32) void {
        self.inner.pos = .{ .x = x, .y = y };
    }
};

pub fn newMouseEvent(gpa: Allocator) !*MouseEvent {
    const self = try gpa.create(MouseEvent);
    self.* = .{};
    return self;
}

pub fn freeMouseEvent(self: *MouseEvent, gpa: Allocator) void {
    gpa.destroy(self);
}

/// Encode a mouse event for `terminal`, whose reporting mode and format decide
/// whether anything is written at all.
///
/// `any_button_pressed` should include this event, so a press reports true.
pub fn encodeMouse(
    writer: *std.Io.Writer,
    terminal: *const Terminal,
    event: *const MouseEvent,
    size: RenderSize,
    any_button_pressed: bool,
) !void {
    var opts: vt.input.MouseEncodeOptions = .fromTerminal(terminal, size.toRenderer());
    opts.any_button_pressed = any_button_pressed;
    try vt.input.encodeMouse(writer, event.inner, opts);
}

/// Encode a focus in/out report (CSI I / CSI O).
pub fn encodeFocus(writer: *std.Io.Writer, event: FocusEvent) !void {
    try vt.input.encodeFocus(writer, event);
}

/// True if `data` can be pasted without the receiving program seeing it as
/// something other than literal text.
pub fn isSafePaste(data: []const u8) bool {
    return vt.input.isSafePaste(data);
}

/// Encode `data` for pasting into `terminal`, respecting bracketed paste mode.
pub fn encodePaste(
    writer: *std.Io.Writer,
    terminal: *const Terminal,
    data: []const u8,
) !void {
    try vt.input.encodePasteWriter(writer, data, .fromTerminal(terminal));
}

/// Create a terminal with the given viewport size.
pub fn newTerminal(gpa: Allocator, io_impl: std.Io, width: u16, height: u16) !*Terminal {
    const self = try gpa.create(Terminal);
    errdefer gpa.destroy(self);
    self.* = try .init(io_impl, gpa, .{ .cols = width, .rows = height });
    return self;
}

/// Destroys a terminal created by `newTerminal`.
pub fn freeTerminal(self: *Terminal, gpa: Allocator) void {
    self.deinit(gpa);
    gpa.destroy(self);
}

/// Create a VT stream that applies escape sequences to `terminal`.
///
/// `continuation_max_bytes` caps the unfinished-sequence suffix the stream
/// tracks across feeds; zero disables tracking.
pub fn newStream(gpa: Allocator, terminal: *Terminal, continuation_max_bytes: usize) !*Stream {
    const self = try gpa.create(Stream);
    errdefer gpa.destroy(self);
    self.* = .init(.{
        .allocator = gpa,
        .handler = .init(terminal),
        .continuation_max_bytes = continuation_max_bytes,
    });
    return self;
}

/// Destroys a stream created by `newStream`.
pub fn freeStream(self: *Stream, gpa: Allocator) void {
    self.deinit();
    gpa.destroy(self);
}

/// True once a sequence failed in a way the terminal could not absorb, such as
/// an allocation failure. Streams are best-effort and keep going regardless.
pub fn streamFailed(self: *Stream) bool {
    return self.handler.semantic_failure;
}

/// Releases a string handed out by `plainString`.
pub fn freeString(gpa: Allocator, str: []const u8) void {
    gpa.free(str);
}

pub fn cols(self: *Terminal) u16 {
    return self.cols;
}

pub fn rows(self: *Terminal) u16 {
    return self.rows;
}

pub fn cursorX(self: *Terminal) u16 {
    return self.screens.active.cursor.x;
}

pub fn cursorY(self: *Terminal) u16 {
    return self.screens.active.cursor.y;
}

pub fn cursorStyle(self: *Terminal) CursorStyle {
    return self.screens.active.cursor.cursor_style;
}
