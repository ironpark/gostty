package desktop

import "testing"

func TestLocalClipboardOwnsItsBytes(t *testing.T) {
	var c Clipboard
	data := []byte("OSC text")
	c.Hold(data)
	data[0] = 'x'
	if got := string(c.Paste()); got != "OSC text" {
		t.Fatalf("held bytes changed: %q", got)
	}
	if err := c.Copy("user copy"); err != nil {
		t.Fatal(err)
	}
	if got := string(c.Paste()); got != "user copy" {
		t.Fatalf("paste = %q", got)
	}
	c.Hold(nil)
	if len(c.Paste()) != 0 {
		t.Fatal("empty clipboard retained content")
	}
}
