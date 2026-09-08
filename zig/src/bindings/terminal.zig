//! Terminal state, screens, selection, search and tracked references.
const p = @import("policy.zig");
const Stream = @import("stream.zig").Stream;
const zigo = p.zigo;
const api = p.api;
const gostty = p.gostty;
const convenience = p.convenience;
const flags = p.flags;
const printed = p.printed;
const withMust = p.withMust;
const mustField = p.mustField;
const enumeration = p.enumeration;
const out = p.out;
const outCodepoints = p.outCodepoints;
const childConstructor = p.childConstructor;

// The types, each captured as a context: one declaration that is both the type
// and the scope its members are selected from. `.context()` only captures --
// what registers the type is the `define()` result below reaching
// `.declarations` -- so the lifecycle and the methods are written in one place
// per type without naming it twice.
const Terminal = api.handle("Terminal", .{
    .fields = &.{
        // Read straight off the terminal rather than through ghostty's `cols()`
        // and `rows()`, which is where these two sentences come from: the
        // accessor's doc is not the field's, so it is spelled here.
        mustField(.{ .path = "cols", .doc = "The current column count, read without touching page memory." }),
        mustField(.{ .path = "rows", .doc = "The number of populated rows, read without touching page memory." }),
        mustField(.{ .path = "screens.active.cursor.x", .name = "cursorX" }),
        mustField(.{ .path = "screens.active.cursor.y", .name = "cursorY" }),
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
}).documented(
    \\Terminal owns mutable terminal state. Serialize all calls, including
    \\getters, with calls on its streams, screens, searches, gestures and grid
    \\references. Handle locks protect lifetime only; they do not serialize native
    \\operations. Callbacks may answer their supplied request but must not
    \\recursively feed, resize, reset or close the same terminal.
).use(convenience.plugin, .{ .feature = .terminal_config }).use(p.build_info.plugin, .{
    .simd = gostty.build_features.simd,
    .kitty_graphics = gostty.build_features.kitty_graphics,
    .tmux_control_mode = gostty.build_features.tmux_control_mode,
}).context();

const Screen = api.handle("Screen", .{}).context();

const Search = api.handle("Search", .{}).context();

const GridRef = api.handle("GridRef", .{}).context();

const Gesture = api.handle("Gesture", .{ .fields = &.{
    .{ .path = "inner.left_click_count", .name = "clickCount", .doc = "How many clicks the current sequence is at: 0 before any press, then 1, 2 or 3. What an emulator switches on to decide what a click means." },
    .{ .path = "inner.left_click_dragged", .name = "dragged", .doc = "Whether the pointer has left the pressed cell during this gesture. Read it on release: a click that never dragged is the one that should follow a hyperlink or move the shell cursor, rather than one that happened to end where it started after a round trip." },
} }).context();

const ColorName = enumeration("ColorName", .{ .text = true, .open = true }).context();

// One group per type: the constructor that makes it, the destructor that ends
// it, and everything a caller can do in between.
const terminal_group = Terminal.define(&.{
    // `Terminal.init` returns by value and takes an `Options` whose `colors`
    // field holds optionals that cannot cross the C ABI. `flatten` picks the
    // two fields that can and leaves the rest at their Zig defaults, so
    // ghostty's own constructor is bound without a wrapper.
    Terminal.func("init", .{
        .name = "newTerminal",
        .role = .{ .constructor = .{ .type = Terminal.typeRef() } },
        .params = &.{zigo.param.flatten(2, &.{ "cols", "rows" })},
    }),
    Terminal.func("deinit", .{ .role = .{ .destructor = Terminal.typeRef() } }),

    // The handles a terminal owns. Each borrows it -- a stream's handler
    // reaches back through it for an allocator as it tears down, and a search,
    // a gesture and a grid reference all hold pins inside its page storage --
    // so each is a child that has to close before the terminal does.
    childConstructor("newStream", Stream),
    childConstructor("newSearch", Search),
    childConstructor("newGesture", Gesture),
    childConstructor("newGridRef", GridRef),

    // Printing and the cursor.
    Terminal.func("printString", .{}),
    Terminal.func("plainString", .{ .returns = zigo.result.owned() }),
    Terminal.func("print", .{ .params = &.{.{ .index = 1, .go_name = "c" }} }),
    Terminal.func("printRepeat", .{}),
    Terminal.func("printSlice", .{ .params = &.{.{ .index = 1, .semantic = .codepoint }} }),
    Terminal.func("setCursorStyle", .{}),
    Terminal.func("setCursorPos", .{}),
    withMust(Terminal.func("carriageReturn", .{})),
    Terminal.func("linefeed", .{}),
    withMust(Terminal.func("backspace", .{})),
    withMust(Terminal.func("cursorIsAtPrompt", .{})),
    withMust(Terminal.func("fullReset", .{})),
    withMust(Terminal.func("cursorUp", .{})),
    withMust(Terminal.func("cursorDown", .{})),
    withMust(Terminal.func("cursorLeft", .{})),
    withMust(Terminal.func("cursorRight", .{})),
    Terminal.func("saveCursor", .{}),
    Terminal.func("restoreCursor", .{}),
    Terminal.func("index", .{}),
    Terminal.func("reverseIndex", .{}),

    // Screens. `switchScreen` returns the screen being left, borrowed, or
    // absent when `key` was already active.
    Terminal.func("switchScreen", .{ .returns = zigo.result.borrowed() }),
    Terminal.func("switchScreenMode", .{}),
    api.func("activeScreen", .{ .returns = zigo.result.borrowed() }),
    api.func("screen", .{ .returns = zigo.result.borrowed() }),
    api.func("printAttributesInto", .{
        .params = &.{out(1)},
        .covers = &.{Terminal.ref("printAttributes")},
    }),
    api.func("historyString", .{
        .returns = zigo.result.owned(),
        .covers = &.{Screen.ref("dumpStringAlloc")},
    }),

    // Tab stops.
    Terminal.func("horizontalTab", .{}),
    Terminal.func("horizontalTabBack", .{}),
    Terminal.func("tabSet", .{}),
    Terminal.func("tabReset", .{}),
    Terminal.func("tabClear", .{}),
    api.func("setTabstop", .{}),
    api.func("unsetTabstop", .{}),
    api.func("resetTabstops", .{}),

    // Scrolling and margins.
    Terminal.func("scrollUp", .{}),
    Terminal.func("scrollDown", .{}),
    Terminal.func("setTopAndBottomMargin", .{}),
    Terminal.func("setLeftAndRightMargin", .{}),
    Terminal.func("scrollViewport", .{}),
    api.func("setScrollbackMaxBytes", .{ .covers = &.{Terminal.ref("setScrollbackMaxBytes")} }),
    api.func("clearScrollbackMaxBytes", .{}),
    api.func("setScrollbackMaxLines", .{ .covers = &.{Terminal.ref("setScrollbackMaxLines")} }),
    api.func("clearScrollbackMaxLines", .{}),

    // Editing.
    Terminal.func("insertLines", .{}),
    Terminal.func("deleteLines", .{}),
    Terminal.func("insertBlanks", .{}),
    Terminal.func("deleteChars", .{}),
    Terminal.func("eraseChars", .{}),
    Terminal.func("eraseLine", .{}),
    Terminal.func("eraseDisplay", .{}),
    Terminal.func("decaln", .{}),
    api.func("resize", .{ .covers = &.{Terminal.ref("resize")} }),
    api.func("resizeCells", .{}),

    // Metadata the terminal tracks for the shell.
    api.func("formatTerminal", .{ .name = "Format" }),
    Terminal.func("setPwd", .{}),
    // Both return `?[:0]const u8`, which the utf8 inference does not reach --
    // dropping these two hints turns the Go results from `string` into
    // `[]byte`, so they are spelled rather than left to the default.
    Terminal.func("getPwd", .{ .returns = .{ .semantic = .utf8_string } }),
    Terminal.func("getTitle", .{ .returns = .{ .semantic = .utf8_string } }),
    Terminal.func("setTitle", .{}),

    // Colors and modes: what a program on the pty asks for.
    api.func("backgroundColor", .{}),
    api.func("foregroundColor", .{}),
    api.func("cursorColor", .{}),
    api.func("paletteColors", .{ .params = &.{out(1)} }),
    api.func("modeEnabled", .{}),
    api.func("setMode", .{}),
    api.func("setAttribute", .{ .covers = &.{Terminal.ref("setAttribute")} }),
    Terminal.func("setProtectedMode", .{}),
    Terminal.func("configureCharset", .{}),
    Terminal.func("invokeCharset", .{}),
    Terminal.func("deccolm", .{}),
    Terminal.func("compressionActivity", .{}),

    // Terminal configuration: what the embedder sets underneath whatever a
    // program on the pty asks for.
    api.func("setDefaultBackgroundColor", .{}),
    api.func("setDefaultForegroundColor", .{}),
    api.func("setDefaultCursorColor", .{}),
    Terminal.func("setDefaultCursorStyle", .{}),
    api.func("setDefaultCursorBlink", .{ .covers = &.{Terminal.ref("setDefaultCursorBlink")} }),
    api.func("resetDefaultCursorBlink", .{}),
    api.func("paletteColor", .{}),
    api.func("setPaletteColor", .{}),
    api.func("resetPaletteColor", .{}),
    api.func("resetPalette", .{}),
    api.func("setDefaultPaletteColor", .{}),
    api.func("resetDefaultPalette", .{}),
    api.func("setDefaultMode", .{}),
    api.func("resetModes", .{}),
    api.func("saveMode", .{}),
    api.func("restoreMode", .{}),

    // Kitty graphics limits and the pixels themselves.
    Terminal.func("setKittyGraphicsSizeLimit", .{}),
    api.func("setKittyGraphicsLoadingLimits", .{
        .covers = &.{Terminal.ref("setKittyGraphicsLoadingLimits")},
    }),
    api.func("kittyImage", .{}),
    api.func("kittyImageData", .{ .params = &.{out(2)} }),

    // State reads: the gets for state the bindings could already set, plus the
    // resolved values no single mode flag reports.
    api.func("scrollRegion", .{}),
    api.func("charset", .{}),
    api.func("mouseTracking", .{}),
    api.func("mouseTrackingSendsMotion", .{}),
    api.func("mouseReportFormat", .{}),
    api.func("modeReport", .{}),

    // The untracked one-off reads; `GridRef` is the tracked form.
    api.func("cellAt", .{}),
    api.func("hyperlinkAt", .{ .returns = zigo.result.owned() }),
});

const screen_group = Screen.define(&.{
    // ghostty takes the screen by value here; zigo passes the handle and the
    // shim copies, so no wrapper is needed.
    withMust(Screen.func("viewportIsBottom", .{})),
    Screen.func("clearSelection", .{}),
    Screen.func("endHyperlink", .{}),
    // Wrappers whose receiver zigo infers from their first argument. The
    // shared prefix is what keeps them apart in Zig; the Go name drops it.
    api.func("screenSelectAll", .{ .name = "selectAll", .covers = &.{Screen.ref("selectAll")} }),
    api.func("screenHasSelection", .{ .name = "hasSelection" }),
    api.func("screenSelectRange", .{ .name = "selectRange", .covers = &.{Screen.ref("select")} }),
    api.func("screenSelectWord", .{ .name = "selectWord", .covers = &.{Screen.ref("selectWord")} }),
    api.func("screenSelectLine", .{ .name = "selectLine", .covers = &.{Screen.ref("selectLine")} }),
    api.func("screenSelectOutput", .{ .name = "selectOutput", .covers = &.{Screen.ref("selectOutput")} }),
    api.func("screenSelectionString", .{
        .name = "selectionString",
        .returns = zigo.result.owned(),
        .covers = &.{Screen.ref("selectionString")},
    }),
    api.func("screenSelection", .{ .name = "selection" }),
    api.func("screenSetSelection", .{ .name = "setSelection" }),
    api.func("screenViewportTop", .{ .name = "viewportTop" }),
    api.func("screenScrollbar", .{ .name = "scrollbar" }),
    api.func("screenFormat", .{ .name = "format" }),
    api.func("screenFormatSelection", .{ .name = "formatSelection" }),
    api.func("screenSelectionContains", .{ .name = "selectionContains" }),
    api.func("screenSelectionAdjust", .{ .name = "selectionAdjust" }),
    api.func("screenStartHyperlink", .{ .name = "startHyperlink", .covers = &.{Screen.ref("startHyperlink")} }),
});

// `newSearch` returns by value, like `Terminal.init`: zigo boxes the result
// and frees the box in `close`.
const search_group = Search.define(&.{
    api.func("searchClose", .{ .role = .{ .destructor = Search.typeRef() } }),
    api.func("searchNeedle", .{ .name = "needle" }),
    api.func("searchStatus", .{ .name = "status" }),
    api.func("searchTick", .{ .name = "tick" }),
    api.func("searchFeed", .{ .name = "feed" }),
    api.func("searchAll", .{ .name = "all" }),
    api.func("searchSelect", .{ .name = "select" }),
    api.func("searchMatchCount", .{ .name = "matchCount" }),
    api.func("searchMatches", .{ .name = "matches", .params = &.{out(1)} }),
    api.func("searchViewportMatches", .{ .name = "viewportMatches", .params = &.{out(1)} }),
    api.func("searchSelectedMatch", .{ .name = "selectedMatch" }),
    api.func("searchSelectedIndex", .{ .name = "selectedIndex" }),
});

// A pin lives in the terminal's page storage and is updated by it, so a
// reference is a child handle and closes first.
const grid_ref_group = GridRef.define(&.{
    api.func("gridRefClose", .{ .role = .{ .destructor = GridRef.typeRef() } }),
    api.func("gridRefHasValue", .{ .name = "hasValue" }),
    api.func("gridRefPoint", .{ .name = "point" }),
    api.func("gridRefSet", .{ .name = "set" }),
    api.func("gridRefCell", .{ .name = "cell" }),
    api.func("gridRefGraphemes", .{ .name = "graphemes", .params = &.{outCodepoints(1)} }),
    api.func("gridRefHyperlinkUri", .{ .name = "hyperlinkUri", .returns = zigo.result.owned() }),
});

// Like `Search`, a gesture holds a tracked pin inside its terminal and hands
// it back on every call, so it is a child handle and closes first.
const gesture_group = Gesture.define(&.{
    api.func("gestureClose", .{ .role = .{ .destructor = Gesture.typeRef() } }),
    api.func("gestureSetBehaviors", .{ .name = "setBehaviors" }),
    api.func("gestureSetWordBoundaries", .{ .name = "setWordBoundaries" }),
    api.func("gestureSetGeometry", .{ .name = "setGeometry" }),
    api.func("gesturePress", .{ .name = "press" }),
    api.func("gestureDrag", .{ .name = "drag" }),
    api.func("gestureAutoscroll", .{ .name = "autoscroll" }),
    api.func("gestureAutoscrollTick", .{ .name = "autoscrollTick" }),
    api.func("gestureDeepPress", .{ .name = "deepPress" }),
    api.func("gestureRelease", .{ .name = "release" }),
    api.func("gestureReset", .{ .name = "reset" }),
});

// A root wrapper, not one of ghostty's own methods on the enum; the receiver
// comes from the owning type of this group and the wrapper's first argument.
const color_name_group = ColorName.define(&.{
    api.func("colorNameDefault", .{
        .name = "default",
        .covers = &.{ColorName.ref("default")},
    }),
});

pub const declarations = [_]zigo.Entry{
    terminal_group,
    screen_group,
    search_group,
    grid_ref_group,
    gesture_group,
    color_name_group,
    enumeration("PointTag", .{ .text = true }),
    printed("GridPoint"),
    enumeration("CursorStyle", .{ .text = true }),
    enumeration("CursorStyleReq", .{}),
    enumeration("EraseDisplay", .{}),
    enumeration("EraseLine", .{ .text = true, .open = true }),
    enumeration("TabClear", .{ .text = true, .open = true }),
    enumeration("ProtectedMode", .{}),
    enumeration("ScreenKey", .{}),
    enumeration("SwitchScreenMode", .{}),
    enumeration("Mode", .{ .text = true, .kit = true }),
    printed("Selection"),
    enumeration("FormatterFormat", .{ .text = true }),
    api.val("FormatOptions", .{}),
    enumeration("SelectionAdjustment", .{ .text = true }),
    enumeration("Underline", .{}),
    api.taggedUnion("Attribute", .{}),
    enumeration("SearchDirection", .{}),
    enumeration("SearchScroll", .{}),
    enumeration("SearchState", .{}),
    enumeration("SearchProgress", .{}),
    api.val("Scrollbar", .{}),
    enumeration("Charset", .{}),
    enumeration("CharsetSlot", .{}),
    enumeration("CharsetActiveSlot", .{}),
    enumeration("DeccolmMode", .{}),
    api.taggedUnion("ScrollViewport", .{}),
    printed("ScrollRegion"),
    enumeration("MouseTracking", .{ .text = true }),
    enumeration("MouseReportFormat", .{ .text = true }),
    enumeration("ModeReport", .{ .text = true }),
    enumeration("GestureBehavior", .{ .text = true }),
    enumeration("GestureAutoscrollDirection", .{ .text = true }),
    api.val("GestureGeometry", .{}),
    api.val("GesturePressEvent", .{}),
    api.val("GestureDragEvent", .{}),
};
