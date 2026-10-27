//go:build !gostty_noembed && !((darwin || linux || windows) && (amd64 || arm64))

package gostty

// No native library is built for this platform.
var embeddedLibrary []byte
