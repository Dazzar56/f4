package main

import "testing"

func TestEscapeIsReadableAndLossless(t *testing.T) {
	got := Escape([]byte("a\x1b[Kb\r\n\x07\x01"))
	want := "a<ESC>[Kb<CR><LF>\n<BEL><1>"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestClipMarksWhatItCut(t *testing.T) {
	if s := Clip("short", 100); s != "short" {
		t.Fatalf("short text was touched: %q", s)
	}
	s := Clip("0123456789", 4)
	if len(s) <= 4 || s[:4] != "0123" {
		t.Fatalf("clip lost the head: %q", s)
	}
	if !contains(s, "10 bytes total") {
		t.Fatalf("clip did not say how much it cut: %q", s)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
