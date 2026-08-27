// Formatting helpers. The line-structure analysis lives in grid.go: an earlier
// version of this file split the stream on CRLF only, which on 10.0.22000 read
// a two-row wrapped line as one 140-character row and produced four confident
// verdicts that were all meaningless. ConPTY does not always mark a line break
// with bytes -- sometimes it just writes past the right edge -- so nothing here
// tries to answer that question without a grid.
package main

import (
	"fmt"
	"strings"
)

// Escape renders bytes readably for the log, one output line per stream line.
func Escape(b []byte) string {
	var sb strings.Builder
	for _, c := range b {
		switch {
		case c == 0x1b:
			sb.WriteString("<ESC>")
		case c == '\r':
			sb.WriteString("<CR>")
		case c == '\n':
			sb.WriteString("<LF>\n")
		case c == 0x07:
			sb.WriteString("<BEL>")
		case c < 32 || c == 127:
			fmt.Fprintf(&sb, "<%d>", c)
		default:
			sb.WriteByte(c)
		}
	}
	return sb.String()
}

// Clip keeps the log small enough to paste into an issue.
func Clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + fmt.Sprintf("\n...[clipped, %d bytes total]", len(s))
}

func clipRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "..."
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func yesno(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
