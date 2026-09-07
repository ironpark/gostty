package gostty

import (
	"encoding/base64"
	"strings"
	"testing"
)

// A write must be answered from inside the callback, which runs while Feed is
// still on the stack. Everything about the request is read off the request
// the callback is handed.
func TestClipboardWrite(t *testing.T) {
	_, stream := newStreamPair(t, 20, 3)

	type capture struct {
		location ClipboardLocation
		mime     string
		data     string
	}
	var got []capture

	err := stream.OnClipboardWriteRequest(func(req *ClipboardRequest) {
		loc, err := req.Location()
		if err != nil {
			t.Errorf("Location: %v", err)
			return
		}
		n, err := req.ContentCount()
		if err != nil {
			t.Errorf("ContentCount: %v", err)
			return
		}
		for i := uint(0); i < n; i++ {
			mime, err := req.ContentMime(i)
			if err != nil {
				t.Errorf("ContentMime: %v", err)
				return
			}
			data, err := req.ContentData(i)
			if err != nil {
				t.Errorf("ContentData: %v", err)
				return
			}
			got = append(got, capture{loc, mime, string(data)})
		}
		if err := req.Allow(false); err != nil {
			t.Errorf("Allow: %v", err)
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
	err := stream.OnClipboardReadRequest(func(req *ClipboardRequest) {
		called++
		if n, err := req.MimeCount(); err != nil || n == 0 {
			t.Errorf("MimeCount() = %d, %v; want at least one", n, err)
		}
		if err := req.ReplyText("from go", false); err != nil {
			t.Errorf("ReplyText: %v", err)
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
	if err := stream.OnClipboardWriteRequest(func(*ClipboardRequest) { silent++ }); err != nil {
		t.Fatalf("OnClipboardWriteRequest: %v", err)
	}
	feed(t, stream, "\x1b]52;c;"+payload+"\x07")
	if silent != 1 {
		t.Errorf("callback ran %d times, want 1", silent)
	}

	if err := stream.OnClipboardWriteRequest(func(req *ClipboardRequest) {
		if err := req.Deny(ClipboardDenialUnsupported); err != nil {
			t.Errorf("Deny: %v", err)
		}
	}); err != nil {
		t.Fatalf("OnClipboardWriteRequest: %v", err)
	}
	feed(t, stream, "\x1b]52;c;"+payload+"\x07")
}

// A request outlives its callback only as a handle: once the callback has
// returned nothing is pending behind it, so the accessors answer their zero
// values and a late reply is a no-op rather than a message to the program.
func TestClipboardRequestAfterCallback(t *testing.T) {
	_, stream := newStreamPair(t, 20, 3)
	var kept *ClipboardRequest
	if err := stream.OnClipboardWriteRequest(func(req *ClipboardRequest) {
		kept = req
		_ = req.Allow(false)
	}); err != nil {
		t.Fatalf("OnClipboardWriteRequest: %v", err)
	}
	feed(t, stream, "\x1b]52;c;"+base64.StdEncoding.EncodeToString([]byte("x"))+"\x07")
	if kept == nil {
		t.Fatal("callback did not run")
	}
	if n, err := kept.ContentCount(); err != nil || n != 0 {
		t.Errorf("ContentCount() after the callback = %d, %v; want 0, nil", n, err)
	}
	if name, err := kept.Name(); err != nil || len(name) != 0 {
		t.Errorf("Name() after the callback = %q, %v; want empty, nil", name, err)
	}
	if err := kept.Allow(false); err != nil {
		t.Errorf("Allow() with nothing pending: %v", err)
	}
}

// A retained callback is owned by the handle it was registered on: registering
// again replaces it, and Close releases whatever is left. Without that, every
// registration would strand a cgo.Handle and the Go closure behind it for the
// life of the process.
func TestClipboardCallbackHandlesAreReleased(t *testing.T) {
	before := zigoActiveCallbackHandleCount()

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
			if err := stream.OnClipboardWriteRequest(func(*ClipboardRequest) {}); err != nil {
				t.Fatalf("OnClipboardWriteRequest: %v", err)
			}
			if got, want := zigoActiveCallbackHandleCount(), before+1; got != want {
				t.Fatalf("after %d registrations, %d handles live, want %d", i+1, got, want)
			}
		}
		if err := stream.OnClipboardReadRequest(func(*ClipboardRequest) {}); err != nil {
			t.Fatalf("OnClipboardReadRequest: %v", err)
		}
		if got, want := zigoActiveCallbackHandleCount(), before+2; got != want {
			t.Fatalf("two slots live = %d, want %d", got, want)
		}
	}()

	if got := zigoActiveCallbackHandleCount(); got != before {
		t.Errorf("after Close, %d handles live, want %d", got, before)
	}
}
