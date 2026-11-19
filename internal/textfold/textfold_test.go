package textfold

import "testing"

func TestFold(t *testing.T) {
	for in, want := range map[string]string{
		"  Éléphant ": "elephant",
		"ÇA VA":       "ca va",
		"Straße":      "strase",
		"déjà-vu":     "deja-vu",
		"Plain":       "plain",
	} {
		if got := Fold(in); got != want {
			t.Errorf("Fold(%q) = %q, want %q", in, got, want)
		}
	}
}
