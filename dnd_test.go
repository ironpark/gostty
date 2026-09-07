package gostty

import (
	"encoding/base64"
	"strings"
	"testing"
)

// The whole conversation, from the program registering to it concluding a
// drop. The embedder drives the native half; the program's half arrives as
// OSC 72 in the stream.
func TestDragAndDrop(t *testing.T) {
	_, stream := newStreamPair(t, 20, 3)

	type seen struct {
		event       DragEvent
		mimes       string
		accepted    DragOperation
		hasAccepted bool
	}
	var events []seen
	if err := stream.OnDrag(func(n DragNotice, mimes string) {
		events = append(events, seen{n.Event, mimes, n.Accepted, n.Answered})
	}); err != nil {
		t.Fatalf("OnDrag: %v", err)
	}

	// Nothing is registered yet, so a native drag is the embedder's own
	// problem and every call here is a no-op.
	if active, err := stream.DragActive(); err != nil || active {
		t.Fatalf("DragActive() before registration = %v, %v; want false", active, err)
	}
	if err := stream.DragMove(DragMove{Operations: DragOperations{Copy: true}}, "text/plain"); err != nil {
		t.Fatalf("DragMove with no program registered: %v", err)
	}
	if got := replyString(t, stream); got != "" {
		t.Errorf("DragMove with no program registered wrote %q, want nothing", got)
	}

	// The program registers and names the types it wants.
	feed(t, stream, "\x1b]72;t=a;image/png text/plain\x1b\\")
	if len(events) != 1 || events[0].event != DragEventRegistration {
		t.Fatalf("events after registration = %+v, want one registration", events)
	}
	if events[0].mimes != "image/png text/plain" {
		t.Errorf("registration carried mimes %q, want the registered list", events[0].mimes)
	}
	if active, err := stream.DragActive(); err != nil || !active {
		t.Fatalf("DragActive() after registration = %v, %v; want true", active, err)
	}
	mimes, err := stream.DragRegisteredMimes()
	if err != nil {
		t.Fatalf("DragRegisteredMimes: %v", err)
	}
	if got, want := strings.Fields(mimes), []string{"image/png", "text/plain"}; len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("DragRegisteredMimes() = %q, want %q", got, want)
	}

	// A native drag moves over the terminal. The program has not answered, so
	// there is no acceptance yet -- which is not the same as a refusal.
	_ = replyString(t, stream)
	if err := stream.DragMove(DragMove{
		CellX: 3, CellY: 1, PixelX: 30, PixelY: 20,
		Operations: DragOperations{Copy: true, Move: true},
	}, "text/uri-list text/plain"); err != nil {
		t.Fatalf("DragMove: %v", err)
	}
	if got := replyString(t, stream); !strings.Contains(got, "t=m") {
		t.Errorf("DragMove wrote %q, want a t=m move event", got)
	}
	if _, ok, err := stream.DragClientAccepted(); err != nil || ok {
		t.Errorf("DragClientAccepted() before the program answers = ok %v, %v; want false", ok, err)
	}

	// The program answers: o=2 is move.
	events = events[:0]
	feed(t, stream, "\x1b]72;t=m:o=2;text/plain\x1b\\")
	if len(events) != 1 || events[0].event != DragEventAcceptance {
		t.Fatalf("events after the answer = %+v, want one acceptance", events)
	}
	if !events[0].hasAccepted || events[0].accepted != DragOperationMove {
		t.Errorf("acceptance carried %v (ok %v), want move", events[0].accepted, events[0].hasAccepted)
	}

	// The user drops. The representations are staged, then handed over.
	if err := stream.DragAddItem("text/uri-list", []byte("file:///tmp/a\r\n")); err != nil {
		t.Fatalf("DragAddItem: %v", err)
	}
	if err := stream.DragAddItem("text/plain", []byte("/tmp/a")); err != nil {
		t.Fatalf("DragAddItem: %v", err)
	}
	_ = replyString(t, stream)
	if err := stream.DragDrop(DragMove{CellX: 3, CellY: 1, Operations: DragOperations{Move: true}}); err != nil {
		t.Fatalf("DragDrop: %v", err)
	}
	if got := replyString(t, stream); !strings.Contains(got, "t=M") {
		t.Errorf("DragDrop wrote %q, want a t=M drop event", got)
	}

	// The program reads a representation and then concludes.
	// The payload comes back base64 encoded, as the protocol carries it.
	feed(t, stream, "\x1b]72;t=r:x=2\x1b\\")
	if got, want := replyString(t, stream), base64.StdEncoding.EncodeToString([]byte("/tmp/a")); !strings.Contains(got, want) {
		t.Errorf("data request answered %q, want it to carry %q (the second staged item)", got, want)
	}
	events = events[:0]
	feed(t, stream, "\x1b]72;t=r:o=2\x1b\\")
	if len(events) != 1 || events[0].event != DragEventConcludedMove {
		t.Fatalf("events after conclusion = %+v, want one concluded_move", events)
	}

	// Unregistering reports one last time, with the state already gone.
	events = events[:0]
	feed(t, stream, "\x1b]72;t=A\x1b\\")
	if len(events) != 1 || events[0].event != DragEventRegistration {
		t.Fatalf("events after unregistration = %+v, want one registration", events)
	}
	if events[0].mimes != "" {
		t.Errorf("unregistration carried mimes %q, want none", events[0].mimes)
	}
	if active, err := stream.DragActive(); err != nil || active {
		t.Errorf("DragActive() after unregistration = %v, %v; want false", active, err)
	}
}

// A drag the window system cancels after the embedder staged its data must not
// leave that data behind for the next one.
func TestDragClearItems(t *testing.T) {
	_, stream := newStreamPair(t, 20, 3)
	feed(t, stream, "\x1b]72;t=a;text/plain\x1b\\")

	if err := stream.DragAddItem("text/plain", []byte("first")); err != nil {
		t.Fatalf("DragAddItem: %v", err)
	}
	if err := stream.DragClearItems(); err != nil {
		t.Fatalf("DragClearItems: %v", err)
	}
	if err := stream.DragAddItem("text/plain", []byte("second")); err != nil {
		t.Fatalf("DragAddItem: %v", err)
	}
	_ = replyString(t, stream)
	if err := stream.DragDrop(DragMove{}); err != nil {
		t.Fatalf("DragDrop: %v", err)
	}
	_ = replyString(t, stream)
	feed(t, stream, "\x1b]72;t=r:x=1\x1b\\")
	got := replyString(t, stream)
	if cleared := base64.StdEncoding.EncodeToString([]byte("first")); strings.Contains(got, cleared) {
		t.Errorf("data request answered %q; the cleared item came back", got)
	}
	if want := base64.StdEncoding.EncodeToString([]byte("second")); !strings.Contains(got, want) {
		t.Errorf("data request answered %q, want it to carry %q", got, want)
	}
}

// Leaving without dropping tells the program so, and only while a program is
// registered.
func TestDragLeave(t *testing.T) {
	_, stream := newStreamPair(t, 20, 3)
	if err := stream.DragLeave(); err != nil {
		t.Fatalf("DragLeave with no program registered: %v", err)
	}
	if got := replyString(t, stream); got != "" {
		t.Errorf("DragLeave with no program registered wrote %q, want nothing", got)
	}

	feed(t, stream, "\x1b]72;t=a;text/plain\x1b\\")
	if err := stream.DragMove(DragMove{Operations: DragOperations{Copy: true}}, "text/plain"); err != nil {
		t.Fatalf("DragMove: %v", err)
	}
	_ = replyString(t, stream)
	if err := stream.DragLeave(); err != nil {
		t.Fatalf("DragLeave: %v", err)
	}
	if got := replyString(t, stream); !strings.Contains(got, "t=m") {
		t.Errorf("DragLeave wrote %q, want a t=m leave event", got)
	}
}
