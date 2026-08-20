package saga

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValidateStickyNoteAnchor(t *testing.T) {
	valid := Anchor{Type: "note", Coordinate: "normalized", Note: &NoteSelector{Text: "Rename this helper", X: .25, Y: .5, Color: "#f2bd4b"}}
	if err := ValidateAnchor(valid); err != nil {
		t.Fatalf("sticky note anchor should be valid: %v", err)
	}
	corner := Anchor{Type: "note", Coordinate: "normalized", Note: &NoteSelector{Text: "Corner"}}
	if err := ValidateAnchor(corner); err != nil {
		t.Fatalf("a note placed at the origin should be valid: %v", err)
	}
	// x and y are required by schema/v2, so a zero placement must survive encoding.
	encoded, err := json.Marshal(corner)
	if err != nil || !strings.Contains(string(encoded), `"x":0`) || !strings.Contains(string(encoded), `"y":0`) {
		t.Fatalf("zero placement was dropped from %s (err=%v)", encoded, err)
	}

	rejected := map[string]Anchor{
		"note is missing":             {Type: "note", Coordinate: "normalized"},
		"placement is not normalized": {Type: "note", Note: &NoteSelector{Text: "Ship it", X: .5, Y: .5}},
		"text is blank":               {Type: "note", Coordinate: "normalized", Note: &NoteSelector{Text: "   ", X: .5, Y: .5}},
		"text exceeds the limit":      {Type: "note", Coordinate: "normalized", Note: &NoteSelector{Text: strings.Repeat("a", MaxNoteRunes+1), X: .5, Y: .5}},
		"placement is off canvas":     {Type: "note", Coordinate: "normalized", Note: &NoteSelector{Text: "Ship it", X: 1.2, Y: .5}},
		"color is unsafe":             {Type: "note", Coordinate: "normalized", Note: &NoteSelector{Text: "Ship it", X: .5, Y: .5, Color: "expression(alert(1))"}},
		"note carries shapes":         {Type: "note", Coordinate: "normalized", Note: &NoteSelector{Text: "Ship it", X: .5, Y: .5}, Shapes: []Shape{{Type: "rect"}}},
		"target anchor carries note":  {Type: "target", Note: &NoteSelector{Text: "Ship it", X: .5, Y: .5}},
		"text anchor carries note":    {Type: "text", Text: &TextSelector{Exact: "quote"}, Note: &NoteSelector{Text: "Ship it", X: .5, Y: .5}},
		"region anchor carries note":  {Type: "region", Coordinate: "normalized", Shapes: []Shape{{Type: "rect"}}, Note: &NoteSelector{Text: "Ship it", X: .5, Y: .5}},
	}
	for name, anchor := range rejected {
		if err := ValidateAnchor(anchor); err == nil {
			t.Errorf("%s: anchor should have been rejected", name)
		}
	}
}
