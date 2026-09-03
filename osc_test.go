package gostty

import "testing"

// drain collects every event a feed produced, with the payload of each.
type event struct {
	kind          StreamEvent
	title, body   string
	progressState ProgressState
	progress      int // -1 when absent
}

func drain(t *testing.T, s *Stream) []event {
	t.Helper()
	var out []event
	for {
		kind, ok, err := s.NextEvent()
		if err != nil {
			t.Fatalf("NextEvent: %v", err)
		}
		if !ok {
			return out
		}
		ev := event{kind: kind, progress: -1}
		title, err := s.EventTitle()
		if err != nil {
			t.Fatalf("EventTitle: %v", err)
		}
		ev.title = title
		body, err := s.EventBody()
		if err != nil {
			t.Fatalf("EventBody: %v", err)
		}
		ev.body = body
		if ev.progressState, err = s.EventProgressState(); err != nil {
			t.Fatalf("EventProgressState: %v", err)
		}
		if p, ok, err := s.EventProgress(); err != nil {
			t.Fatalf("EventProgress: %v", err)
		} else if ok {
			ev.progress = int(p)
		}
		out = append(out, ev)
	}
}

func TestNoEventsWithoutOSC(t *testing.T) {
	_, stream := newStreamPair(t, 20, 3)
	feed(t, stream, "plain text\r\n")
	if got := drain(t, stream); len(got) != 0 {
		t.Errorf("got %d events for plain text, want none", len(got))
	}
}

func TestBellEvent(t *testing.T) {
	_, stream := newStreamPair(t, 20, 3)
	feed(t, stream, "a\x07b")
	got := drain(t, stream)
	if len(got) != 1 || got[0].kind != StreamEventBell {
		t.Fatalf("events = %+v, want one bell", got)
	}
}

// OSC 9 carries only a body; OSC 777 carries a title and a body.
func TestDesktopNotificationEvent(t *testing.T) {
	_, stream := newStreamPair(t, 20, 3)
	feed(t, stream, "\x1b]9;build finished\x07")
	feed(t, stream, "\x1b]777;notify;Build;all green\x07")

	got := drain(t, stream)
	if len(got) != 2 {
		t.Fatalf("events = %+v, want two notifications", got)
	}
	for i, ev := range got {
		if ev.kind != StreamEventDesktopNotification {
			t.Fatalf("event %d = %v, want a desktop notification", i, ev.kind)
		}
	}
	if got[0].body != "build finished" {
		t.Errorf("OSC 9 body = %q, want %q", got[0].body, "build finished")
	}
	if got[1].title != "Build" || got[1].body != "all green" {
		t.Errorf("OSC 777 = title %q, body %q; want %q, %q", got[1].title, got[1].body, "Build", "all green")
	}
}

// OSC 9;4 reports progress. State 1 carries a percentage, state 3 does not.
func TestProgressReportEvent(t *testing.T) {
	_, stream := newStreamPair(t, 20, 3)
	feed(t, stream, "\x1b]9;4;1;42\x07")
	feed(t, stream, "\x1b]9;4;3\x07")
	feed(t, stream, "\x1b]9;4;0\x07")

	got := drain(t, stream)
	if len(got) != 3 {
		t.Fatalf("events = %+v, want three progress reports", got)
	}
	for i, ev := range got {
		if ev.kind != StreamEventProgressReport {
			t.Fatalf("event %d = %v, want a progress report", i, ev.kind)
		}
	}
	if got[0].progressState != ProgressStateSet || got[0].progress != 42 {
		t.Errorf("first = state %v, progress %d; want set, 42", got[0].progressState, got[0].progress)
	}
	if got[1].progressState != ProgressStateIndeterminate || got[1].progress != -1 {
		t.Errorf("second = state %v, progress %d; want indeterminate, absent", got[1].progressState, got[1].progress)
	}
	if got[2].progressState != ProgressStateRemove {
		t.Errorf("third = state %v, want remove", got[2].progressState)
	}
}

// Title and pwd changes are announced as events; the values stay on the
// terminal, which is where ghostty keeps them.
func TestTitleAndPwdEvents(t *testing.T) {
	term, stream := newStreamPair(t, 40, 3)
	feed(t, stream, "\x1b]0;my title\x07\x1b]7;file:///tmp/work\x1b\\")

	got := drain(t, stream)
	if len(got) != 2 {
		t.Fatalf("events = %+v, want a title and a pwd change", got)
	}
	if got[0].kind != StreamEventTitleChanged || got[1].kind != StreamEventPwdChanged {
		t.Fatalf("events = %v, %v; want title_changed then pwd_changed", got[0].kind, got[1].kind)
	}

	title, ok, err := term.GetTitle()
	if err != nil || !ok || title != "my title" {
		t.Errorf("GetTitle() = %q, %v, %v", title, ok, err)
	}
	pwd, ok, err := term.GetPwd()
	if err != nil || !ok || pwd != "file:///tmp/work" {
		t.Errorf("GetPwd() = %q, %v, %v", pwd, ok, err)
	}
}

// Draining is destructive and the queue survives across feeds.
func TestEventQueueDrains(t *testing.T) {
	_, stream := newStreamPair(t, 20, 3)
	feed(t, stream, "\x07")
	feed(t, stream, "\x07")

	if got := drain(t, stream); len(got) != 2 {
		t.Fatalf("got %d events, want 2", len(got))
	}
	if got := drain(t, stream); len(got) != 0 {
		t.Errorf("got %d events on a second drain, want none", len(got))
	}
}
