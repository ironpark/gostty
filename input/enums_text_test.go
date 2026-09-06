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
		Key input.Key    `json:"key"`
		Mod input.KeyMod `json:"mod"`
	}
	if err := json.Unmarshal([]byte(`{"key":"enter","mod":"ctrl"}`), &binding); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if binding.Key != input.KeyEnter || binding.Mod != input.KeyModCtrl {
		t.Errorf("decoded %+v, want enter/ctrl", binding)
	}
	out, err := json.Marshal(binding)
	if err != nil || string(out) != `{"key":"enter","mod":"ctrl"}` {
		t.Errorf("json.Marshal = %s, %v", out, err)
	}
}
