//! Reads of terminal state that a program can change but the bindings could
//! otherwise only write.
//!
//! Everything here answers a question an embedder has to ask after feeding
//! bytes: what did that sequence actually leave behind? The setters for most of
//! it are already bound; these are the matching gets.
const vt = @import("ghostty_vt");

const Terminal = vt.Terminal;

/// The margins DECSTBM and DECSLRM set, 0-indexed and inclusive.
///
/// A terminal always has a region: with no margins set it covers the whole
/// screen, so `bottom` is `rows - 1` and `right` is `cols - 1`.
pub const ScrollRegion = extern struct {
    top: u16,
    bottom: u16,
    left: u16,
    right: u16,
};

/// The current scrolling region.
pub fn scrollRegion(self: *Terminal) ScrollRegion {
    const r = self.scrolling_region;
    return .{
        .top = r.top,
        .bottom = r.bottom,
        .left = r.left,
        .right = r.right,
    };
}

/// The character set configured in one slot, as SCS set it.
pub fn charset(self: *Terminal, slot: vt.CharsetSlot) vt.Charset {
    return self.screens.active.charset.charsets.get(slot);
}

/// The slot GL resolves to: the set used for codepoints up to 127.
pub fn charsetGL(self: *Terminal) vt.CharsetSlot {
    return self.screens.active.charset.gl;
}

/// The slot GR resolves to: the set used for 8-bit printable codepoints.
pub fn charsetGR(self: *Terminal) vt.CharsetSlot {
    return self.screens.active.charset.gr;
}

/// The slot a pending single shift (SS2/SS3) will use for exactly one
/// character, or absent if none is pending.
pub fn charsetSingleShift(self: *Terminal) ?vt.CharsetSlot {
    return self.screens.active.charset.single_shift;
}

/// The most recent protected mode (DECSCA or the older SPA/EPA) on the active
/// screen. This never returns to `off` once set, until the screen is reset:
/// ECH and friends key off the most recent mode, not the current pen.
pub fn protectedMode(self: *Terminal) vt.ProtectedMode {
    return self.screens.active.protected_mode;
}

/// How the terminal reports mouse activity.
///
/// This is the resolved tracking mode rather than any one mode flag: several
/// DEC modes select tracking and the last one written wins, so the modes alone
/// cannot tell you which is in effect.
///
/// A plain mirror of ghostty's `mouse.Event`, which is built by `lib.Enum` and
/// so has no name a binding can use.
pub const MouseTracking = enum(u8) {
    /// No reporting.
    none,
    /// Mode 9: press only.
    x10,
    /// Mode 1000: press and release.
    normal,
    /// Mode 1002: press, release, and motion while a button is held.
    button,
    /// Mode 1003: press, release, and all motion.
    any,
};

/// The tracking mode currently in effect.
pub fn mouseTracking(self: *Terminal) MouseTracking {
    return switch (self.flags.mouse_event) {
        .none => .none,
        .x10 => .x10,
        .normal => .normal,
        .button => .button,
        .any => .any,
    };
}

/// Whether the current tracking mode reports motion as well as buttons.
pub fn mouseTrackingSendsMotion(self: *Terminal) bool {
    // ghostty's `mouse.eventSendsMotion` is not reachable from libghostty-vt's
    // public surface, so the rule it encodes is repeated here.
    return switch (mouseTracking(self)) {
        .button, .any => true,
        .none, .x10, .normal => false,
    };
}

/// The encoding used for mouse reports.
///
/// A plain mirror of ghostty's `mouse.Format`, for the same reason as
/// `MouseTracking`.
pub const MouseReportFormat = enum(u8) {
    /// The original single-byte encoding, coordinates offset by 32.
    x10,
    /// Mode 1005: UTF-8 coordinates.
    utf8,
    /// Mode 1006: SGR.
    sgr,
    /// Mode 1015: urxvt.
    urxvt,
    /// Mode 1016: SGR with pixel coordinates.
    sgr_pixels,
};

/// The report encoding currently in effect.
pub fn mouseReportFormat(self: *Terminal) MouseReportFormat {
    return switch (self.flags.mouse_format) {
        .x10 => .x10,
        .utf8 => .utf8,
        .sgr => .sgr,
        .urxvt => .urxvt,
        .sgr_pixels => .sgr_pixels,
    };
}

/// What a DECRQM query of a mode would answer.
pub const ModeReport = enum(u8) {
    /// The terminal does not implement this mode.
    not_recognized = 0,
    set = 1,
    reset = 2,
    /// Always on; setting or resetting it does nothing.
    permanently_set = 3,
    /// Always off; setting or resetting it does nothing.
    permanently_reset = 4,
};

/// The DECRPM state of a mode, by its number.
///
/// `ansi` distinguishes the two namespaces: false is a DEC private mode
/// (`CSI ? Pd $ p`), true is an ANSI mode (`CSI Pd $ p`). Modes are taken as
/// numbers rather than as an enum on purpose -- the point of the query is to
/// learn whether a number is implemented at all, which an exhaustive enum
/// could never express.
pub fn modeReport(self: *Terminal, mode: u16, ansi: bool) ModeReport {
    const report = self.modes.getReport(.{
        .value = @truncate(mode),
        .ansi = ansi,
    });
    return switch (report.state) {
        .not_recognized => .not_recognized,
        .set => .set,
        .reset => .reset,
        .permanently_set => .permanently_set,
        .permanently_reset => .permanently_reset,
    };
}
