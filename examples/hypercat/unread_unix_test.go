//go:build !windows

package main

// A full queue, which is the interesting state: the reader goroutine is blocked
// on a send, and `close` has to get it out of there. A pty passes writes
// through, so a shell in a loop fills this in no time.
const unreadChunksWanted = 64
