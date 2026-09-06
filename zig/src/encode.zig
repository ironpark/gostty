//! Input encoding: key, mouse and paste events into the bytes a program
//! reading the pty expects.
//!
//! ghostty's own event types hold borrowed slices and modifiers with no C
//! representation, so the ones here are the flattened equivalents. The `Key`
//! helpers are free functions because `Key` is an enum, and zigo binds methods
//! on structs and opaque types only.
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

pub const Key = vt.input.Key;
/// What happened to the key. Declared here rather than re-exported so that
/// press is zero: `KeyEvent` is a value, and a Go literal that names only
/// the key should describe a press, which is what ghostty's own default is.
pub const KeyAction = enum(u8) {
    press,
    release,
    repeat,

    fn toGhostty(self: KeyAction) vt.input.KeyAction {
        return switch (self) {
            .press => .press,
            .release => .release,
            .repeat => .repeat,
        };
    }
};

// `Key` is an enum, and zigo binds functions on structs and opaque types only,
// so its helpers are re-exported as free functions here. They are for a
// caller mapping platform key codes onto the enum, or deciding whether a
// press can carry text.

/// The key for a printable ASCII byte, or null if none maps to it.
pub fn keyFromASCII(ch: u8) ?Key {
    return Key.fromASCII(ch);
}

/// The Unicode codepoint the key produces on a US layout, if it has one.
pub fn keyCodepoint(key: Key) ?u21 {
    return key.codepoint();
}

/// True for keys that produce text on a US layout.
pub fn keyPrintable(key: Key) bool {
    return key.printable();
}

/// True for modifier keys such as shift, control and alt.
pub fn keyModifier(key: Key) bool {
    return key.modifier();
}

/// True for keys on the numeric keypad.
pub fn keyKeypad(key: Key) bool {
    return key.keypad();
}

/// True for shift on either side.
pub fn keyLeftOrRightShift(key: Key) bool {
    return key.leftOrRightShift();
}

/// True for alt on either side.
pub fn keyLeftOrRightAlt(key: Key) bool {
    return key.leftOrRightAlt();
}

/// True for the platform's primary modifier: command on macOS, control
/// everywhere else. Which one that is was decided when the native library
/// for this platform was built, so it needs no runtime check in Go.
pub fn keyCtrlOrSuper(key: Key) bool {
    return key.ctrlOrSuper();
}

/// True for keys a keybinding UI may remap by default.
///
/// False for the W3C "writing system" keys -- the letters, digits and
/// punctuation -- because what those produce is decided by the user's layout,
/// so a binding on one is not the same key for everyone. Everything else,
/// function and navigation keys included, is fair game.
pub fn keyShouldBeRemappable(key: Key) bool {
    return key.shouldBeRemappable();
}

/// The W3C `KeyboardEvent.code` name for the key, such as "KeyA" or
/// "ArrowUp", empty for a key with none. Static storage, so the string stays
/// valid for the life of the process.
pub fn keyW3C(key: Key) []const u8 {
    return key.w3c();
}

/// The key a W3C `KeyboardEvent.code` name selects, or null if none does.
/// For an embedder mapping browser or Electron key events onto the enum.
pub fn keyFromW3C(w3c_code: []const u8) ?Key {
    return Key.fromW3C(w3c_code);
}
pub const FocusEvent = vt.input.FocusEvent;

/// The modifiers held during a key or mouse event.
///
/// ghostty's own `Mods` is a `packed struct(u16)` that also records which
/// side each modifier was pressed on; the encoders do not read the sides, so
/// this carries the six flags in one byte.
pub const KeyMods = packed struct(u8) {
    shift: bool = false,
    ctrl: bool = false,
    alt: bool = false,
    super: bool = false,
    caps_lock: bool = false,
    num_lock: bool = false,
    _padding: u2 = 0,

    fn toGhostty(self: KeyMods) vt.input.KeyMods {
        return .{
            .shift = self.shift,
            .ctrl = self.ctrl,
            .alt = self.alt,
            .super = self.super,
            .caps_lock = self.caps_lock,
            .num_lock = self.num_lock,
        };
    }
};

/// A key event to encode. A plain value: build one per press and pass it to
/// `encodeKey` together with the text the key produced.
///
/// ghostty's `KeyEvent` holds a borrowed `utf8` slice and modifiers with no C
/// representation, so this is the flattened equivalent.
pub const KeyEvent = extern struct {
    action: KeyAction = .press,
    key: Key = .unidentified,
    mods: KeyMods = .{},
    /// Modifiers that were consumed producing the event text. Effective
    /// modifiers are `mods` minus these.
    consumed_mods: KeyMods = .{},
    /// True while the event is part of an unfinished dead-key composition.
    composing: bool = false,
    /// The codepoint the key produces unshifted, or zero for none.
    unshifted_codepoint: u32 = 0,
};

/// Encode a key event for `terminal`, whose modes decide the encoding. `utf8`
/// is the text the key produced, empty when it produced none.
pub fn encodeKey(
    writer: *std.Io.Writer,
    terminal: *const Terminal,
    event: KeyEvent,
    utf8: []const u8,
) !void {
    const inner: vt.input.KeyEvent = .{
        .action = event.action.toGhostty(),
        .key = event.key,
        .mods = event.mods.toGhostty(),
        .consumed_mods = event.consumed_mods.toGhostty(),
        .composing = event.composing,
        .utf8 = utf8,
        .unshifted_codepoint = @intCast(event.unshifted_codepoint),
    };
    try vt.input.encodeKey(writer, inner, .fromTerminal(terminal));
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

/// A mouse event to encode. A plain value: build one per event and pass it
/// to `encodeMouse`.
pub const MouseEvent = extern struct {
    action: MouseAction = .press,
    /// The button the event is about. Ignored unless `has_button` is set,
    /// which a motion event without a held button leaves clear.
    button: MouseButton = .unknown,
    has_button: bool = false,
    mods: KeyMods = .{},
    /// Position in already-DPI-scaled pixels, relative to the surface.
    x: f32 = 0,
    y: f32 = 0,
};

/// Encode a mouse event for `terminal`, whose reporting mode and format decide
/// whether anything is written at all.
///
/// `any_button_pressed` should include this event, so a press reports true.
pub fn encodeMouse(
    writer: *std.Io.Writer,
    terminal: *const Terminal,
    event: MouseEvent,
    size: RenderSize,
    any_button_pressed: bool,
) !void {
    var opts: vt.input.MouseEncodeOptions = .fromTerminal(terminal, size.toRenderer());
    opts.any_button_pressed = any_button_pressed;
    const inner: vt.input.MouseEncodeEvent = .{
        .action = event.action,
        .button = if (event.has_button) event.button else null,
        .mods = event.mods.toGhostty(),
        .pos = .{ .x = event.x, .y = event.y },
    };
    try vt.input.encodeMouse(writer, inner, opts);
}

/// Encode `data` for pasting into `terminal`, respecting bracketed paste mode.
pub fn encodePaste(
    writer: *std.Io.Writer,
    terminal: *const Terminal,
    data: []const u8,
) !void {
    try vt.input.encodePasteWriter(writer, data, .fromTerminal(terminal));
}
