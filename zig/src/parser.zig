//! The two parsers ghostty can drive without a `Terminal`.
//!
//! `Stream` is the whole VT machine: bytes in, terminal state out. An embedder
//! that only wants to read one kind of sequence -- a shell integration harness
//! reading OSC, a pager colouring its own output with SGR -- has no terminal to
//! feed and no interest in the rest of the state machine. ghostty exposes both
//! parsers on their own, so they are bound on their own.
const std = @import("std");
const vt = @import("ghostty_vt");
const terminal_ = @import("terminal.zig");
const stream_ = @import("stream.zig");

const Allocator = std.mem.Allocator;

// -- OSC ---------------------------------------------------------------------

/// How an OSC sequence was terminated. ST is the standard, BEL is what a lot
/// of shells still emit, and a reply is expected to use whichever the request
/// did.
///
/// Mirrored rather than aliased: ghostty's `osc.Terminator` carries a `format`
/// method written against the pre-0.16 `std.fmt` API, and binding the enum
/// makes zigo analyse it.
pub const OSCTerminator = enum(u8) { st, bel };

/// The step of the shell's prompt lifecycle an OSC 133 sequence marks.
pub const SemanticPromptAction = vt.osc.Command.SemanticPrompt.Action;

/// Which OSC command the parser recognised.
///
/// This is the complete set: ghostty's `osc.Command.Key` in its own order, so
/// a command this binding has no accessor for is still identifiable rather
/// than silently lost. `OSCParser` exposes payloads for the subset an embedder
/// normally acts on; for the rest the kind is the whole answer.
///
/// Mirrored rather than reused because `osc.Command.Key` is built by
/// `lib.Enum`, whose type name is a generic instantiation zigo cannot bind.
pub const OSCCommand = enum(u8) {
    invalid,
    change_window_title,
    change_window_icon,
    semantic_prompt,
    clipboard_contents,
    report_pwd,
    mouse_shape,
    color_operation,
    kitty_color_protocol,
    show_desktop_notification,
    hyperlink_start,
    hyperlink_end,
    conemu_sleep,
    conemu_show_message_box,
    conemu_change_tab_title,
    conemu_progress_report,
    conemu_wait_input,
    conemu_guimacro,
    conemu_run_process,
    conemu_output_environment_variable,
    conemu_xterm_emulation,
    conemu_comment,
    kitty_text_sizing,
    kitty_clipboard_protocol,
    kitty_dnd_protocol,
    context_signal,
    kitty_desktop_notification,
};

/// A standalone OSC parser: bytes in, one command out.
///
/// Use it by feeding the payload of an OSC sequence -- everything between
/// `ESC ]` and the terminator, exclusive -- then calling `end`. `end` reports
/// whether a command was recognised; `command` and the accessors below then
/// describe it, and stay valid until the next `feed`, `end` or `reset`.
///
/// Each string payload has an accessor named after what the string is, so a
/// caller reads `windowTitle` or `hyperlinkURI` rather than a generic "text"
/// whose meaning depends on the command. Every accessor answers for its own
/// command only and is empty for any other, which means `command` still has to
/// be consulted first: an empty title and no title at all read the same.
///
/// ghostty's own `end` hands back a pointer into the parser's buffer that the
/// next call invalidates. That contract does not survive a trip through Go, so
/// this wrapper copies the payloads it exposes into storage it owns.
pub const OSCParser = struct {
    inner: vt.osc.Parser,
    gpa: Allocator,

    kind: OSCCommand = .invalid,
    window_title: []const u8 = "",
    icon_name: []const u8 = "",
    pwd_value: []const u8 = "",
    hyperlink_uri: []const u8 = "",
    hyperlink_id: []const u8 = "",
    notification_title: []const u8 = "",
    notification_body: []const u8 = "",
    clipboard_data: []const u8 = "",
    mouse_shape: []const u8 = "",
    prompt_options: []const u8 = "",
    clipboard_kind: u8 = 0,
    prompt_action: SemanticPromptAction = .fresh_line,
    progress_state: stream_.ProgressState = .remove,
    /// Percent complete, or -1 when the report carried no percentage.
    progress: i16 = -1,

    /// Feed the parser the bytes of an OSC payload. May be called repeatedly;
    /// a sequence can arrive split across reads.
    pub fn feed(self: *OSCParser, bytes: []const u8) void {
        self.inner.nextSlice(bytes);
    }

    /// Finish the sequence and report whether it named a command.
    pub fn end(self: *OSCParser, terminator: OSCTerminator) bool {
        self.clear();
        const cmd = self.inner.end(switch (terminator) {
            .st => 0x1b,
            .bel => 0x07,
        }) orelse return false;
        self.capture(cmd);
        return true;
    }

    /// Discard the sequence in progress and the last command's payloads.
    pub fn reset(self: *OSCParser) void {
        self.clear();
        self.inner.reset();
    }

    /// The last command parsed, or `invalid` if `end` has not said yes.
    pub fn command(self: *OSCParser) OSCCommand {
        return self.kind;
    }

    /// OSC 0 and OSC 2: the window title.
    ///
    /// ghostty does not decode it. Under title mode 0 the bytes are hex, and
    /// otherwise they are UTF-8 or latin1 depending on how the terminal was
    /// set up, which only the embedder knows.
    pub fn windowTitle(self: *OSCParser) []const u8 {
        return self.window_title;
    }

    /// OSC 1: the icon name. Not well defined by any specification, and
    /// ghostty itself ignores it.
    pub fn icon(self: *OSCParser) []const u8 {
        return self.icon_name;
    }

    /// OSC 7: the shell's working directory, as a `file://` URL. ghostty does
    /// not check that it is one, and neither does this.
    pub fn pwd(self: *OSCParser) []const u8 {
        return self.pwd_value;
    }

    /// OSC 8: the link target.
    pub fn hyperlinkURI(self: *OSCParser) []const u8 {
        return self.hyperlink_uri;
    }

    /// OSC 8: the link's `id=` parameter, empty when it carried none. Two runs
    /// sharing an id are one link even when they are not adjacent.
    pub fn hyperlinkID(self: *OSCParser) []const u8 {
        return self.hyperlink_id;
    }

    /// OSC 9 and OSC 777: the notification title.
    pub fn notificationTitle(self: *OSCParser) []const u8 {
        return self.notification_title;
    }

    /// OSC 9 and OSC 777: the notification body.
    pub fn notificationBody(self: *OSCParser) []const u8 {
        return self.notification_body;
    }

    /// OSC 52: the base64 payload to put on the clipboard, or a bare `?` when
    /// the program is asking to read the clipboard rather than to write it.
    pub fn clipboardData(self: *OSCParser) []const u8 {
        return self.clipboard_data;
    }

    /// OSC 52: which selection the request names, as the protocol's own
    /// character (`c` for clipboard, `p` for primary, `s` for the configured
    /// default), or zero for any other command.
    pub fn clipboardSelection(self: *OSCParser) u8 {
        return self.clipboard_kind;
    }

    /// OSC 22: the pointer shape, usually a W3C CSS cursor name. ghostty
    /// parses whatever string is given without validating it.
    pub fn mouseShape(self: *OSCParser) []const u8 {
        return self.mouse_shape;
    }

    /// OSC 133: which prompt boundary this sequence marks.
    pub fn semanticPromptAction(self: *OSCParser) SemanticPromptAction {
        return self.prompt_action;
    }

    /// OSC 133: the raw, unvalidated option string that followed the action.
    pub fn semanticPromptOptions(self: *OSCParser) []const u8 {
        return self.prompt_options;
    }

    /// OSC 9;4: what the program says about its progress.
    pub fn progressState(self: *OSCParser) stream_.ProgressState {
        return self.progress_state;
    }

    /// OSC 9;4: percent complete, or -1 when the report carried no percentage.
    pub fn progressValue(self: *OSCParser) i16 {
        return self.progress;
    }

    /// Copy out of `cmd` whatever this binding exposes. Commands with no
    /// accessors leave every slot empty: their kind is the whole payload.
    fn capture(self: *OSCParser, cmd: *const vt.osc.Command) void {
        // ghostty's tag enum is a `lib.Enum`, so the mirror is matched by name
        // rather than by value. The two lists are in the same order, but a new
        // upstream command inserted in the middle should widen `OSCCommand`,
        // not shift every kind Go already knows.
        self.kind = std.meta.stringToEnum(
            OSCCommand,
            @tagName(cmd.*),
        ) orelse .invalid;

        switch (cmd.*) {
            .change_window_title => |v| self.window_title = self.dupe(v),
            .change_window_icon => |v| self.icon_name = self.dupe(v),
            .report_pwd => |v| self.pwd_value = self.dupe(v.value),
            .mouse_shape => |v| self.mouse_shape = self.dupe(v.value),
            .hyperlink_start => |v| {
                self.hyperlink_uri = self.dupe(v.uri);
                if (v.id) |id| self.hyperlink_id = self.dupe(id);
            },
            .show_desktop_notification => |v| {
                self.notification_title = self.dupe(v.title);
                self.notification_body = self.dupe(v.body);
            },
            .clipboard_contents => |v| {
                self.clipboard_kind = v.kind;
                self.clipboard_data = self.dupe(v.data);
            },
            .semantic_prompt => |v| {
                self.prompt_action = v.action;
                self.prompt_options = self.dupe(v.options_unvalidated);
            },
            .conemu_progress_report => |v| {
                self.progress_state = v.state;
                self.progress = if (v.progress) |p| @intCast(p) else -1;
            },
            else => {},
        }
    }

    /// Payload copies are best effort: an allocation failure here would have to
    /// be reported from an accessor that has nothing to say about it, so a
    /// string that cannot be copied is handed over empty instead.
    fn dupe(self: *OSCParser, str: []const u8) []const u8 {
        return self.gpa.dupe(u8, str) catch "";
    }

    fn clear(self: *OSCParser) void {
        inline for (.{
            "window_title",
            "icon_name",
            "pwd_value",
            "hyperlink_uri",
            "hyperlink_id",
            "notification_title",
            "notification_body",
            "clipboard_data",
            "mouse_shape",
            "prompt_options",
        }) |name| {
            const owned = @field(self, name);
            if (owned.len > 0) self.gpa.free(owned);
            @field(self, name) = "";
        }
        self.kind = .invalid;
        self.clipboard_kind = 0;
        self.prompt_action = .fresh_line;
        self.progress_state = .remove;
        self.progress = -1;
    }
};

/// Create a standalone OSC parser.
///
/// The parser is given an allocator, so the sequences that are allowed to grow
/// past ghostty's 2 KiB inline buffer -- OSC 52 clipboard writes, mainly -- are
/// parsed rather than dropped.
pub fn newOSCParser(gpa: Allocator) !*OSCParser {
    const self = try gpa.create(OSCParser);
    self.* = .{ .inner = .init(gpa), .gpa = gpa };
    return self;
}

/// Destroys a parser created by `newOSCParser`.
pub fn freeOSCParser(self: *OSCParser) void {
    const gpa = self.gpa;
    self.clear();
    self.inner.deinit();
    gpa.destroy(self);
}

// -- SGR ---------------------------------------------------------------------

/// The most parameters one CSI sequence can carry, and so the most the SGR
/// functions will accept. This is ghostty's own limit, not one added here: its
/// CSI parser stops recording after this many, and the colon bitset that says
/// how they were separated is exactly this wide.
pub const MAX_SGR_PARAMS: usize = @TypeOf(@as(vt.sgr.Parser, undefined).params_sep).bit_length;

/// Which attribute a parsed SGR parameter selects: `Attribute`'s tag, spelled
/// out as its own enum.
///
/// The tag of a bound tagged union is generated for Go but has no name zigo can
/// bind on the Zig side, so it is written out here and checked against
/// `Attribute` at compile time. Nothing can drift: adding a variant to
/// `Attribute` without adding it here fails the build.
pub const SgrAttributeTag = enum(u8) {
    unset,
    bold,
    reset_bold,
    italic,
    reset_italic,
    faint,
    underline,
    underline_color_rgb,
    underline_color_256,
    reset_underline_color,
    overline,
    reset_overline,
    blink,
    reset_blink,
    inverse,
    reset_inverse,
    invisible,
    reset_invisible,
    strikethrough,
    reset_strikethrough,
    direct_color_fg,
    direct_color_bg,
    color_256_fg,
    color_256_bg,
    named_fg,
    named_bg,
    bright_named_fg,
    bright_named_bg,
    reset_fg,
    reset_bg,
    unknown,

    comptime {
        const mirrored = @typeInfo(@This()).@"enum".fields;
        const original = @typeInfo(terminal_.Attribute).@"union".fields;
        if (mirrored.len != original.len) @compileError("SgrAttributeTag is out of step with Attribute");
        for (mirrored, original) |a, b| {
            if (!std.mem.eql(u8, a.name, b.name)) @compileError("SgrAttributeTag is out of step with Attribute at " ++ a.name);
        }
    }
};

/// One attribute a CSI SGR parameter list selected: an `Attribute` taken apart
/// into its tag and its scalar payload.
///
/// It is not an `Attribute` because zigo can pass a tagged union into native
/// code -- which is how `setAttribute` takes one -- but cannot hand a usable one
/// back: a returned union arrives with its tag readable and its payload
/// unreachable. The pair here carries both, and because the tags are the same
/// list in the same order, the `Attribute<Tag>` constructor named after the tag
/// rebuilds the attribute exactly.
pub const SgrAttribute = extern struct {
    tag: SgrAttributeTag,
    /// Zero where the tag carries no payload. Otherwise an RGB colour as
    /// `0xRRGGBB` for the direct-colour and underline-colour tags, a palette
    /// index for the 256-colour tags, an `Underline` style for `underline`,
    /// and a `ColorName` for the named-colour tags.
    value: u32,
};

/// How many attributes a CSI SGR parameter list yields, for sizing the slice
/// `sgrAttributes` fills. An empty list is the SGR reset, so it yields one.
///
/// See `sgrAttributes` for what `colon_mask` means.
pub fn sgrAttributeCount(params: []const u16, colon_mask: u32) !usize {
    var parser = try sgrParser(params, colon_mask);
    var count: usize = 0;
    while (parser.next()) |_| count += 1;
    return count;
}

/// Parse a CSI SGR parameter list into `dst`, in order, and report how many
/// attributes were written.
///
/// `params` is the parameter list of a `CSI ... m` sequence with the `m`
/// dropped, and `colon_mask` says how the parameters were separated: bit `i`
/// set means parameter `i` was followed by a colon rather than a semicolon,
/// which is ghostty's own convention for the bitset. That distinction is not
/// cosmetic -- `4;3` is underline then italic while `4:3` is a curly underline,
/// and `38;2;r;g;b` and `38:2::r:g:b` are both truecolor -- so it has to be
/// carried explicitly. A mask is used rather than ghostty's own
/// `std.StaticBitSet` because a bitset has no C representation.
///
/// A parameter ghostty does not implement yields `unknown` rather than being
/// skipped, so the attributes line up with the sequence as written.
pub fn sgrAttributes(params: []const u16, colon_mask: u32, dst: []SgrAttribute) !usize {
    var parser = try sgrParser(params, colon_mask);
    var written: usize = 0;
    while (parser.next()) |attr| : (written += 1) {
        if (written >= dst.len) return error.NoSpaceLeft;
        dst[written] = decompose(terminal_.Attribute.fromVt(attr));
    }
    return written;
}

fn decompose(attr: terminal_.Attribute) SgrAttribute {
    return .{
        .tag = @enumFromInt(@intFromEnum(attr)),
        .value = switch (attr) {
            .underline => |v| @intFromEnum(v),
            .underline_color_rgb,
            .direct_color_fg,
            .direct_color_bg,
            => |v| v,
            .underline_color_256,
            .color_256_fg,
            .color_256_bg,
            => |v| v,
            .named_fg,
            .named_bg,
            .bright_named_fg,
            .bright_named_bg,
            => |v| @intFromEnum(v),
            else => 0,
        },
    };
}

fn sgrParser(params: []const u16, colon_mask: u32) !vt.sgr.Parser {
    if (params.len > MAX_SGR_PARAMS) return error.SGRTooManyParams;
    var sep: @TypeOf(@as(vt.sgr.Parser, undefined).params_sep) = .initEmpty();
    for (0..params.len) |i| {
        if (colon_mask & (@as(u32, 1) << @intCast(i)) != 0) sep.set(i);
    }
    return .{ .params = params, .params_sep = sep };
}
