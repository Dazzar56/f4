package main

import "testing"

func TestDeferredWrapDoesNotBreakAnExactWidthLine(t *testing.T) {
	// Ten characters in a ten-column console, then CRLF. That is a hard break,
	// not a wrap, and the whole reflow question turns on telling them apart.
	g := NewGrid(10, 5)
	g.Feed([]byte("0123456789\r\nnext\r\n"))
	rows := g.Rows()
	if rows[0].EndedBy != EndLF {
		t.Fatalf("exact-width line ended by %q, want %q", rows[0].EndedBy, EndLF)
	}
	if rows[1].Text != "next" {
		t.Fatalf("row 1 = %q", rows[1].Text)
	}
}

func TestExactWidthHardBreakHasNoELAndWouldFoolTheHint(t *testing.T) {
	g := NewGrid(10, 3)
	g.Feed([]byte("AAAAAAAAAA\r\nnext\r\n"))
	v := AnalyzeLine(g, "AAAAAAAA")
	if !v.HardCRLF || v.FirstEnd != EndLF {
		t.Fatalf("exact-width verdict = %#v, want hard CRLF", v)
	}
	if v.ELOnBreak {
		t.Fatalf("exact-width hard break unexpectedly carried ESC[K: %#v", v)
	}
	if hintWouldJoin := v.HardCRLF && !v.ELOnBreak; !hintWouldJoin {
		t.Fatal("test setup no longer demonstrates the full-row/no-EL hint ambiguity")
	}
}

func TestAutowrapIsSeenAsAWrap(t *testing.T) {
	g := NewGrid(10, 5)
	g.Feed([]byte("0123456789abc\r\n"))
	rows := g.Rows()
	if rows[0].EndedBy != EndWrap {
		t.Fatalf("row 0 ended by %q, want %q", rows[0].EndedBy, EndWrap)
	}
	if rows[1].Text != "abc" || rows[1].EndedBy != EndLF {
		t.Fatalf("row 1 = %#v", rows[1])
	}
}

// The bytes below are from the f4probe run on 10.0.22000.2538 (issue #425).
// ConPTY emitted no CRLF at all here: 65 characters at a width of 40, then an
// absolute CUP. The old CRLF-only parser read this as one 140-character row.
func TestLiveEchoOn22000IsReadAsTwoWrappedRows(t *testing.T) {
	live := []byte("echo ABCDEFGHIJ0123456789abcdefghij0123456789ABCDEFGHIJ0123456789" +
		"\x1b[9;1H\x1b[?25lABCDEFGHIJ0123456789abcdefghij0123456789ABCDEFGHIJ0123456789" +
		"\x1b[12;1HD:\\f4probe-pkg>\x1b[?25h")
	g := NewGrid(40, 12)
	g.Feed(live)
	v := AnalyzeLine(g, "echo ABCDEFGHIJ")
	if !v.SoftWrap {
		t.Errorf("the echoed command wrapped off the edge, but SoftWrap=false: %#v", v)
	}
	if v.HardCRLF {
		t.Errorf("there is no CRLF in this sample, but HardCRLF=true")
	}
	if g.CUPs != 2 {
		t.Errorf("CUPs = %d, want 2", g.CUPs)
	}
}

// The 19045 sample from TERMINAL_CONPTY_FINDINGS §1: a real CRLF at the wrap
// point, the tail padded to the width. The two builds must come out different.
func TestRecorded19045SampleIsReadAsAHardBreak(t *testing.T) {
	g := NewGrid(40, 12)
	g.Feed([]byte("C:\\2work-150>echo ABCDEFGHIJ0123456789ab\r\n" +
		"cdefghij0123456789ABCDEFGHIJ0123456789  \r\n"))
	rows := g.Rows()
	if rows[0].EndedBy != EndLF {
		t.Fatalf("row 0 ended by %q, want %q", rows[0].EndedBy, EndLF)
	}
	if !rows[0].Full(40) {
		t.Fatalf("row 0 wrote %d columns, want the full 40", rows[0].Written)
	}
	v := AnalyzeLine(g, "echo ABCDEFGHIJ")
	if !v.HardCRLF || v.SoftWrap {
		t.Fatalf("19045 sample verdict = %#v, want a hard break", v)
	}
}

func TestELShortensTheRowAndIsRecorded(t *testing.T) {
	g := NewGrid(20, 3)
	g.Feed([]byte("hello world\rbye\x1b[K\r\n"))
	rows := g.Rows()
	if rows[0].Text != "bye" {
		t.Fatalf("row 0 = %q, want %q", rows[0].Text, "bye")
	}
	if !rows[0].EL {
		t.Error("ESC[K was not recorded")
	}
}

func TestOSCAndWinOpsAreCollected(t *testing.T) {
	g := NewGrid(20, 3)
	g.Feed([]byte("\x1b[8;12;100t\x1b]0;title - cmd\x07x"))
	if len(g.WinOps) != 1 || g.WinOps[0] != "8;12;100" {
		t.Fatalf("winops = %v", g.WinOps)
	}
	if len(g.OSCs) != 1 || g.OSCs[0] != "0;title - cmd" {
		t.Fatalf("oscs = %v", g.OSCs)
	}
}

func TestScrollOffIsCounted(t *testing.T) {
	g := NewGrid(10, 3)
	g.Feed([]byte("a\r\nb\r\nc\r\nd\r\ne\r\n"))
	if g.Scrolled < 2 {
		t.Fatalf("scrolled = %d, want at least 2", g.Scrolled)
	}
}
