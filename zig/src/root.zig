//! The libghostty-vt surface exposed to Go.
//!
//! `Terminal`, `Screen`, `Search` and `RenderState` are ghostty's own types,
//! bound directly: their methods become Go methods without any wrapper. This
//! module adds only what ghostty does not provide and zigo needs:
//!
//!   - an `std.Io` value for `.io` injection (ghostty ships `TinyIo` as a type,
//!     not as a ready-made `Io` declaration);
//!   - a release function for the strings bound with `.returns = .caller`;
//!   - wrappers where ghostty's signature cannot cross the C ABI (page pins,
//!     nested optional structs, generic functions), each saying why.
//!
//! Plain field reads are not here: `.fields` in `bindings.zig` generates those
//! accessors from the field path.
//!
//! Every `gpa` / `io_impl` parameter below is filled in by zigo's `.allocator`
//! and `.io` injection and never appears in the C or Go signatures.
const std = @import("std");
const vt = @import("ghostty_vt");

const Allocator = std.mem.Allocator;

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
/// program reading the pty expects. `encodeFocus` and `isSafePaste` are
/// bound straight out of here; the others take a `Terminal` and need the
/// wrappers below.
pub const input = vt.input;

/// Releases a string a bound function handed out with `.returns = .caller`:
/// `plainString`, `selectionString`, `historyString`, `renderHyperlinkAt`.
pub fn freeString(gpa: Allocator, str: []const u8) void {
    gpa.free(str);
}

/// Answers a system request. `len` is the size of the request: the PNG's
/// byte count, or how many random bytes are wanted. Returning without a
/// reply fails the operation that needed it. `userdata` is last: that is
/// where zigo expects the handle it passes back.
pub const SysFn = *const fn (len: usize, userdata: usize) callconv(.c) void;

/// The hooks, grouped so that `clear` releases both callbacks at once. Bound
/// as `root.sys.*`.
/// The hooks themselves, grouped so that one `clear` releases both callbacks.
pub const sys = @import("sys.zig");

// -- The surface -------------------------------------------------------------
//
// One `pub` per declaration `bindings.zig` names, so that every path stays
// `root.<name>` and this file alone answers "what does Go see". The
// implementation and the prose explaining each wrapper live in the file the
// group names.

// Core aliases and the injected `std.Io`.
const common_ = @import("common.zig");

pub const io = common_.io;
pub const Terminal = common_.Terminal;
pub const CursorStyle = common_.CursorStyle;
pub const CursorStyleReq = common_.CursorStyleReq;
pub const ScreenKey = common_.ScreenKey;
pub const Screen = common_.Screen;
pub const Underline = common_.Underline;

// The VT stream: bytes in, terminal state and events out.
const stream_ = @import("stream.zig");

pub const StreamEvent = stream_.StreamEvent;
pub const ProgressState = stream_.ProgressState;
pub const ColorScheme = stream_.ColorScheme;
pub const ClipboardLocation = stream_.ClipboardLocation;
pub const ClipboardDenial = stream_.ClipboardDenial;
pub const ClipboardFn = stream_.ClipboardFn;
pub const Stream = stream_.Stream;
pub const newStream = stream_.newStream;
pub const freeStream = stream_.freeStream;

// Screens and selections.
const screen_ = @import("screen.zig");

pub const SwitchScreenMode = screen_.SwitchScreenMode;
pub const activeScreen = screen_.activeScreen;
pub const screen = screen_.screen;
pub const screenSelectAll = screen_.screenSelectAll;
pub const screenSelectRange = screen_.screenSelectRange;
pub const Selection = screen_.Selection;
pub const screenSelection = screen_.screenSelection;
pub const screenSetSelection = screen_.screenSetSelection;
pub const SelectionAdjustment = screen_.SelectionAdjustment;
pub const screenSelectionContains = screen_.screenSelectionContains;
pub const screenSelectionAdjust = screen_.screenSelectionAdjust;
pub const screenViewportTop = screen_.screenViewportTop;
pub const Scrollbar = screen_.Scrollbar;
pub const screenScrollbar = screen_.screenScrollbar;

// Kitty drag and drop (OSC 72).
const dnd_ = @import("dnd.zig");

pub const DragEvent = dnd_.DragEvent;
pub const DragOperation = dnd_.DragOperation;
pub const DragOperations = dnd_.DragOperations;
pub const DragMove = dnd_.DragMove;
pub const DragFn = dnd_.DragFn;
pub const onDrag = dnd_.onDrag;
pub const dragEvent = dnd_.dragEvent;
pub const dragAccepted = dnd_.dragAccepted;
pub const dragActive = dnd_.dragActive;
pub const dragRegisteredMimes = dnd_.dragRegisteredMimes;
pub const dragClientAccepted = dnd_.dragClientAccepted;
pub const dragMove = dnd_.dragMove;
pub const dragLeave = dnd_.dragLeave;
pub const dragAddItem = dnd_.dragAddItem;
pub const dragDrop = dnd_.dragDrop;
pub const dragClearItems = dnd_.dragClearItems;

// Text search over a terminal.
const search_ = @import("search.zig");

pub const Search = search_.Search;
pub const SearchDirection = search_.SearchDirection;
pub const SearchScroll = search_.SearchScroll;
pub const SearchState = search_.SearchState;
pub const SearchProgress = search_.SearchProgress;
pub const newSearch = search_.newSearch;
pub const searchClose = search_.searchClose;
pub const searchNeedle = search_.searchNeedle;
pub const searchStatus = search_.searchStatus;
pub const searchTick = search_.searchTick;
pub const searchFeed = search_.searchFeed;
pub const searchAll = search_.searchAll;
pub const searchSelect = search_.searchSelect;
pub const searchMatchCount = search_.searchMatchCount;
pub const searchMatches = search_.searchMatches;
pub const searchViewportMatches = search_.searchViewportMatches;
pub const searchSelectedMatch = search_.searchSelectedMatch;
pub const searchSelectedIndex = search_.searchSelectedIndex;
pub const screenSelectWord = screen_.screenSelectWord;
pub const screenSelectLine = screen_.screenSelectLine;
pub const screenSelectOutput = screen_.screenSelectOutput;
pub const screenHasSelection = screen_.screenHasSelection;
pub const screenSelectionString = screen_.screenSelectionString;
pub const screenStartHyperlink = screen_.screenStartHyperlink;

// Terminal-wide state: size, colours, modes, attributes, limits.
const terminal_ = @import("terminal.zig");

pub const ProtectedMode = terminal_.ProtectedMode;
pub const Charset = terminal_.Charset;
pub const CharsetSlot = terminal_.CharsetSlot;
pub const CharsetActiveSlot = terminal_.CharsetActiveSlot;
pub const DeccolmMode = terminal_.DeccolmMode;
pub const ScrollViewport = terminal_.ScrollViewport;
pub const EraseDisplay = terminal_.EraseDisplay;
pub const EraseLine = terminal_.EraseLine;
pub const TabClear = terminal_.TabClear;
pub const resize = terminal_.resize;
pub const resizeCells = terminal_.resizeCells;
pub const printAttributesInto = terminal_.printAttributesInto;
pub const historyString = terminal_.historyString;
pub const ColorName = terminal_.ColorName;
pub const colorNameDefault = terminal_.colorNameDefault;
pub const Attribute = terminal_.Attribute;
pub const setAttribute = terminal_.setAttribute;
pub const setDefaultCursorBlink = terminal_.setDefaultCursorBlink;
pub const resetDefaultCursorBlink = terminal_.resetDefaultCursorBlink;
pub const setScrollbackMaxBytes = terminal_.setScrollbackMaxBytes;
pub const clearScrollbackMaxBytes = terminal_.clearScrollbackMaxBytes;
pub const setScrollbackMaxLines = terminal_.setScrollbackMaxLines;
pub const clearScrollbackMaxLines = terminal_.clearScrollbackMaxLines;
pub const backgroundColor = terminal_.backgroundColor;
pub const foregroundColor = terminal_.foregroundColor;
pub const cursorColor = terminal_.cursorColor;
pub const paletteColors = terminal_.paletteColors;
pub const setDefaultBackgroundColor = terminal_.setDefaultBackgroundColor;
pub const setDefaultForegroundColor = terminal_.setDefaultForegroundColor;
pub const setDefaultCursorColor = terminal_.setDefaultCursorColor;
pub const Mode = terminal_.Mode;
pub const modeEnabled = terminal_.modeEnabled;
pub const setMode = terminal_.setMode;

// Screen contents as text, VT sequences or HTML.
const format_ = @import("format.zig");

pub const FormatterFormat = format_.FormatterFormat;
pub const FormatOptions = format_.FormatOptions;
pub const formatTerminal = format_.formatTerminal;
pub const screenFormat = format_.screenFormat;
pub const screenFormatSelection = format_.screenFormatSelection;

// Saving and restoring a whole terminal.
const snapshot_ = @import("snapshot.zig");

pub const Snapshot = snapshot_.Snapshot;
pub const decodeSnapshot = snapshot_.decodeSnapshot;
pub const snapshotRestoreInto = snapshot_.snapshotRestoreInto;
pub const snapshotContinuation = snapshot_.snapshotContinuation;
pub const SnapshotDecoder = snapshot_.SnapshotDecoder;
pub const SnapshotProgress = snapshot_.SnapshotProgress;
pub const newSnapshotDecoder = snapshot_.newSnapshotDecoder;
pub const freeSnapshotDecoder = snapshot_.freeSnapshotDecoder;
pub const snapshotDecoderReady = snapshot_.snapshotDecoderReady;
pub const snapshotDecoderRestoreInto = snapshot_.snapshotDecoderRestoreInto;
pub const snapshotDecoderContinuation = snapshot_.snapshotDecoderContinuation;
pub const snapshotDecoderNext = snapshot_.snapshotDecoderNext;

// Key, mouse and paste events into pty bytes.
const encode_ = @import("encode.zig");

pub const Key = encode_.Key;
pub const KeyAction = encode_.KeyAction;
pub const keyFromASCII = encode_.keyFromASCII;
pub const keyCodepoint = encode_.keyCodepoint;
pub const keyPrintable = encode_.keyPrintable;
pub const keyModifier = encode_.keyModifier;
pub const keyKeypad = encode_.keyKeypad;
pub const keyLeftOrRightShift = encode_.keyLeftOrRightShift;
pub const keyLeftOrRightAlt = encode_.keyLeftOrRightAlt;
pub const keyCtrlOrSuper = encode_.keyCtrlOrSuper;
pub const keyShouldBeRemappable = encode_.keyShouldBeRemappable;
pub const keyW3C = encode_.keyW3C;
pub const keyFromW3C = encode_.keyFromW3C;
pub const FocusEvent = encode_.FocusEvent;
pub const KeyMods = encode_.KeyMods;
pub const KeyEvent = encode_.KeyEvent;
pub const encodeKey = encode_.encodeKey;
pub const MouseAction = encode_.MouseAction;
pub const MouseButton = encode_.MouseButton;
pub const RenderSize = encode_.RenderSize;
pub const MouseEvent = encode_.MouseEvent;
pub const encodeMouse = encode_.encodeMouse;
pub const encodePaste = encode_.encodePaste;

// One frame of the viewport, flattened for a renderer.
const render_ = @import("render.zig");

pub const RenderState = render_.RenderState;
pub const CellWidth = render_.CellWidth;
pub const CellFlags = render_.CellFlags;
pub const RenderCell = render_.RenderCell;
pub const newRenderState = render_.newRenderState;
pub const renderCellCount = render_.renderCellCount;
pub const renderCells = render_.renderCells;
pub const renderRowCells = render_.renderRowCells;
pub const RenderDirty = render_.RenderDirty;
pub const renderDirty = render_.renderDirty;
pub const renderDirtyRows = render_.renderDirtyRows;
pub const renderGraphemes = render_.renderGraphemes;
pub const renderHyperlinkAt = render_.renderHyperlinkAt;
pub const renderBackground = render_.renderBackground;
pub const renderForeground = render_.renderForeground;
pub const renderCursorX = render_.renderCursorX;
pub const renderCursorY = render_.renderCursorY;
pub const renderCursorWideTail = render_.renderCursorWideTail;
pub const renderCursorColor = render_.renderCursorColor;

// Kitty graphics, snapshotted per frame.
const kitty_ = @import("kitty.zig");

pub const KittyFormat = kitty_.KittyFormat;
pub const KittyCompression = kitty_.KittyCompression;
pub const KittyLayer = kitty_.KittyLayer;
pub const KittyPlacement = kitty_.KittyPlacement;
pub const KittyImages = kitty_.KittyImages;
pub const newKittyImages = kitty_.newKittyImages;
pub const freeKittyImages = kitty_.freeKittyImages;
pub const kittyUpdate = kitty_.kittyUpdate;
pub const kittyPlacementCount = kitty_.kittyPlacementCount;
pub const kittyPlacements = kitty_.kittyPlacements;
pub const KittyImage = kitty_.KittyImage;
pub const kittyImage = kitty_.kittyImage;
pub const kittyImageData = kitty_.kittyImageData;

// Click, drag and autoscroll: the state machine behind text selection.
const gesture_ = @import("gesture.zig");

pub const Gesture = gesture_.Gesture;
pub const GestureBehavior = gesture_.GestureBehavior;
pub const GestureAutoscrollDirection = gesture_.GestureAutoscrollDirection;
pub const GestureGeometry = gesture_.GestureGeometry;
pub const GesturePressEvent = gesture_.GesturePressEvent;
pub const GestureDragEvent = gesture_.GestureDragEvent;
pub const newGesture = gesture_.newGesture;
pub const gestureClose = gesture_.gestureClose;
pub const gestureSetBehaviors = gesture_.gestureSetBehaviors;
pub const gestureSetWordBoundaries = gesture_.gestureSetWordBoundaries;
pub const gestureSetGeometry = gesture_.gestureSetGeometry;
pub const gesturePress = gesture_.gesturePress;
pub const gestureDrag = gesture_.gestureDrag;
pub const gestureAutoscroll = gesture_.gestureAutoscroll;
pub const gestureAutoscrollTick = gesture_.gestureAutoscrollTick;
pub const gestureDeepPress = gesture_.gestureDeepPress;
pub const gestureRelease = gesture_.gestureRelease;
pub const gestureReset = gesture_.gestureReset;
pub const gestureClickCount = gesture_.gestureClickCount;
pub const gestureDragged = gesture_.gestureDragged;

// Reads of terminal state the bindings could otherwise only write.
const state_ = @import("state.zig");

pub const ScrollRegion = state_.ScrollRegion;
pub const scrollRegion = state_.scrollRegion;
pub const charset = state_.charset;
pub const charsetGL = state_.charsetGL;
pub const charsetGR = state_.charsetGR;
pub const charsetSingleShift = state_.charsetSingleShift;
pub const protectedMode = state_.protectedMode;
pub const MouseTracking = state_.MouseTracking;
pub const mouseTracking = state_.mouseTracking;
pub const mouseTrackingSendsMotion = state_.mouseTrackingSendsMotion;
pub const MouseReportFormat = state_.MouseReportFormat;
pub const mouseReportFormat = state_.mouseReportFormat;
pub const ModeReport = state_.ModeReport;
pub const modeReport = state_.modeReport;

// Configuration the embedder owns: the palette, mode defaults, tabstops and
// which mediums a Kitty image may arrive over.
const config_ = @import("config.zig");

pub const paletteColor = config_.paletteColor;
pub const setPaletteColor = config_.setPaletteColor;
pub const resetPaletteColor = config_.resetPaletteColor;
pub const resetPalette = config_.resetPalette;
pub const setDefaultPaletteColor = config_.setDefaultPaletteColor;
pub const resetDefaultPalette = config_.resetDefaultPalette;
pub const setDefaultMode = config_.setDefaultMode;
pub const resetModes = config_.resetModes;
pub const saveMode = config_.saveMode;
pub const restoreMode = config_.restoreMode;
pub const setTabstop = config_.setTabstop;
pub const unsetTabstop = config_.unsetTabstop;
pub const resetTabstops = config_.resetTabstops;
pub const setKittyGraphicsLoadingLimits = config_.setKittyGraphicsLoadingLimits;

// The standalone OSC and SGR parsers: a sequence in, what it said out, with no
// terminal behind them.
//
// `OSCParser`'s own methods are bound as `root.OSCParser.<name>` rather than
// through one alias each. zigo checks every public declaration in this file
// against every binding entry at comptime, so the cost of the surface grows
// with the product of the two, and eighteen more names here is enough to
// exhaust Zig's branch budget inside the generator. Reaching the methods
// through the type they already live on costs nothing.
const parser_ = @import("parser.zig");

pub const OSCParser = parser_.OSCParser;
pub const OSCCommand = parser_.OSCCommand;
pub const OSCTerminator = parser_.OSCTerminator;
pub const SemanticPromptAction = parser_.SemanticPromptAction;
pub const newOSCParser = parser_.newOSCParser;
pub const freeOSCParser = parser_.freeOSCParser;
pub const SgrAttributeTag = parser_.SgrAttributeTag;
pub const SgrAttribute = parser_.SgrAttribute;
pub const sgrAttributeCount = parser_.sgrAttributeCount;
pub const sgrAttributes = parser_.sgrAttributes;
