package cloud

import "testing"

func TestEscapeFolderName(t *testing.T) {
	cases := map[string]string{
		"Orc Incremental":                    "Orc Incremental",
		"Clive Barker's Hellraiser: Revival": "Clive Barker's Hellraiser_ Revival",
		"Star Wars: Zero Company":            "Star Wars_ Zero Company",
		".hack//G.U.":                        "_hack__G.U_",
		"What?":                              "What_",
	}
	for in, want := range cases {
		if got := EscapeFolderName(in); got != want {
			t.Errorf("EscapeFolderName(%q) = %q, want %q", in, got, want)
		}
	}
}
