//! Rendering screen contents back out as text, VT sequences or HTML.
const std = @import("std");
const vt = @import("ghostty_vt");
const common = @import("common.zig");

const Terminal = common.Terminal;
const Screen = common.Screen;
const screen = @import("screen.zig");
const Selection = screen.Selection;
const selectionToPins = screen.selectionToPins;

/// What `formatTerminal` and `Screen.format` emit. Declared here rather than
/// re-exported because ghostty's enum has a two-bit tag, which cannot sit in
/// the extern `FormatOptions`.
pub const FormatterFormat = enum(u8) {
    /// Plain text.
    plain,
    /// VT sequences that replay colors, styles and links; lines end in CRLF.
    vt,
    /// HTML with inline styles; palette colors become CSS variables unless
    /// `resolve_palette` is set.
    html,

    fn toGhostty(self: FormatterFormat) vt.formatter.Format {
        return common.mirror(vt.formatter.Format, self);
    }
};

/// How the formatters render screen contents. Every field is off in its
/// zero value, so a Go `FormatOptions{}` is plain text, trimmed, with styles
/// and links included where the format can carry them.
pub const FormatOptions = extern struct {
    /// Which of the three renderings to emit. The zero value is plain text.
    format: FormatterFormat = .plain,
    /// Join soft-wrapped lines back into one instead of emitting them as
    /// they are laid out at the current width.
    unwrap: bool = false,
    /// Keep trailing spaces on lines that have other text. Trailing blank
    /// lines are always dropped.
    keep_trailing_whitespace: bool = false,
    /// Include the cursor position. Styled formats only.
    cursor: bool = false,
    /// Leave out text styles. Styled formats only.
    no_styles: bool = false,
    /// Leave out OSC 8 hyperlinks. Styled formats only.
    no_hyperlinks: bool = false,
    /// Resolve palette indices to the terminal's current RGB values rather
    /// than emitting the index. Styled formats only.
    ///
    /// `Terminal.format` and `Terminal.formatSelection` only. A `Screen` is
    /// borrowed from its terminal and does not carry the palette or the default
    /// colors, so its formatters ignore this and emit the index. Since the
    /// terminal's formatter reaches the scrollback too, format through the
    /// terminal when the output has to carry real colors.
    resolve_palette: bool = false,

    fn toGhostty(self: FormatOptions) vt.formatter.Options {
        return .{ .emit = self.format.toGhostty(), .unwrap = self.unwrap, .trim = !self.keep_trailing_whitespace };
    }

    fn screenExtra(self: FormatOptions) vt.formatter.ScreenFormatter.Extra {
        var extra: vt.formatter.ScreenFormatter.Extra = .none;
        extra.cursor = self.cursor;
        extra.style = !self.no_styles;
        extra.hyperlink = !self.no_hyperlinks;
        return extra;
    }
};

/// Format the active screen with the terminal's colors and, for styled output,
/// its palette, modes and other state a replay needs.
///
/// Scrollback is included: ghostty's terminal formatter walks the whole page
/// list of the active screen, not just the rows on display. This differs from
/// `Screen.format` only in that it follows whichever screen is active rather
/// than staying with the one it was given.
pub fn formatTerminal(self: *Terminal, opts: FormatOptions, writer: *std.Io.Writer) !void {
    var f = vt.formatter.TerminalFormatter.init(self, opts.toGhostty());
    f.opts.background = self.colors.background.get();
    f.opts.foreground = self.colors.foreground.get();
    if (opts.resolve_palette) f.opts.palette = &self.colors.palette.current;
    try f.format(writer);
}

/// Format the part of the active screen inside `sel`, using the terminal's
/// default colors and palette. Returns false, writing nothing, when `sel` is
/// outside the active screen.
pub fn formatTerminalSelection(self: *Terminal, opts: FormatOptions, sel: Selection, writer: *std.Io.Writer) !bool {
    const inner = selectionToPins(screen.activeScreen(self), sel) orelse return false;
    var f = vt.formatter.TerminalFormatter.init(self, opts.toGhostty());
    f.content = .{ .selection = inner };
    f.opts.background = self.colors.background.get();
    f.opts.foreground = self.colors.foreground.get();
    if (opts.resolve_palette) f.opts.palette = &self.colors.palette.current;
    try f.format(writer);
    return true;
}

/// Format a whole screen, scrollback included.
///
/// Unlike `Terminal.format` this stays with the screen it is given rather than
/// following the active one. It also has no terminal to ask, so the default
/// colors and `resolve_palette` do not apply.
pub fn screenFormat(self: *Screen, opts: FormatOptions, writer: *std.Io.Writer) !void {
    var f = vt.formatter.ScreenFormatter.init(self, opts.toGhostty());
    f.extra = opts.screenExtra();
    try f.format(writer);
}

/// Format the part of a screen inside `sel`. Returns false, writing
/// nothing, if `sel` is outside the screen.
pub fn screenFormatSelection(self: *Screen, opts: FormatOptions, sel: Selection, writer: *std.Io.Writer) !bool {
    const inner = selectionToPins(self, sel) orelse return false;
    var f = vt.formatter.ScreenFormatter.init(self, opts.toGhostty());
    f.content = .{ .selection = inner };
    f.extra = opts.screenExtra();
    try f.format(writer);
    return true;
}
