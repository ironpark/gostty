package input_test

import (
	"encoding/json"
	"testing"

	"github.com/ironpark/gostty/input"
)

// Keybinding configuration names keys and modifiers in text; the generated
// parsers turn that text back into the enums the encoder takes.
func TestKeyTextRoundTrip(t *testing.T) {
	key, err := input.ParseKey("bracket_left")
	if err != nil || key != input.KeyBracketLeft {
		t.Errorf("ParseKey = %v, %v; want %v", key, err, input.KeyBracketLeft)
	}
	var binding struct {
		Key    input.Key         `json:"key"`
		Button input.MouseButton `json:"button"`
	}
	if err := json.Unmarshal([]byte(`{"key":"enter","button":"left"}`), &binding); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if binding.Key != input.KeyEnter || binding.Button != input.MouseButtonLeft {
		t.Errorf("decoded %+v, want enter/left", binding)
	}
	out, err := json.Marshal(binding)
	if err != nil || string(out) != `{"key":"enter","button":"left"}` {
		t.Errorf("json.Marshal = %s, %v", out, err)
	}
}
