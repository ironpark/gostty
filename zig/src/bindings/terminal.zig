//! Terminal state, screens, selection, search and tracked references.
const p = @import("policy.zig");
const Stream = @import("stream.zig").Stream;
const zigo = p.zigo;
const api = p.api;

// The types, each captured as a context: one declaration that is both the type
// and the scope its members are selected from. `.context()` only captures --
// what registers the type is the `members()` result below reaching
// `.declarations` -- so the lifecycle and the methods are written in one place
// per type without naming it twice.
pub const Terminal = api.handle("Terminal", .{
    .fields = &.{
        // Read straight off the terminal rather than through ghostty's `cols()`
        // and `rows()`, which is where these two sentences come from: the
        // accessor's doc is not the field's, so it is spelled here.
        .{ .path = "cols", .doc = "The current column count, read without touching page memory." },
        .{ .path = "rows", .doc = "The number of populated rows, read without touching page memory." },
        .{ .path = "screens.active.cursor.x", .name = "cursorX" },
        .{ .path = "screens.active.cursor.y", .name = "cursorY" },
        .{ .path = "screens.active.cursor.cursor_style", .name = "cursorStyle" },
        .{ .path = "screens.active_key", .name = "activeScreenKey" },
        // The LCF: set once printing has filled the last column, so the next
        // print soft-wraps instead of overwriting.
        .{ .path = "screens.active.cursor.pending_wrap", .name = "cursorPendingWrap" },
        .{ .path = "screens.active.cursor.protected", .name = "cursorProtected" },
        .{ .path = "width_px", .name = "widthPx" },
        .{ .path = "height_px", .name = "heightPx" },
        .{ .path = "flags.focused", .name = "focused" },
        .{ .path = "flags.visible", .name = "visible" },
        .{ .path = "flags.password_input", .name = "passwordInput" },
        // Charset state and the protected mode are plain reads off the active
        // screen, so they are paths rather than wrappers that would forward to
        // exactly these fields. Only `charset` stays a function: it takes a
        // slot, so there is no one field for it to be.
        .{ .path = "screens.active.charset.gl", .name = "charsetGL", .doc = "The slot GL resolves to: the set used for codepoints up to 127." },
        .{ .path = "screens.active.charset.gr", .name = "charsetGR", .doc = "The slot GR resolves to: the set used for 8-bit printable codepoints." },
        .{ .path = "screens.active.charset.single_shift", .name = "charsetSingleShift", .doc = "The slot a pending single shift (SS2/SS3) will use for exactly one character, or absent if none is pending." },
        .{ .path = "screens.active.protected_mode", .name = "protectedMode", .doc = "The most recent protected mode (DECSCA or the older SPA/EPA) on the active screen. This never returns to off once set, until the screen is reset: ECH and friends key off the most recent mode, not the current pen." },
    },
}).with(.{ .doc =
    \\Terminal owns mutable terminal state. Serialize all calls, including
    \\getters, with calls on its streams, screens, searches, gestures and grid
    \\references. Handle locks protect lifetime only; they do not serialize native
    \\operations. Callbacks may answer their supplied request but must not
    \\recursively feed, resize, reset or close the same terminal.
}).use(p.convenience.plugin, .{ .feature = .terminal_config }).use(p.build_info.plugin, .{
    .simd = p.gostty.build_features.simd,
    .kitty_graphics = p.gostty.build_features.kitty_graphics,
    .tmux_control_mode = p.gostty.build_features.tmux_control_mode,
}).context();

const Screen = p.trimmed(api.handle("Screen", .{})).context();

const Search = p.trimmed(api.handle("Search", .{})).context();

const GridRef = p.trimmed(api.handle("GridRef", .{})).context();

const Gesture = p.trimmed(api.handle("Gesture", .{ .fields = &.{
    .{ .path = "inner.left_click_count", .name = "clickCount", .doc = "How many clicks the current sequence is at: 0 before any press, then 1, 2 or 3. What an emulator switches on to decide what a click means." },
    .{ .path = "inner.left_click_dragged", .name = "dragged", .doc = "Whether the pointer has left the pressed cell during this gesture. Read it on release: a click that never dragged is the one that should follow a hyperlink or move the shell cursor, rather than one that happened to end where it started after a round trip." },
} })).context();

const ColorName = p.trimmed(p.enumeration("ColorName", .{ .text = true, .open = true })).context();

// One group per type: the constructor that makes it, the destructor that ends
// it, and everything a caller can do in between.
const terminal_group = Terminal.members(p.functions(api, .{
    // `Terminal.init` returns by value and takes an `Options`. `options` lowers
    // it exactly as `flatten` does -- same C symbol, same shim -- and splits the
    // listed fields by whether Zig gave them a default: `cols` and `rows` have
    // none, because there is no right size to assume, so they stay positional,
    // and the rest become `With*`. ghostty's own constructor is still bound
    // without a wrapper, and now with its own defaults rather than only two of
    // its fields.
    //
    // What is left out is what cannot cross: `colors` holds optionals inside a
    // struct and `default_modes` is an unregistered packed struct (both
    // ZIGO040). Those stay post-construction setters, which is what
    // `TerminalConfig` in the convenience plugin is for.
    Terminal.func("init", .{
        .name = "newTerminal",
        .role = .{ .constructor = .{ .type = Terminal.typeRef() } },
        .params = &.{zigo.param.options(2, &.{
            "cols",
            "rows",
            "max_scrollback_bytes",
            "max_scrollback_lines",
            "default_cursor_style",
            "default_cursor_blink",
        }, .{})},
    }),
    Terminal.func("deinit", .{ .role = .{ .destructor = Terminal.typeRef() } }),

    // The handles a terminal owns. Each borrows it -- a stream's handler
    // reaches back through it for an allocator as it tears down, and a search,
    // a gesture and a grid reference all hold pins inside its page storage --
    // so each is a child that has to close before the terminal does.
    p.childConstructor("newStream", Stream),
    p.childConstructor("newSearch", Search),
    p.childConstructor("newGesture", Gesture),
    p.childConstructor("newGridRef", GridRef),

    // Printing and the cursor.
    Terminal.func("printString", .{}),
    Terminal.func("plainString", .{ .returns = zigo.result.owned() }),
    Terminal.func("print", .{ .params = &.{.{ .index = 1, .go_name = "c" }} }),
    Terminal.func("printRepeat", .{}),
    Terminal.func("printSlice", .{ .params = &.{.{ .index = 1, .semantic = .codepoint }} }),
    Terminal.func("setCursorStyle", .{}),
    Terminal.func("setCursorPos", .{}),
    p.withMust(Terminal.func("carriageReturn", .{})),
    Terminal.func("linefeed", .{}),
    p.withMust(Terminal.func("backspace", .{})),
    p.withMust(Terminal.func("cursorIsAtPrompt", .{})),
    p.withMust(Terminal.func("fullReset", .{})),
    p.withMust(Terminal.func("cursorUp", .{})),
    p.withMust(Terminal.func("cursorDown", .{})),
    p.withMust(Terminal.func("cursorLeft", .{})),
    p.withMust(Terminal.func("cursorRight", .{})),
}) ++ Terminal.funcs(.{ .names = &.{
    "saveCursor",
    "restoreCursor",
    "index",
    "reverseIndex",
} }) ++ p.functions(api, .{
    // Screens. `switchScreen` returns the screen being left, borrowed, or
    // absent when `key` was already active.
    Terminal.func("switchScreen", .{ .returns = zigo.result.borrowed() }),
    Terminal.func("switchScreenMode", .{}),
    api.func("activeScreen", .{ .returns = zigo.result.borrowed() }),
    api.func("screen", .{ .returns = zigo.result.borrowed() }),
    // ghostty writes into the caller's buffer and returns the prefix it filled.
    // `.returned_slice` takes that slice's length as the written count, so the
    // Go signature is the same as any other out buffer and no wrapper has to
    // exist just to say `.len`.
    Terminal.func("printAttributes", .{
        .name = "printAttributesInto",
        .params = &.{zigo.param.output(1, .returned_slice)},
    }),
    api.func("historyString", .{
        .returns = zigo.result.owned(),
        .covers = &.{Screen.ref("dumpStringAlloc")},
    }),
}) ++ Terminal.funcs(.{
    .names = &.{
        // Tab stops.
        "horizontalTab",
        "horizontalTabBack",
        "tabSet",
        "tabReset",
        "tabClear",
    },
}) ++ p.functions(api, .{
    "setTabstop",
    "unsetTabstop",
    "resetTabstops",

    // Scrolling and margins.
    Terminal.func("scrollUp", .{}),
    Terminal.func("scrollDown", .{}),
    Terminal.func("setTopAndBottomMargin", .{}),
    Terminal.func("setLeftAndRightMargin", .{}),
    Terminal.func("scrollViewport", .{}),
    api.func("setScrollbackMaxBytes", .{ .covers = &.{Terminal.ref("setScrollbackMaxBytes")} }),
    "clearScrollbackMaxBytes",
    api.func("setScrollbackMaxLines", .{ .covers = &.{Terminal.ref("setScrollbackMaxLines")} }),
    "clearScrollbackMaxLines",
}) ++ Terminal.funcs(.{
    .names = &.{
        // Editing.
        "insertLines",
        "deleteLines",
        "insertBlanks",
        "deleteChars",
        "eraseChars",
        "eraseLine",
        "eraseDisplay",
        "decaln",
    },
}) ++ p.functions(api, .{
    // Omitted cell_size_px keeps its native null default.
    Terminal.func("resize", .{
        .doc = "Change the viewport size, leaving the pixel geometry alone.",
        .params = &.{zigo.param.flatten(2, &.{ "cols", "rows" })},
    }),
    "resizeCells",

    // Metadata the terminal tracks for the shell.
    api.func("formatTerminal", .{ .name = "Format" }),
    api.func("formatTerminalSelection", .{ .name = "FormatSelection" }),
    Terminal.func("setPwd", .{}),
    // Both return `?[:0]const u8`, which the utf8 inference does not reach --
    // dropping these two hints turns the Go results from `string` into
    // `[]byte`, so they are spelled rather than left to the default.
    Terminal.func("getPwd", .{ .returns = .{ .semantic = .utf8_string } }),
    Terminal.func("getTitle", .{ .returns = .{ .semantic = .utf8_string } }),
    Terminal.func("setTitle", .{}),

    // Colors and modes: what a program on the pty asks for.
    "backgroundColor",
    "foregroundColor",
    "cursorColor",
    api.func("paletteColors", .{ .params = &.{p.out(1)} }),
    "modeEnabled",
    "setMode",
    api.func("setAttribute", .{ .covers = &.{Terminal.ref("setAttribute")} }),
    Terminal.func("setProtectedMode", .{}),
    Terminal.func("configureCharset", .{}),
    Terminal.func("invokeCharset", .{}),
    Terminal.func("deccolm", .{}),
    Terminal.func("compressionActivity", .{}),

    // Terminal configuration: what the embedder sets underneath whatever a
    // program on the pty asks for.
    "setDefaultBackgroundColor",
    "setDefaultForegroundColor",
    "setDefaultCursorColor",
    Terminal.func("setDefaultCursorStyle", .{}),
    api.func("setDefaultCursorBlink", .{ .covers = &.{Terminal.ref("setDefaultCursorBlink")} }),
    "resetDefaultCursorBlink",
    "paletteColor",
    "setPaletteColor",
    "resetPaletteColor",
    "resetPalette",
    "setDefaultPaletteColor",
    "resetDefaultPalette",
    "setDefaultMode",
    "resetModes",
    "saveMode",
    "restoreMode",

    // Kitty graphics limits and the pixels themselves.
    Terminal.func("setKittyGraphicsSizeLimit", .{}),
    api.func("setKittyGraphicsLoadingLimits", .{
        .covers = &.{Terminal.ref("setKittyGraphicsLoadingLimits")},
    }),
    "kittyImage",
    api.func("kittyImageData", .{ .params = &.{p.out(2)} }),

    // State reads: the gets for state the bindings could already set, plus the
    // resolved values no single mode flag reports.
    "scrollRegion",
    "charset",
    "mouseTracking",
    "mouseTrackingSendsMotion",
    "mouseReportFormat",
    "modeReport",

    // The untracked one-off reads; `GridRef` is the tracked form.
    "cellAt",
    api.func("hyperlinkAt", .{ .returns = zigo.result.owned() }),
}));

const screen_group = Screen.members(p.functions(api, .{
    // ghostty takes the screen by value here; zigo passes the handle and the
    // shim copies, so no wrapper is needed.
    Screen.func("viewportIsBottom", .{}),
    Screen.func("clearSelection", .{}),
    Screen.func("endHyperlink", .{}),
    // Wrappers whose receiver zigo infers from their first argument. The
    // shared prefix is what keeps them apart in Zig; the Go name drops it.
    api.func("screenSelectAll", .{ .covers = &.{Screen.ref("selectAll")} }),
    "screenHasSelection",
    api.func("screenSelectRange", .{ .covers = &.{Screen.ref("select")} }),
    api.func("screenSelectWord", .{ .covers = &.{Screen.ref("selectWord")} }),
    api.func("screenSelectLine", .{ .covers = &.{Screen.ref("selectLine")} }),
    api.func("screenSelectOutput", .{ .covers = &.{Screen.ref("selectOutput")} }),
    api.func("screenSelectionString", .{
        .returns = zigo.result.owned(),
        .covers = &.{Screen.ref("selectionString")},
    }),
    "screenSelection",
    "screenSetSelection",
    "screenViewportTop",
    "screenScrollbar",
    "screenFormat",
    "screenFormatSelection",
    "screenSelectionContains",
    "screenSelectionAdjust",
    api.func("screenStartHyperlink", .{ .covers = &.{Screen.ref("startHyperlink")} }),
}));

// `newSearch` returns by value, like `Terminal.init`: zigo boxes the result
// and frees the box in `close`.
const search_group = Search.members(p.functions(api, .{
    api.func("searchClose", .{ .role = .{ .destructor = Search.typeRef() } }),
    "searchNeedle",
    "searchStatus",
    "searchTick",
    "searchFeed",
    "searchAll",
    "searchSelect",
    "searchMatchCount",
    api.func("searchMatches", .{ .params = &.{p.out(1)} }),
    api.func("searchViewportMatches", .{ .params = &.{p.out(1)} }),
    "searchSelectedMatch",
    "searchSelectedIndex",
}));

// A pin lives in the terminal's page storage and is updated by it, so a
// reference is a child handle and closes first.
const grid_ref_group = GridRef.members(p.functions(api, .{
    api.func("gridRefClose", .{ .role = .{ .destructor = GridRef.typeRef() } }),
    "gridRefHasValue",
    "gridRefPoint",
    "gridRefSet",
    "gridRefCell",
    api.func("gridRefGraphemes", .{ .params = &.{p.outCodepoints(1)} }),
    api.func("gridRefHyperlinkUri", .{ .returns = zigo.result.owned() }),
}));

// Like `Search`, a gesture holds a tracked pin inside its terminal and hands
// it back on every call, so it is a child handle and closes first.
const gesture_group = Gesture.members(p.functions(api, .{
    api.func("gestureClose", .{ .role = .{ .destructor = Gesture.typeRef() } }),
    "gestureSetBehaviors",
    "gestureSetWordBoundaries",
    "gestureSetGeometry",
    "gesturePress",
    "gestureDrag",
    "gestureAutoscroll",
    "gestureAutoscrollTick",
    "gestureDeepPress",
    "gestureRelease",
    "gestureReset",
}));

// A root wrapper, not one of ghostty's own methods on the enum; the receiver
// comes from the owning type of this group and the wrapper's first argument.
const color_name_group = ColorName.members(p.functions(api, .{
    api.func("colorNameDefault", .{ .covers = &.{ColorName.ref("default")} }),
}));

// The terminal and every child it hands out, as one object with one `Close`.
// A terminal refuses to close while a child is open, so the order is a contract
// rather than a preference; the generator writes it, closing children in
// reverse adoption order and the terminal last, joining every error and
// tolerating a second call.
//
// All four children are here. Searches, gestures and grid references are
// shorter-lived than a stream and a caller may well close them by hand, but
// adopting them is what makes forgetting one harmless: `Close` is the single
// place that has to be right. `Search` needs `.accessor` because the default is
// the type name plus `s`, and this one's is not `Searchs`.
const session = zigo.session(.{
    .name = "Session",
    .primary = Terminal.typeRef(),
    .children = &.{
        .{ .type = Stream.typeRef() },
        .{ .type = Search.typeRef(), .accessor = "Searches" },
        .{ .type = Gesture.typeRef() },
        .{ .type = GridRef.typeRef(), .accessor = "GridRefs" },
    },
    .doc =
    \\Session owns a terminal and every handle it handed out, so a caller
    \\holds one thing and closes it once. Adopting a child hands its lifetime
    \\over: `Close` closes the children first, in reverse adoption order,
    \\then the terminal.
    ,
});

pub const declarations = [_]zigo.Entry{
    terminal_group,
    session,
    screen_group,
    search_group,
    grid_ref_group,
    gesture_group,
    color_name_group,
    p.enumeration("PointTag", .{ .text = true }),
    p.printed("GridPoint"),
    p.enumeration("CursorStyle", .{ .text = true }),
    p.enumeration("CursorStyleReq", .{}),
    p.enumeration("EraseDisplay", .{}),
    p.enumeration("EraseLine", .{ .text = true, .open = true }),
    p.enumeration("TabClear", .{ .text = true, .open = true }),
    p.enumeration("ProtectedMode", .{}),
    p.enumeration("ScreenKey", .{}),
    p.enumeration("SwitchScreenMode", .{}),
    p.enumeration("Mode", .{ .text = true, .kit = true }),
    p.printed("Selection"),
    p.enumeration("FormatterFormat", .{ .text = true }),
    api.value("FormatOptions", .{}),
    api.value("RGB", .{ .go = .{
        .type = "RGB",
        .to_raw = "rgbToRaw",
        .from_raw = "rgbFromRaw",
    } }),
    p.enumeration("SelectionAdjustment", .{ .text = true }),
    p.enumeration("Underline", .{ .docs = &.{
        .{ .name = "none", .doc = "No underline. What `SGR 24` resets to." },
        .{ .name = "single", .doc = "One line, the ordinary `SGR 4`." },
        .{ .name = "double", .doc = "Two lines (`SGR 4:2`). Distinct from a doubly-struck glyph." },
        .{ .name = "curly", .doc = "A wave, conventionally used to mark a spelling or syntax error (`SGR 4:3`)." },
        .{ .name = "dotted", .doc = "A dotted line (`SGR 4:4`)." },
        .{ .name = "dashed", .doc = "A dashed line (`SGR 4:5`)." },
    } }),
    api.taggedUnion("Attribute", .{}),
    p.enumeration("SearchDirection", .{}),
    p.enumeration("SearchScroll", .{}),
    p.enumeration("SearchState", .{}),
    p.enumeration("SearchProgress", .{}),
    api.value("Scrollbar", .{}),
    p.enumeration("Charset", .{}),
    p.enumeration("CharsetSlot", .{}),
    p.enumeration("CharsetActiveSlot", .{}),
    p.enumeration("DeccolmMode", .{}),
    api.taggedUnion("ScrollViewport", .{}),
    p.printed("ScrollRegion"),
    p.enumeration("MouseTracking", .{ .text = true }),
    p.enumeration("MouseReportFormat", .{ .text = true }),
    p.enumeration("ModeReport", .{ .text = true }),
    p.enumeration("GestureBehavior", .{ .text = true }),
    p.enumeration("GestureAutoscrollDirection", .{ .text = true }),
    api.value("GestureGeometry", .{}),
    api.value("GesturePressEvent", .{}),
    api.value("GestureDragEvent", .{}),
};
