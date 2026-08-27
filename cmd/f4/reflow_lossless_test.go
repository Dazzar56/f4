package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/unxed/vtui"
)

// The re-wrap must be lossless in isolation. The field chase of #425 spent
// four runs on hypotheses about reflowLocked before this was tested; it was
// innocent throughout (6.14, 6.17). The matrix here is every resize shape a
// mouse drag produces, so that a regression is caught by name.

// reflowSnapshot is the buffer's text as logical lines, history first, with
// trailing blanks kept: on a wrapped row they are content (ledger A5), and a
// snapshot that trimmed them could not see that loss.
func reflowSnapshot(tv *TerminalView) []string {
	var out []string
	var cur string
	for i, row := range tv.GridHistory {
		cur += reflowRowText(row)
		if i >= len(tv.GridHistoryWrap) || !tv.GridHistoryWrap[i] {
			out = append(out, strings.TrimRight(cur, " "))
			cur = ""
		}
	}
	for y := 0; y < tv.Height; y++ {
		cur += reflowRowText(tv.Lines[y])
		if y >= len(tv.WrapFlags) || !tv.WrapFlags[y] {
			if strings.TrimSpace(cur) != "" {
				out = append(out, strings.TrimRight(cur, " "))
			}
			cur = ""
		}
	}
	if strings.TrimSpace(cur) != "" {
		out = append(out, strings.TrimRight(cur, " "))
	}
	return out
}

func reflowRowText(row []vtui.CharInfo) string {
	var b []rune
	for _, c := range row {
		if c.Char == 0 {
			b = append(b, ' ')
			continue
		}
		b = append(b, rune(c.Char))
	}
	return string(b)
}

func filledTerminalView(t *testing.T, w, h, lines int) *TerminalView {
	t.Helper()
	tv := NewTerminalView(w, h)
	p := NewAnsiParser(tv, nil)
	for i := 0; i < lines; i++ {
		p.Process([]byte(fmt.Sprintf("line %03d %s\r\n", i, strings.Repeat("x", 90))))
	}
	return tv
}

func TestReflowAtTheSameWidthIsTheIdentity(t *testing.T) {
	tv := filledTerminalView(t, 120, 10, 300)
	defer tv.Close()
	before := reflowSnapshot(tv)
	tv.mu.Lock()
	tv.reflowLocked(tv.Width, tv.Height)
	tv.mu.Unlock()
	after := reflowSnapshot(tv)
	if len(before) != len(after) {
		t.Fatalf("a no-op re-wrap changed the line count: %d -> %d", len(before), len(after))
	}
	for i := range before {
		if before[i] != after[i] {
			t.Fatalf("a no-op re-wrap changed line %d:\n before %q\n after  %q", i, before[i], after[i])
		}
	}
}

func TestReflowLosesNothingAcrossEveryResizeShape(t *testing.T) {
	type step struct{ w, h int }
	cases := []struct {
		name  string
		hint  bool
		steps []step
	}{
		{"width only", true, []step{{119, 10}, {97, 10}, {41, 10}, {97, 10}, {120, 10}}},
		{"height only", true, []step{{120, 9}, {120, 6}, {120, 3}, {120, 8}, {120, 10}}},
		{"both, shrinking", true, []step{{119, 9}, {97, 8}, {61, 6}, {41, 4}, {37, 3}}},
		{"both, a real drag", true, []step{
			{119, 29}, {110, 27}, {97, 26}, {80, 24}, {61, 22}, {41, 20}, {37, 19},
			{61, 22}, {97, 26}, {120, 30}}},
		{"same size repeatedly", true, []step{{120, 10}, {120, 10}, {120, 10}}},
		{"one column at a time", true, []step{{119, 10}, {118, 10}, {117, 10}, {116, 10}, {115, 10}}},
		{"width only, hint off", false, []step{{119, 10}, {97, 10}, {41, 10}, {120, 10}}},
		{"both, hint off", false, []step{{119, 9}, {97, 8}, {41, 4}, {120, 10}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tv := filledTerminalView(t, 120, 10, 300)
			defer tv.Close()
			tv.HintWrap = tc.hint
			tv.ReflowOnResize = true
			before := reflowSnapshot(tv)
			for i, s := range tc.steps {
				tv.Resize(s.w, s.h)
				after := reflowSnapshot(tv)
				if len(after) < len(before) {
					t.Fatalf("step %d (%dx%d): %d logical lines -> %d", i, s.w, s.h, len(before), len(after))
				}
				for j := range before {
					if j < len(after) && before[j] != after[j] {
						t.Fatalf("step %d (%dx%d) changed line %d:\n before %q\n after  %q",
							i, s.w, s.h, j, before[j], after[j])
					}
				}
			}
		})
	}
}

// Trailing spaces inside a wrapped line are content (A5): the column
// alignment of a listing is made of them.
func TestReflowKeepsSpacesInsideWrappedLines(t *testing.T) {
	tv := NewTerminalView(20, 6)
	defer tv.Close()
	tv.ReflowOnResize = true
	p := NewAnsiParser(tv, nil)
	line := "aaa" + strings.Repeat(" ", 10) + "bbb" + strings.Repeat(" ", 10) + "ccc"
	p.Process([]byte(line + "\r\n"))
	for _, w := range []int{18, 14, 30, 20} {
		tv.Resize(w, 6)
	}
	after := reflowSnapshot(tv)
	if len(after) == 0 || after[0] != line {
		t.Fatalf("spacing inside a wrapped line changed:\n want %q\n got  %v", line, after)
	}
}

// Output arriving mid-drag must not disturb the lines already complete.
func TestReflowDuringOutputKeepsCompletedLines(t *testing.T) {
	tv := filledTerminalView(t, 120, 10, 50)
	defer tv.Close()
	tv.ReflowOnResize = true
	p := NewAnsiParser(tv, nil)
	before := reflowSnapshot(tv)
	for i, w := range []int{110, 90, 70, 90, 120} {
		p.Process([]byte(fmt.Sprintf("mid %02d %s", i, strings.Repeat("y", 60))))
		tv.Resize(w, 10)
		p.Process([]byte("\r\n"))
	}
	after := reflowSnapshot(tv)
	for j := range before {
		if j < len(after) && before[j] != after[j] {
			t.Fatalf("output during a resize changed earlier line %d:\n before %q\n after  %q", j, before[j], after[j])
		}
	}
}
