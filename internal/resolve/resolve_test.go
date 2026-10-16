package resolve

import "testing"

func TestCleanName(t *testing.T) {
	cases := map[string]string{
		"Brave.New.Wonders":            "Brave New Wonders",
		"VORON_Ravens_Story_v1.361":    "VORON Ravens Story",
		"Warhammer.40000.Boltgun.2":    "Warhammer 40000 Boltgun 2",
		"Please, Don’t Touch Anything": "Please, Don't Touch Anything",
		"Anno 1800":                    "Anno 1800",
		"Mortal Kombat 1":              "Mortal Kombat 1",
	}
	for in, want := range cases {
		if got := CleanName(in); got != want {
			t.Errorf("CleanName(%q) = %q, want %q", in, got, want)
		}
	}
}
