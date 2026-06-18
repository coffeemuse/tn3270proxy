package screens

import (
	"testing"

	"github.com/racingmars/go3270"
)

func TestCommandLine(t *testing.T) {
	g := Geometry{Rows: 24, Cols: 80}
	prompt, input, stop := commandLine(g, "Option ===>", FieldSelection)

	if prompt.Row != 1 || prompt.Col != 0 || prompt.Color != go3270.Turquoise {
		t.Errorf("prompt = %+v, want row 1 col 0 turquoise", prompt)
	}
	if prompt.Content != "Option ===>" {
		t.Errorf("prompt content = %q", prompt.Content)
	}
	// "Option ===>" is 11 runes: attr at col 0, content cols 1..11, next attr at col 12.
	if input.Col != 12 || input.Color != go3270.Green || !input.Write {
		t.Errorf("input = %+v, want col 12 green writable", input)
	}
	if input.Name != FieldSelection {
		t.Errorf("input name = %q, want %q", input.Name, FieldSelection)
	}
	if c := cursorAt(input); c.Row != 1 || c.Col != 13 {
		t.Errorf("cursor = %+v, want (1,13)", c)
	}
	// The input runs to the end of the line: stop field at col 79.
	if stop.Col != 79 {
		t.Errorf("stop field col = %d, want 79", stop.Col)
	}
}
