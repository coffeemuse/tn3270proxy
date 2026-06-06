package screens

import (
	"testing"

	"github.com/racingmars/go3270"
)

func TestCommandLine(t *testing.T) {
	g := Geometry{Rows: 24, Cols: 80}
	prompt, input, _ := commandLine(g, "Option ===>", FieldSelection)

	if prompt.Row != 1 || prompt.Col != 2 || prompt.Color != go3270.Turquoise {
		t.Errorf("prompt = %+v, want row 1 col 2 turquoise", prompt)
	}
	if prompt.Content != "Option ===>" {
		t.Errorf("prompt content = %q", prompt.Content)
	}
	// "Option ===>" is 11 runes: content cols 3..13, next attr at col 14.
	if input.Col != 14 || input.Color != go3270.Green || !input.Write {
		t.Errorf("input = %+v, want col 14 green writable", input)
	}
	if input.Name != FieldSelection {
		t.Errorf("input name = %q, want %q", input.Name, FieldSelection)
	}
	if c := cursorAt(input); c.Row != 1 || c.Col != 15 {
		t.Errorf("cursor = %+v, want (1,15)", c)
	}
}
