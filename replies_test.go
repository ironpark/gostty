package gostty

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

type replyWriterFunc func([]byte) (int, error)

func (f replyWriterFunc) Write(p []byte) (int, error) { return f(p) }

func TestWriteRepliesRetainsOutputOnFailure(t *testing.T) {
	writeErr := errors.New("pty unavailable")
	for _, tc := range []struct {
		name   string
		writer io.Writer
		want   error
	}{
		{"error", replyWriterFunc(func([]byte) (int, error) { return 0, writeErr }), writeErr},
		{"short write", replyWriterFunc(func([]byte) (int, error) { return 0, nil }), io.ErrShortWrite},
		{"nil writer", nil, ErrNilStream},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, stream := newStreamPair(t, 20, 3)
			feed(t, stream, "\x1b[5n")
			if err := stream.WriteReplies(tc.writer); !errors.Is(err, tc.want) {
				t.Fatalf("WriteReplies error = %v, want %v", err, tc.want)
			}
			if has, err := stream.HasReplies(); err != nil || !has {
				t.Fatalf("HasReplies after failed write = %v, %v; want true, nil", has, err)
			}
			var buf bytes.Buffer
			if err := stream.WriteReplies(&buf); err != nil {
				t.Fatalf("retry WriteReplies: %v", err)
			}
			if got, want := buf.String(), "\x1b[0n"; got != want {
				t.Fatalf("retried reply = %q, want %q", got, want)
			}
			if has, err := stream.HasReplies(); err != nil || has {
				t.Fatalf("HasReplies after successful write = %v, %v; want false, nil", has, err)
			}
		})
	}
}

func TestWriteRepliesLargeOutput(t *testing.T) {
	_, stream := newStreamPair(t, 20, 3)
	// Exceed the binding adapter's staging buffer with ordered query replies.
	const count = 10000
	feed(t, stream, strings.Repeat("\x1b[5n\x1b[6n", count))
	var buf bytes.Buffer
	if err := stream.WriteReplies(&buf); err != nil {
		t.Fatalf("WriteReplies: %v", err)
	}
	if want := strings.Repeat("\x1b[0n\x1b[1;1R", count); buf.String() != want {
		t.Fatalf("reply content mismatch: got %d bytes, want %d", buf.Len(), len(want))
	}
	if err := stream.WriteReplies(&buf); err != nil {
		t.Fatalf("WriteReplies after drain: %v", err)
	}
	if want := len("\x1b[0n\x1b[1;1R") * count; buf.Len() != want {
		t.Fatalf("replies repeated after drain: got %d bytes, want %d", buf.Len(), want)
	}
}
