package main

import "testing"

func TestFrameBufferCollectsAndFlushes(t *testing.T) {
	var b frameBuffer
	var sink frameBuffer
	if err := b.flush(&sink); err != nil || len(sink) != 0 {
		t.Fatal("an empty buffer wrote something")
	}
	_, _ = b.Write([]byte("ab"))
	_, _ = b.Write([]byte("c"))
	if err := b.flush(&sink); err != nil || string(sink) != "abc" {
		t.Fatalf("flushed %q", sink)
	}
	b.reset()
	if len(b) != 0 || cap(b) == 0 {
		t.Fatal("reset should keep the allocation and drop the bytes")
	}
}
