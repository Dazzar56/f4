package main

import (
	"strings"
	"testing"
)

func TestInspectMarkerDetectsWholeAndSplit(t *testing.T) {
	marker := "C_LINE_00_BEGINXXXXXXXXXXXXXXXX_END"
	if got := inspectMarker([]byte("\x1b[1;1H"+marker+"\r\n"), marker); !got.whole || got.split {
		t.Fatalf("whole marker classified as %+v", got)
	}
	cut := strings.Index(marker, "XXXXXXXXXXXXXXXX")
	raw := []byte("C_LINE_00_BEGIN" + marker[cut:cut+4] + "\r\n" + marker[cut+4:])
	if got := inspectMarker(raw, marker); got.whole || !got.split {
		t.Fatalf("split marker classified as %+v", got)
	}
}

func TestLineRowsModelsDeferredWrap(t *testing.T) {
	if got := lineRows([]byte("\x1b[1;1H12345\r\n"), 5, "12345"); got != 1 {
		t.Fatalf("hard break after a full row got %d rows", got)
	}
	if got := lineRows([]byte("\x1b[1;1H123456"), 5, "123456"); got != 0 {
		t.Fatalf("six characters should occupy two rows at width five, got %d", got)
	}
	if got := lineRows([]byte("\x1b[1;1H1234\r\n56"), 5, "123456"); got != 0 {
		t.Fatalf("split marker unexpectedly appeared in one row: %d", got)
	}
}

func TestLineRowsHandlesRepaintCursorMoves(t *testing.T) {
	marker := "C_LINE_00_BEGINXXXX_END"
	raw := []byte("\x1b[?25l\x1b[1;1H" + marker + "\x1b[2;1H\x1b[K\x1b[?25h")
	if got := lineRows(raw, 80, marker); got != 1 {
		t.Fatalf("repaint marker got %d rows", got)
	}
}

func TestLineRowsIgnoresInteractiveCallEcho(t *testing.T) {
	marker := "C_LINE_00_BEGINXXXX_END"
	raw := []byte("D:\\probe>call \"C:\\Temp\\conptyc-1.bat\"\r\n" + marker + "\r\n")
	if got := lineRows(raw, 80, marker); got != 1 {
		t.Fatalf("batch output marker got %d rows with call echo", got)
	}
}
