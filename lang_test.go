package main

import "testing"

func TestMsg(t *testing.T) {
	// 1. Test existing key
	got := Msg("Panel.UpDir")
	want := "UP-DIR"
	if got != want {
		t.Errorf("Msg(Panel.UpDir) = %q; want %q", got, want)
	}

	// 2. Test missing key (should return {key})
	got = Msg("NonExistentKey")
	want = "{NonExistentKey}"
	if got != want {
		t.Errorf("Msg(NonExistentKey) = %q; want %q", got, want)
	}
}