package main

import "testing"

func TestParseXTWINOPSReadsTheMeasuredAnswers(t *testing.T) {
	// Both strings are verbatim from the 10.0.22000 Windows Terminal report in
	// docs/WINCON_805_HANDOVER.md F16.
	cell := parseXTWINOPS("\x1b[6;20;10t")
	if !cell.ok || cell.kind != 6 || cell.a != 20 || cell.b != 10 {
		t.Fatalf("cell report parsed as %+v", cell)
	}
	area := parseXTWINOPS("\x1b[4;600;1200t")
	if !area.ok || area.kind != 4 || area.a != 600 || area.b != 1200 {
		t.Fatalf("area report parsed as %+v", area)
	}
}

func TestParseXTWINOPSRejectsIncompleteAnswers(t *testing.T) {
	for _, bad := range []string{"", "\x1b[6;20t", "no answer", "\x1b[6;20;xt", "\x1b[6;20;10"} {
		if got := parseXTWINOPS(bad); got.ok {
			t.Fatalf("%q was accepted as %+v", bad, got)
		}
	}
}

func TestResolveCellSizePrefersTheTerminalReport(t *testing.T) {
	// Windows Terminal on 22000: CSI 16 t says 20x10, while the Win32 font
	// call claims a zero-wide cell (F17). The terminal must win.
	got := resolveCellSize(
		parseXTWINOPS("\x1b[6;20;10t"),
		parseXTWINOPS("\x1b[4;600;1200t"),
		parseXTWINOPS("\x1b[8;30;120t"),
		0, 16, false,
	)
	if got.Width != 10 || got.Height != 20 {
		t.Fatalf("got %+v, want the 10x20 cell from CSI 16 t", got)
	}
}

func TestResolveCellSizeDerivesFromTheTextArea(t *testing.T) {
	// Same terminal, but with no CSI 16 t answer: 600x1200 px over 30x120
	// cells is the same 10x20 cell, derived rather than reported.
	got := resolveCellSize(
		xtwinopsReport{},
		parseXTWINOPS("\x1b[4;600;1200t"),
		parseXTWINOPS("\x1b[8;30;120t"),
		0, 16, false,
	)
	if got.Width != 10 || got.Height != 20 {
		t.Fatalf("got %+v, want a derived 10x20 cell", got)
	}
}

func TestResolveCellSizeUsesTheFontOnlyForAClassicConsole(t *testing.T) {
	// Classic conhost on the same machine answers no size query at all and
	// reports an honest 8x16 font (F13, F17).
	classic := resolveCellSize(xtwinopsReport{}, xtwinopsReport{}, xtwinopsReport{}, 8, 16, true)
	if classic.Width != 8 || classic.Height != 16 {
		t.Fatalf("classic console: got %+v, want 8x16", classic)
	}
	// The same silence behind a pseudo console must not fall through to the
	// Win32 font: that is the zero-width answer of F17.
	pseudo := resolveCellSize(xtwinopsReport{}, xtwinopsReport{}, xtwinopsReport{}, 0, 16, false)
	if pseudo.Usable() {
		t.Fatalf("pseudo console: got %+v, want an unusable result", pseudo)
	}
}

func TestResolveCellSizeNeverDividesByZero(t *testing.T) {
	// A text-area report with a zero cell grid must be discarded, not divided.
	got := resolveCellSize(
		xtwinopsReport{},
		parseXTWINOPS("\x1b[4;600;1200t"),
		parseXTWINOPS("\x1b[8;0;0t"),
		0, 0, false,
	)
	if got.Usable() {
		t.Fatalf("got %+v, want an unusable result rather than a division", got)
	}
}

func TestConsoleFontTrustExplainsItself(t *testing.T) {
	if s := consoleFontTrust(false, 0, 16); s == "" || !contains(s, "F17") {
		t.Fatalf("pseudo console verdict should cite the finding: %q", s)
	}
	if s := consoleFontTrust(true, 8, 16); !contains(s, "usable") {
		t.Fatalf("classic console verdict should be usable: %q", s)
	}
	if s := consoleFontTrust(true, 0, 16); contains(s, "usable:") {
		t.Fatalf("a zero-width classic cell must not be called usable: %q", s)
	}
}
