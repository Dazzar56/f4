package main

import (
	"strings"
	"testing"
	"time"
)

// Tests of the fake against what the probe measured. A fake that drifts from
// the field is worse than none: every test above it then proves the wrong
// thing, which is what 6.15 cost. Each assertion names the finding it holds.

func fakeFrame(t *testing.T, f *fakeConPTY, cols, rows int) string {
	t.Helper()
	f.SetSize(cols, rows)
	select {
	case b := <-f.out:
		return string(b)
	case <-time.After(time.Second):
		t.Fatal("no repaint frame")
		return ""
	}
}

func TestFakeConPTYRepaintsOnEveryResizeCall(t *testing.T) {
	// P7: a resize repaints the whole viewport. 6.15: a height-only change
	// too. 6.16: a call for the size it already has too, and that one has no
	// size report.
	for _, b := range conptyBuilds {
		t.Run(b.name, func(t *testing.T) {
			f := newFakeConPTYFor(b, 40, 6)
			f.print("x")
			<-f.out
			width := fakeFrame(t, f, 30, 6)
			height := fakeFrame(t, f, 30, 4)
			same := fakeFrame(t, f, 30, 4)
			for name, fr := range map[string]string{"width": width, "height-only": height, "same size": same} {
				if !strings.HasPrefix(fr, "\x1b[?25l") || !strings.HasSuffix(fr, "\x1b[?25h") {
					t.Errorf("%s: not a delimited frame: %q", name, fr)
				}
			}
			if b.sizeReport {
				if !strings.Contains(width, "\x1b[8;6;30t") || !strings.Contains(height, "\x1b[8;4;30t") {
					t.Errorf("P14: size-changing repaints must carry the size report: %q / %q", width, height)
				}
				if strings.Contains(same, "\x1b[8;") {
					t.Errorf("6.16: a same-size repaint carries no size report: %q", same)
				}
			}
			if f.snapshotRepaints() != 3 {
				t.Errorf("three resize calls must produce three frames, got %d", f.snapshotRepaints())
			}
		})
	}
}

func TestFakeConPTYRepaintShapeMatchesTheBuild(t *testing.T) {
	long := strings.Repeat("A", 25) // two rows at 20 columns
	// P12 on 22000: the line is written whole; only its last row ends in CRLF.
	f := newFakeConPTYFor(conpty22000, 40, 4)
	f.print(long)
	<-f.out
	fr := fakeFrame(t, f, 20, 4)
	if strings.Contains(fr, strings.Repeat("A", 20)+"\r\n") {
		t.Fatalf("22000 must not break a wrapped line with CRLF: %q", fr)
	}
	if !strings.Contains(fr, strings.Repeat("A", 25)+"\x1b[K\r\n") {
		t.Fatalf("22000 writes the line whole, short last row erased: %q", fr)
	}
	// P6 on 19045: rows joined by hard CRLF, full row with no ESC[K.
	g := newFakeConPTYFor(conpty19045, 40, 4)
	g.print(long)
	<-g.out
	gr := fakeFrame(t, g, 20, 4)
	if !strings.Contains(gr, strings.Repeat("A", 20)+"\r\n"+strings.Repeat("A", 5)+"\x1b[K") {
		t.Fatalf("19045 breaks the wrapped line with a hard CRLF and no ESC[K on the full row: %q", gr)
	}
}

func TestFakeConPTYExactWidthLineIsAmbiguous(t *testing.T) {
	// P13: a hard-broken line exactly the width arrives as a full row plus
	// CRLF with no ESC[K -- indistinguishable from a wrap by the hint.
	for _, b := range conptyBuilds {
		f := newFakeConPTYFor(b, 10, 4)
		f.print(strings.Repeat("A", 10), "b")
		<-f.out
		fr := fakeFrame(t, f, 10, 4)
		if !strings.Contains(fr, strings.Repeat("A", 10)+"\r\n") || strings.Contains(fr, strings.Repeat("A", 10)+"\x1b[K") {
			t.Errorf("%s: exact-width line must be a full row, CRLF, no ESC[K: %q", b.name, fr)
		}
	}
}

func TestFakeConPTYKeepsNoScrollback(t *testing.T) {
	// P16: the repaint covers the viewport only; lines that scrolled off are
	// gone from ConPTY's side for good.
	f := newFakeConPTYFor(conpty22000, 40, 3)
	f.print("one", "two", "three", "four", "five")
	<-f.out
	fr := fakeFrame(t, f, 40, 3)
	for _, gone := range []string{"one", "two"} {
		if strings.Contains(fr, gone) {
			t.Fatalf("P16: %q scrolled off and must not be repainted: %q", gone, fr)
		}
	}
	if !strings.Contains(fr, "five") {
		t.Fatalf("the viewport's last line must be repainted: %q", fr)
	}
}

func TestFakeConPTYWideResizeRejoins(t *testing.T) {
	// P15: the oracle's wide resize brings a wrapped line back as one row.
	f := newFakeConPTYFor(conpty22000, 20, 4)
	long := strings.Repeat("Z", 35)
	f.print(long)
	<-f.out
	fr := fakeFrame(t, f, 4000, 4)
	if !strings.Contains(fr, long+"\x1b[K") {
		t.Fatalf("P15: the wide repaint must carry the line whole: %q", fr)
	}
}

func TestFakeConPTYLiveStreamBreaksWithHardCRLF(t *testing.T) {
	// P6/P11: live output breaks a wrapped line with a real CRLF, full rows
	// padded to the width, ESC[K only after a short row.
	f := newFakeConPTYFor(conpty22000, 10, 4)
	f.print(strings.Repeat("Q", 15))
	live := string(<-f.out)
	if !strings.Contains(live, strings.Repeat("Q", 10)+"\r\n"+strings.Repeat("Q", 5)+"\x1b[K\r\n") {
		t.Fatalf("live stream shape (P6): %q", live)
	}
}
