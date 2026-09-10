// Package graphics adapts gostty Kitty image snapshots to Ebitengine textures.
// It owns image snapshots and GPU resources, but borrows the caller's terminal.
// Refresh runs with the application's terminal updates; Draw uses cached data.
// DecodePNG is registered once through gostty/sys before any terminal is opened.
package graphics
