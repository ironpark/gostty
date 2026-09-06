//! Text search over a terminal.
//!
//! ghostty has two searchers. `search.Screen` searches one screen, and
//! `search.Terminal` wraps one per screen, keeps a viewport searcher beside
//! them, and reconciles all of it as the terminal changes. This binds the
//! terminal one: results survive a switch to the alternate screen and back,
//! the scan can be driven a slice at a time so a large scrollback does not
//! block a UI, and the matches a renderer actually needs -- the ones on
//! screen -- can be asked for without walking the whole result list.
//!
//! The split that matters is which calls read the terminal. `tick` does not,
//! so it is safe to run beside terminal IO; `feed`, `select` and `all` do, so
//! the caller must not be writing to the terminal at the same time.
const std = @import("std");
const vt = @import("ghostty_vt");
const common = @import("common.zig");
const screen_mod = @import("screen.zig");

const Allocator = std.mem.Allocator;
const Terminal = common.Terminal;
const Screen = common.Screen;
const Selection = screen_mod.Selection;

/// A text search over a terminal: every screen, plus the scrollback.
///
/// A child of the terminal it reads, so the close order is search, then
/// terminal. ghostty's searcher takes the terminal back on every call that
/// reads it, including its own `deinit`, so the terminal is held here rather
/// than passed in each time: a search that outlived its terminal could not be
/// closed safely anyway.
pub const Search = struct {
    inner: vt.search.Terminal,
    terminal: *Terminal,
};

/// Which way `Search.select` moves through the matches.
///
/// Named `SearchDirection` rather than mirroring ghostty's `Select`: the C
/// typedef for a `SearchSelect` would be `zg_search_select`, colliding with the
/// function symbol for `Search.select`.
pub const SearchDirection = vt.search.Screen.Select;

/// Whether `Search.select` scrolls the viewport to the match it selected.
pub const SearchScroll = vt.search.Terminal.SelectScroll;

/// How much of the terminal the search has covered.
///
/// Named `SearchState` rather than mirroring ghostty's `Status`: the C typedef
/// for a `SearchStatus` would collide with the symbol for `Search.status`.
pub const SearchState = vt.search.Terminal.Status;

/// What one `Search.tick` achieved. Named for the same reason as
/// `SearchState`: `SearchTick` would collide with `Search.tick`.
pub const SearchProgress = vt.search.Terminal.Tick;

/// Start searching `t` for `needle`, which is copied.
///
/// The search is fed once here, so it has seen the terminal before it is
/// returned and `tick` can make progress immediately. Returned by value, like
/// `Terminal.init`: zigo boxes the result and frees the box in `close`.
pub fn newSearch(t: *Terminal, gpa: Allocator, needle_unowned: []const u8) Allocator.Error!Search {
    var inner: vt.search.Terminal = try .init(gpa, needle_unowned);
    inner.feed(t, true);
    return .{ .inner = inner, .terminal = t };
}

/// Release the search, and the tracked pins it holds inside the terminal's
/// page storage. Must happen before the terminal is closed.
pub fn searchClose(self: *Search) void {
    self.inner.deinit(self.terminal);
}

/// The needle being searched for, borrowed.
pub fn searchNeedle(self: *Search) []const u8 {
    return self.inner.needle();
}

/// How much of the terminal the search has covered so far.
pub fn searchStatus(self: *Search) SearchState {
    return self.inner.status();
}

/// Push the search forward as far as it can go without reading the terminal,
/// and report what that achieved. Safe to run beside terminal IO, so a UI can
/// spend a slice of each frame here rather than blocking on `searchAll`.
///
/// `blocked` means the searcher has consumed everything the last `searchFeed`
/// gave it and needs another one.
pub fn searchTick(self: *Search) SearchProgress {
    return self.inner.tick();
}

/// Read the terminal into the search: reconcile its screens, hand the
/// searchers more scrollback, and notice whether the viewport moved.
///
/// This is also the only way the search learns that the terminal changed, so
/// keep feeding it while it is in use, even after it reports complete.
///
/// Pass `active_dirty` false only if you know the active area has not changed
/// since the last feed; true is always correct and the rescan is cheap.
pub fn searchFeed(self: *Search, active_dirty: bool) void {
    self.inner.feed(self.terminal, active_dirty);
}

/// Run the search to completion, feeding and ticking until nothing is left.
///
/// The simple call, for a program that would rather block than drive the
/// search itself. A large scrollback can take a while; `searchTick` and
/// `searchFeed` are the way to spread that over frames instead.
pub fn searchAll(self: *Search) void {
    // Feed first: the terminal may have changed since the last one, including
    // which screen is active, and `tick` alone would report the stale state
    // complete without ever noticing.
    self.inner.feed(self.terminal, true);
    while (true) switch (self.inner.tick()) {
        .progress => {},
        .complete => return,
        .blocked => self.inner.feed(self.terminal, true),
    };
}

/// Move to the next or previous match on the active screen, wrapping at the
/// ends, and select it on the screen. False if there is nothing to select.
///
/// Feeds first, so it always works against current terminal state.
pub fn searchSelect(self: *Search, to: SearchDirection, scroll: SearchScroll) !bool {
    return self.inner.select(self.terminal, to, scroll);
}

/// How many matches have been found on the active screen so far. Grows as the
/// search progresses, so it is only final once the status is `complete`.
pub fn searchMatchCount(self: *Search) usize {
    const inner = self.inner.activeScreenSearch() orelse return 0;
    return inner.matchesLen();
}

/// Copy the matches found on the active screen so far into `dst`, most recent
/// screen content first, and return how many were written. Matches are in
/// screen coordinates; size `dst` from `searchMatchCount`.
pub fn searchMatches(self: *Search, dst: []Selection) usize {
    const screen_search = self.inner.activeScreenSearch() orelse return 0;
    const pages = &activeScreen(self).pages;
    var written: usize = 0;
    const total = screen_search.matchesLen();
    var i: usize = 0;
    while (i < total and written < dst.len) : (i += 1) {
        const match = screen_search.matchAt(i) orelse continue;
        const bounds = match.untracked();
        dst[written] = Selection.fromPins(pages, bounds.start, bounds.end, false) orelse continue;
        written += 1;
    }
    return written;
}

/// The matches on the pages the viewport covers, in screen coordinates, for a
/// renderer highlighting what is on screen. Returns how many were written.
///
/// This is the cheap way to draw highlights: the viewport is searched on its
/// own and the results are cached until it moves, so a frame does not pay for
/// the whole scrollback, and the answer is there before the scrollback search
/// has finished. It can include a few matches just off screen when they share
/// a page with the viewport, which a renderer clips anyway.
///
/// The count is not known in advance, so size `dst` generously and treat a
/// full `dst` as "there may be more".
pub fn searchViewportMatches(self: *Search, dst: []Selection) Allocator.Error!usize {
    const pages = &activeScreen(self).pages;
    var written: usize = 0;
    for (try self.inner.viewportMatches()) |match| {
        if (written >= dst.len) break;
        const bounds = match.untracked();
        dst[written] = Selection.fromPins(pages, bounds.start, bounds.end, false) orelse continue;
        written += 1;
    }
    return written;
}

/// The match `searchSelect` last moved to, or null before the first move.
pub fn searchSelectedMatch(self: *Search) ?Selection {
    const screen_search = self.inner.activeScreenSearch() orelse return null;
    const match = screen_search.selectedMatch() orelse return null;
    const bounds = match.untracked();
    return Selection.fromPins(&activeScreen(self).pages, bounds.start, bounds.end, false);
}

/// The index of the selected match in the list `searchMatches` writes, or null
/// before the first `searchSelect`. What a UI shows as "3 of 12".
pub fn searchSelectedIndex(self: *Search) ?usize {
    const screen_search = self.inner.activeScreenSearch() orelse return null;
    const sel = screen_search.selected orelse return null;
    return sel.idx;
}

/// The screen the search last saw as active, which is the one every
/// single-screen read resolves against. Falls back to the terminal's own
/// active screen if that one is gone.
fn activeScreen(self: *Search) *Screen {
    const t = self.terminal;
    return t.screens.get(self.inner.active_key) orelse t.screens.active;
}
