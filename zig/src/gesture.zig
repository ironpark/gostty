//! The click, drag and autoscroll state machine behind text selection.
//!
//! The primitives on `Screen` say what a word or a line is; this says what a
//! mouse does. It counts clicks into single, double and triple, remembers the
//! cell the gesture started on as a tracked pin so terminal output cannot move
//! it out from under the drag, picks word or line granularity from the click
//! count, and reports when a drag has left the surface and the viewport should
//! be scrolled. Every call answers with a `Selection` to apply, or with nothing
//! when the gesture produces none -- a first press, in particular, clears the
//! selection rather than making one.
//!
//! What ghostty passes per event, this keeps: the click-count behaviors, the
//! word boundary codepoints and the surface geometry are configuration that
//! changes on a config reload or a resize, not on every mouse move, so they are
//! set once and the event calls stay short enough to be read at the call site.
const std = @import("std");
const vt = @import("ghostty_vt");
const common = @import("common.zig");
const screen_mod = @import("screen.zig");

const Allocator = std.mem.Allocator;
const Terminal = common.Terminal;
const Screen = common.Screen;
const Selection = screen_mod.Selection;
const Inner = vt.SelectionGesture;

/// How a click and the drag after it select.
///
/// A plain mirror of ghostty's `SelectionGesture.Behavior`: that one is built by
/// `lib.Enum`, which cannot be a field of an extern struct or be registered as a
/// Go enum, so the two are converted at the boundary.
pub const GestureBehavior = enum(u8) {
    /// Cell-granular drag. A press of its own selects nothing, which is what
    /// makes a single click clear the selection.
    cell,
    /// The word under the press, then word-granular drag.
    word,
    /// The line under the press, then line-granular drag.
    line,
    /// The shell command output the press is inside, which needs OSC 133
    /// prompt marks to exist at all.
    output,

    fn toGhostty(self: GestureBehavior) Inner.Behavior {
        return common.mirror(Inner.Behavior, self);
    }
};

/// Which way a drag that has left the surface wants the viewport scrolled.
///
/// While this is not `none`, run a timer -- roughly every 15ms is what ghostty
/// suggests -- calling `autoscrollTick` with the last pointer position.
pub const GestureAutoscrollDirection = enum(u8) {
    none,
    up,
    down,
};

/// The rendered geometry a drag is interpreted against.
///
/// Selection flips to the next cell at 60% across it, so a drag needs to know
/// how wide a cell is and where the grid starts; autoscroll needs to know where
/// the bottom edge is. Set this once, and again on resize.
pub const GestureGeometry = extern struct {
    /// Columns in the rendered grid.
    columns: u32 = 0,
    /// One cell's width in surface pixels.
    cell_width: u32 = 0,
    /// Padding before the first column, in surface pixels.
    padding_left: u32 = 0,
    /// The surface height in pixels, which is where a downward drag starts
    /// asking for autoscroll.
    screen_height: u32 = 0,

    fn toGhostty(self: GestureGeometry) Inner.Drag.Geometry {
        return .{
            .columns = self.columns,
            .cell_width = self.cell_width,
            .padding_left = self.padding_left,
            .screen_height = self.screen_height,
        };
    }
};

/// A press of the primary button.
pub const GesturePressEvent = extern struct {
    /// The cell under the pointer, in viewport coordinates.
    x: u16 = 0,
    y: u16 = 0,
    /// The pointer in surface pixels from the top left. Kept apart from the
    /// cell because both the multi-click distance test and the within-cell
    /// selection threshold need sub-cell precision.
    xpos: f64 = 0,
    ypos: f64 = 0,
    /// How far a second press may be from the first and still count as a
    /// repeat. One cell width is a reasonable choice.
    max_distance: f64 = 0,
    /// How long after a press a second one still counts as a repeat. Zero
    /// disables multi-click entirely: every press is then a fresh single
    /// click, which is also the honest setting for a caller with no clock.
    repeat_interval_ns: u64 = 0,
    /// When the press happened, in nanoseconds on a monotonic clock.
    ///
    /// Passed in rather than read from a clock here: the embedder already has
    /// the timestamp the window system delivered with the event, that one is
    /// the truth about when the user clicked (a queued event can reach the
    /// terminal much later), and a test can drive double clicks without
    /// sleeping. Only differences matter, so any epoch will do. Time that runs
    /// backwards ends the click sequence.
    time_ns: i64 = 0,
};

/// A pointer movement while the primary button is down, and the same payload an
/// autoscroll tick repeats.
pub const GestureDragEvent = extern struct {
    /// The cell under the pointer, in viewport coordinates. For an autoscroll
    /// tick this is resolved after the viewport moves, so it names the row that
    /// scrolled under the pointer.
    x: u16 = 0,
    y: u16 = 0,
    /// The pointer in surface pixels from the top left.
    xpos: f64 = 0,
    ypos: f64 = 0,
    /// Select the block between the two corners rather than the flow of text.
    rectangle: bool = false,
};

/// A selection gesture over one terminal.
///
/// Holds the terminal for the same reason `Search` does: ghostty takes it back
/// on every call including `deinit`, so a gesture that outlived its terminal
/// could not be closed. Close it before the terminal.
///
/// One gesture tracks one pointer. It is not safe to use concurrently with
/// itself or with terminal writes.
pub const Gesture = struct {
    inner: Inner,
    terminal: *Terminal,
    gpa: Allocator,
    behaviors: [3]Inner.Behavior,
    /// Owned copy: ghostty borrows the boundary set on every call that reads a
    /// word, and the caller's slice does not have to outlive the call.
    boundaries: []u21,
    geometry: Inner.Drag.Geometry,
};

/// Start a selection gesture over `t`, with ghostty's standard click behaviors:
/// single click clears, double selects a word, triple selects a line.
///
/// Returned by value, like `Terminal.init`: zigo boxes it and frees the box in
/// `Close`.
pub fn newGesture(t: *Terminal, gpa: Allocator) Gesture {
    return .{
        .inner = .init,
        .terminal = t,
        .gpa = gpa,
        .behaviors = Inner.default_behaviors,
        .boundaries = &.{},
        .geometry = .{ .columns = 0, .cell_width = 0, .padding_left = 0, .screen_height = 0 },
    };
}

/// Release the gesture and the tracked pin it holds inside the terminal's page
/// storage. Must happen before the terminal is closed.
pub fn gestureClose(self: *Gesture) void {
    self.inner.deinit(self.terminal);
    self.gpa.free(self.boundaries);
    self.boundaries = &.{};
}

/// What a single, double and triple click select.
///
/// Defaults to ghostty's own mapping, so this is only for an emulator offering
/// something else -- selecting command output on triple click, say. Takes
/// effect on the next press; a gesture already in progress keeps the behavior
/// its press chose.
pub fn gestureSetBehaviors(
    self: *Gesture,
    single_click: GestureBehavior,
    double_click: GestureBehavior,
    triple_click: GestureBehavior,
) void {
    self.behaviors = .{ single_click.toGhostty(), double_click.toGhostty(), triple_click.toGhostty() };
}

/// The codepoints that end a word, for double-click and deep-press selection.
///
/// ghostty has no default on purpose -- its own UI reads the set from
/// configuration -- so this starts empty, and until it is set a "word" runs to
/// the end of the line. A space is the minimum worth setting. Copied, so the
/// caller's slice does not have to outlive the call; set it again on a config
/// reload.
pub fn gestureSetWordBoundaries(self: *Gesture, boundaries: []const u21) Allocator.Error!void {
    const copy = try self.gpa.dupe(u21, boundaries);
    self.gpa.free(self.boundaries);
    self.boundaries = copy;
}

/// The rendered geometry drags are measured against. Set it before the first
/// drag, and again on every resize; a zero `columns` or `cell_width` makes
/// cell-granular drags select nothing.
pub fn gestureSetGeometry(self: *Gesture, geometry: GestureGeometry) void {
    self.geometry = geometry.toGhostty();
}

/// Record a press and return the selection it makes, if any.
///
/// A press near the previous one and within `repeat_interval_ns` of it raises
/// the click count, up to three; anything else starts over at one. Nothing is
/// selected here: apply the result with `Screen.SetSelection` when there is one
/// and call `Screen.ClearSelection` when there is not, which is what makes a
/// single click clear the selection.
///
/// Nothing is returned and no gesture starts if the cell is outside the
/// viewport.
pub fn gesturePress(self: *Gesture, p: GesturePressEvent) !?Selection {
    const t = self.terminal;
    const pin = t.screens.active.pages.pin(.{
        .viewport = .{ .x = p.x, .y = p.y },
    }) orelse return null;
    const sel = try self.inner.press(t, .{
        // A zero interval cannot admit any repeat, so it is spelled as the
        // clockless case ghostty already handles rather than as an interval no
        // press can fall inside.
        .time = if (p.repeat_interval_ns == 0) null else .{ .nanoseconds = p.time_ns },
        .pin = pin,
        .xpos = p.xpos,
        .ypos = p.ypos,
        .max_distance = p.max_distance,
        .repeat_interval = p.repeat_interval_ns,
        .word_boundary_codepoints = self.boundaries,
        .behaviors = &self.behaviors,
    }) orelse return null;
    return selectionOf(t, sel);
}

/// Record a pointer movement while the button is down and return the selection
/// it now covers, if any.
///
/// Null means there is nothing to change: no gesture is in progress, the press
/// it started from is no longer on the active screen, or the pointer has not
/// yet moved far enough to select the first cell. Word and line drags are
/// recomputed against the terminal as it is now, so output arriving mid-drag is
/// picked up.
///
/// Check `Autoscroll` afterwards: a drag past the top or bottom edge asks for a
/// timer calling `AutoscrollTick`.
pub fn gestureDrag(self: *Gesture, d: GestureDragEvent) ?Selection {
    const t = self.terminal;
    const pin = t.screens.active.pages.pin(.{
        .viewport = .{ .x = d.x, .y = d.y },
    }) orelse return null;
    const sel = self.inner.drag(t, .{
        .pin = pin,
        .xpos = d.xpos,
        .ypos = d.ypos,
        .rectangle = d.rectangle,
        .word_boundary_codepoints = self.boundaries,
        .geometry = self.geometry,
    }) orelse return null;
    return selectionOf(t, sel);
}

/// Which way an active drag wants the viewport scrolled, `none` when it does
/// not. Read it after every `Drag` to start or stop the autoscroll timer.
pub fn gestureAutoscroll(self: *Gesture) GestureAutoscrollDirection {
    return common.mirror(GestureAutoscrollDirection, self.inner.left_drag_autoscroll);
}

/// Scroll the viewport one row in the autoscroll direction and continue the
/// drag at the pointer's current position, which now names a different row.
///
/// Call this from the timer `Autoscroll` asked for, passing the last position
/// the pointer was seen at. It scrolls exactly one row per call: tick faster to
/// scroll faster. A null result with `Autoscroll` back at `none` means the
/// gesture ended -- stop the timer and leave the selection alone.
pub fn gestureAutoscrollTick(self: *Gesture, d: GestureDragEvent) ?Selection {
    const t = self.terminal;
    const sel = self.inner.autoscrollTick(t, .{
        .viewport = .{ .x = d.x, .y = d.y },
        .xpos = d.xpos,
        .ypos = d.ypos,
        .rectangle = d.rectangle,
        .word_boundary_codepoints = self.boundaries,
        .geometry = self.geometry,
    }) orelse return null;
    return selectionOf(t, sel);
}

/// Record a force click -- a pressure activation while the button is already
/// down, which is what macOS calls a deep click.
///
/// Selects the word under the original press and ends the gesture, so further
/// movement does not drag it. Null when there is no valid gesture to deepen.
pub fn gestureDeepPress(self: *Gesture) ?Selection {
    const t = self.terminal;
    const sel = self.inner.deepPress(t, .{
        .word_boundary_codepoints = self.boundaries,
    }) orelse return null;
    return selectionOf(t, sel);
}

/// Record the release of the primary button at a viewport cell.
///
/// This ends the drag and stops autoscroll but deliberately keeps the click
/// count, which is what lets the next press become a double or triple click.
/// A cell outside the viewport is fine: the gesture then records that the
/// pointer moved away, which is the conservative answer for `Dragged`.
pub fn gestureRelease(self: *Gesture, x: u16, y: u16) void {
    const t = self.terminal;
    self.inner.release(t, .{
        .pin = t.screens.active.pages.pin(.{ .viewport = .{ .x = x, .y = y } }),
    });
}

/// Abandon the gesture, click sequence included.
///
/// For cancellation rather than the ordinary release: the program turned on
/// mouse reporting, the window lost the pointer, the surface is going away. The
/// next press is then a fresh single click.
pub fn gestureReset(self: *Gesture) void {
    self.inner.reset(self.terminal);
}

/// ghostty's selections hold page pins, which cannot cross the C ABI; screen
/// coordinates are the same information. Null if the pins have already been
/// scrolled out of the page list, which the caller handles the same as no
/// selection.
fn selectionOf(t: *Terminal, sel: vt.Selection) ?Selection {
    return Selection.fromPins(
        &t.screens.active.pages,
        sel.start(),
        sel.end(),
        sel.rectangle,
    );
}
