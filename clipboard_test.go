package gostty

import (
	"encoding/base64"
	"strings"
	"testing"
)

// A write must be answered from inside the callback, which runs while Feed is
// still on the stack. Everything about the request is read off the stream.
func TestClipboardWrite(t *testing.T) {
	_, stream := newStreamPair(t, 20, 3)

	type capture struct {
		location ClipboardLocation
		mime     string
		data     string
	}
	var got []capture

	err := stream.OnClipboardWriteRequest(func() {
		loc, err := stream.ClipboardLocation()
		if err != nil {
			t.Errorf("ClipboardLocation: %v", err)
			return
		}
		n, err := stream.ClipboardContentCount()
		if err != nil {
			t.Errorf("ClipboardContentCount: %v", err)
			return
		}
		for i := uint(0); i < n; i++ {
			mime, err := stream.ClipboardContentMime(i)
			if err != nil {
				t.Errorf("ClipboardContentMime: %v", err)
				return
			}
			data, err := stream.ClipboardContentData(i)
			if err != nil {
				t.Errorf("ClipboardContentData: %v", err)
				return
			}
			got = append(got, capture{loc, string(mime), string(data)})
		}
		if err := stream.AllowClipboard(false); err != nil {
			t.Errorf("AllowClipboard: %v", err)
		}
	})
	if err != nil {
		t.Fatalf("OnClipboardWriteRequest: %v", err)
	}

	payload := base64.StdEncoding.EncodeToString([]byte("copied text"))
	feed(t, stream, "\x1b]52;c;"+payload+"\x07")

	if len(got) != 1 {
		t.Fatalf("captured %+v, want one representation", got)
	}
	if got[0].data != "copied text" {
		t.Errorf("data = %q, want %q", got[0].data, "copied text")
	}
	if !strings.HasPrefix(got[0].mime, "text/plain") {
		t.Errorf("mime = %q, want a text mime", got[0].mime)
	}
	if got[0].location != ClipboardLocationStandard {
		t.Errorf("location = %v, want standard", got[0].location)
	}
}

// A read is served with text, which the terminal encodes back to the program.
func TestClipboardRead(t *testing.T) {
	term, stream := newStreamPair(t, 40, 3)

	called := 0
	err := stream.OnClipboardReadRequest(func() {
		called++
		if n, err := stream.ClipboardMimeCount(); err != nil || n == 0 {
			t.Errorf("ClipboardMimeCount() = %d, %v; want at least one", n, err)
		}
		if err := stream.ReplyClipboardText([]byte("from go"), false); err != nil {
			t.Errorf("ReplyClipboardText: %v", err)
		}
	})
	if err != nil {
		t.Fatalf("OnClipboardReadRequest: %v", err)
	}

	// The terminal answers a read by writing to the pty, which a readonly
	// stream discards, so what this pins is that the callback ran and replied.
	feed(t, stream, "\x1b]52;c;?\x07")
	if called != 1 {
		t.Fatalf("read callback ran %d times, want 1", called)
	}
	if failed, err := stream.Failed(); err != nil || failed {
		t.Errorf("Failed() = %v, %v; want false, nil", failed, err)
	}
	if _, err := term.Cols(); err != nil {
		t.Errorf("terminal unusable after the read: %v", err)
	}
}

// Denying is the default: without a callback nothing is served, and an
// installed callback that returns without answering denies too.
func TestClipboardDenied(t *testing.T) {
	_, stream := newStreamPair(t, 20, 3)

	payload := base64.StdEncoding.EncodeToString([]byte("secret"))
	feed(t, stream, "\x1b]52;c;"+payload+"\x07")
	if failed, err := stream.Failed(); err != nil || failed {
		t.Errorf("Failed() with no handler = %v, %v; want false, nil", failed, err)
	}

	silent := 0
	if err := stream.OnClipboardWriteRequest(func() { silent++ }); err != nil {
		t.Fatalf("OnClipboardWriteRequest: %v", err)
	}
	feed(t, stream, "\x1b]52;c;"+payload+"\x07")
	if silent != 1 {
		t.Errorf("callback ran %d times, want 1", silent)
	}

	if err := stream.OnClipboardWriteRequest(func() {
		if err := stream.DenyClipboard(ClipboardDenialUnsupported); err != nil {
			t.Errorf("DenyClipboard: %v", err)
		}
	}); err != nil {
		t.Fatalf("OnClipboardWriteRequest: %v", err)
	}
	feed(t, stream, "\x1b]52;c;"+payload+"\x07")
}

// Nothing is pending outside a callback, so the accessors answer safely.
func TestClipboardAccessorsOutsideCallback(t *testing.T) {
	_, stream := newStreamPair(t, 20, 3)
	if n, err := stream.ClipboardContentCount(); err != nil || n != 0 {
		t.Errorf("ClipboardContentCount() = %d, %v; want 0, nil", n, err)
	}
	if name, err := stream.ClipboardName(); err != nil || len(name) != 0 {
		t.Errorf("ClipboardName() = %q, %v; want empty, nil", name, err)
	}
	if err := stream.AllowClipboard(false); err != nil {
		t.Errorf("AllowClipboard() with nothing pending: %v", err)
	}
}

// A retained callback is owned by the handle it was registered on: registering
// again replaces it, and Close releases whatever is left. Without that, every
// registration would strand a cgo.Handle and the Go closure behind it for the
// life of the process.
func TestClipboardCallbackHandlesAreReleased(t *testing.T) {
	before := activeCallbackHandleCount()

	func() {
		term, err := NewTerminal(20, 3)
		if err != nil {
			t.Fatalf("NewTerminal: %v", err)
		}
		defer term.Close()
		stream, err := term.NewStream(0)
		if err != nil {
			t.Fatalf("NewStream: %v", err)
		}
		defer stream.Close()

		for i := 0; i < 5; i++ {
			if err := stream.OnClipboardWriteRequest(func() {}); err != nil {
				t.Fatalf("OnClipboardWriteRequest: %v", err)
			}
			if got, want := activeCallbackHandleCount(), before+1; got != want {
				t.Fatalf("after %d registrations, %d handles live, want %d", i+1, got, want)
			}
		}
		if err := stream.OnClipboardReadRequest(func() {}); err != nil {
			t.Fatalf("OnClipboardReadRequest: %v", err)
		}
		if got, want := activeCallbackHandleCount(), before+2; got != want {
			t.Fatalf("two slots live = %d, want %d", got, want)
		}
	}()

	if got := activeCallbackHandleCount(); got != before {
		t.Errorf("after Close, %d handles live, want %d", got, before)
	}
}
