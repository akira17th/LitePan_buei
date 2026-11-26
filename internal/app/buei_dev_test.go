package app

import "testing"

func TestBueiDevMode(t *testing.T) {
	cases := map[string]bool{
		"":         false,
		"0":        false,
		"false":    false,
		"1":        true,
		"true":     true,
		" ON ":     true,
		"yes":      true,
		"nonsense": false,
	}
	for raw, want := range cases {
		t.Setenv("LITEPAN_DEV", raw)
		if got := bueiDevMode(); got != want {
			t.Errorf("bueiDevMode(LITEPAN_DEV=%q) = %v, want %v", raw, got, want)
		}
	}
}
