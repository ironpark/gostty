package shelltest

// Windows has no hangup signal: closing the pseudoconsole is what ends the
// child, and nothing here can decline it.
func ignoreHangup() {}
