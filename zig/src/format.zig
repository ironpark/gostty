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

/// Format the active area -- the rows on screen, not the scrollback -- with
/// the terminal's colors and, for styled output, its palette, modes and
/// other state a replay needs. `Screen.format` covers the scrollback too.
pub fn formatTerminal(self: *Terminal, opts: FormatOptions, writer: *std.Io.Writer) !void {
    var f = vt.formatter.TerminalFormatter.init(self, opts.toGhostty());
    f.opts.background = self.colors.background.get();
    f.opts.foreground = self.colors.foreground.get();
    if (opts.resolve_palette) f.opts.palette = &self.colors.palette.current;
    try f.format(writer);
}

/// Format a whole screen, scrollback included. `Terminal.format` is the
/// active area only.
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
