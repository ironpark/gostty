package main

// A pseudoconsole renders rather than passes through, and a shell printing the
// same line over and over redraws to almost nothing -- five seconds of it
// arrives as a couple of chunks. So this asks for what a pseudoconsole gives:
// output waiting to be read when `close` is called, if not enough of it to have
// the reader blocked.
const unreadChunksWanted = 1
