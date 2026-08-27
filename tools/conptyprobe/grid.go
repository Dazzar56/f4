// A minimal grid model. The first field run showed why it is needed: ConPTY
// does not put a CRLF at a wrap point in the live stream at all. It writes the
// characters and lets the terminal's own autowrap take them to the next row,
// then jumps with an absolute CUP. A parser that only splits on CRLF sees one
// 140-character line and reports nonsense.
//
// So the probe keeps a cursor, a width, and for every row how that row *ended*:
// by running off the right edge (a soft wrap, the join information f4 wants),
// by an explicit line feed (a hard break), or by the cursor being moved away.
// Deferred wrap is modelled properly, because the whole question turns on it:
// a line of exactly W characters followed by CRLF must not be mistaken for a
// wrap.
//
// No syscalls here, so it is testable everywhere.
package main

import (
	"fmt"
	"strings"
)

// How a row ended.
const (
	EndWrap = "wrap" // ran off the right edge; the next row continues it
	EndLF   = "lf"   // an explicit line feed
	EndCUP  = "cup"  // the cursor was moved somewhere else
	EndNone = ""     // still the last row when the chunk ran out
)

type GridRow struct {
	Text    string
	Written int
	EL      bool // an erase-to-end-of-line happened on this row
	EndedBy string
}

// Full reports whether the row filled the console width.
func (r GridRow) Full(w int) bool { return r.Written >= w }

type Grid struct {
	W, H     int
	rows     [][]rune
	written  []int
	el       []bool
	ended    []string
	x, y     int
	pending  bool // deferred wrap: the last column is written, the wrap is not
	Scrolled int
	OSCs     []string
	WinOps   []string // ESC[8;h;w t and friends
	CUPs     int
}

func NewGrid(w, h int) *Grid {
	g := &Grid{W: w, H: h}
	for i := 0; i < h; i++ {
		g.addRow()
	}
	return g
}

func (g *Grid) addRow() {
	row := make([]rune, g.W)
	for i := range row {
		row[i] = ' '
	}
	g.rows = append(g.rows, row)
	g.written = append(g.written, 0)
	g.el = append(g.el, false)
	g.ended = append(g.ended, EndNone)
}

func (g *Grid) endRow(kind string) {
	if g.y >= 0 && g.y < len(g.ended) && g.ended[g.y] == EndNone {
		g.ended[g.y] = kind
	}
}

func (g *Grid) nextRow() {
	g.y++
	if g.y >= len(g.rows) {
		g.addRow()
	}
	if g.y >= g.H {
		// The viewport scrolled; the top row leaves for the history.
		g.Scrolled++
	}
}

func (g *Grid) put(r rune) {
	if g.y < 0 {
		g.y = 0
	}
	for g.y >= len(g.rows) {
		g.addRow()
	}
	if g.x >= g.W {
		g.x = g.W - 1
	}
	g.rows[g.y][g.x] = r
	if g.x+1 > g.written[g.y] {
		g.written[g.y] = g.x + 1
	}
	if g.x == g.W-1 {
		g.pending = true
	} else {
		g.x++
	}
}

func (g *Grid) Feed(b []byte) {
	for i := 0; i < len(b); i++ {
		c := b[i]
		if c == 0x1b {
			i += g.esc(b[i:]) - 1
			continue
		}
		switch {
		case c == '\r':
			g.x = 0
			g.pending = false
		case c == '\n':
			g.endRow(EndLF)
			g.nextRow()
			g.pending = false
		case c == 0x08:
			if g.x > 0 {
				g.x--
			}
			g.pending = false
		case c < 0x20 || c == 0x7f:
			// bell and friends: no effect on the layout
		default:
			if g.pending {
				g.endRow(EndWrap)
				g.nextRow()
				g.x = 0
				g.pending = false
			}
			g.put(rune(c))
		}
	}
}

// esc consumes one escape sequence and returns its length in bytes.
func (g *Grid) esc(b []byte) int {
	if len(b) < 2 {
		return len(b)
	}
	switch b[1] {
	case '[':
		i := 2
		for i < len(b) && b[i] >= 0x20 && b[i] <= 0x3f {
			i++
		}
		if i >= len(b) {
			return len(b)
		}
		raw := string(b[2:i])
		final := b[i]
		i++
		g.csi(raw, final)
		return i
	case ']':
		for i := 2; i < len(b); i++ {
			if b[i] == 0x07 {
				g.OSCs = append(g.OSCs, string(b[2:i]))
				return i + 1
			}
			if b[i] == 0x1b && i+1 < len(b) && b[i+1] == '\\' {
				g.OSCs = append(g.OSCs, string(b[2:i]))
				return i + 2
			}
		}
		g.OSCs = append(g.OSCs, string(b[2:]))
		return len(b)
	case 'P', 'X', '^', '_':
		for i := 2; i < len(b); i++ {
			if b[i] == 0x1b && i+1 < len(b) && b[i+1] == '\\' {
				return i + 2
			}
		}
		return len(b)
	default:
		return 2
	}
}

func (g *Grid) csi(raw string, final byte) {
	private := strings.HasPrefix(raw, "?") || strings.HasPrefix(raw, ">")
	body := strings.TrimLeft(raw, "?>=<")
	var params []int
	for _, p := range strings.Split(body, ";") {
		params = append(params, atoiDefault(p, 0))
	}
	param := func(i, def int) int {
		if i < len(params) && params[i] > 0 {
			return params[i]
		}
		return def
	}
	switch final {
	case 'H', 'f':
		if private {
			return
		}
		row, col := param(0, 1)-1, param(1, 1)-1
		if row != g.y {
			g.endRow(EndCUP)
		}
		g.CUPs++
		g.y, g.x = row, col
		g.pending = false
		for g.y >= len(g.rows) {
			g.addRow()
		}
	case 'A':
		g.endRow(EndCUP)
		g.y -= param(0, 1)
		if g.y < 0 {
			g.y = 0
		}
		g.pending = false
	case 'B':
		g.endRow(EndCUP)
		g.y += param(0, 1)
		for g.y >= len(g.rows) {
			g.addRow()
		}
		g.pending = false
	case 'C':
		g.x += param(0, 1)
		g.pending = false
	case 'D':
		g.x -= param(0, 1)
		if g.x < 0 {
			g.x = 0
		}
		g.pending = false
	case 'K':
		if private {
			return
		}
		g.el[g.y] = true
		switch param(0, 0) {
		case 0:
			if g.written[g.y] > g.x {
				g.written[g.y] = g.x
			}
		case 2:
			g.written[g.y] = 0
		}
	case 'J':
		if private {
			return
		}
		if param(0, 0) == 2 {
			for i := range g.rows {
				g.written[i] = 0
				g.ended[i] = EndNone
			}
		}
	case 't':
		g.WinOps = append(g.WinOps, raw)
	}
}

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return def
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func (g *Grid) Rows() []GridRow {
	out := make([]GridRow, 0, len(g.rows))
	for i := range g.rows {
		out = append(out, GridRow{
			Text:    string(g.rows[i][:g.written[i]]),
			Written: g.written[i],
			EL:      g.el[i],
			EndedBy: g.ended[i],
		})
	}
	return out
}

// Report formats the rows for the log. This is the table that replaces the
// broken one: the "end" column is the whole point.
func (g *Grid) Report(maxRows int) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "    grid %dx%d, cursor at %d,%d, %d CUPs, %d rows scrolled off, winops=%v\n",
		g.W, g.H, g.x, g.y, g.CUPs, g.Scrolled, g.WinOps)
	fmt.Fprintf(&sb, "    %-4s %-5s %-5s %-6s %s\n", "row", "len", "end", "ESC[K", "text")
	rows := g.Rows()
	for i, r := range rows {
		if i >= maxRows {
			fmt.Fprintf(&sb, "    ... %d more rows\n", len(rows)-i)
			break
		}
		if r.Written == 0 && r.EndedBy == EndNone && !r.EL {
			continue
		}
		mark := ""
		if r.Full(g.W) {
			mark = " <-full"
		}
		fmt.Fprintf(&sb, "    %-4d %-5d %-5s %-6v %q%s\n", i, r.Written, r.EndedBy, r.EL,
			clipRunes(r.Text, 44), mark)
	}
	return sb.String()
}

// LineVerdict is what the wrap question actually asks, once the grid is
// modelled instead of guessed at.
type LineVerdict struct {
	Rows        int    // how many console rows the marked line occupies
	FirstEnd    string // how the first of those rows ended
	SoftWrap    bool   // it ran off the edge: the stream carries the join
	HardCRLF    bool   // a full row followed by an explicit line feed
	ELOnBreak   bool   // the row that ended the line carried ESC[K
	ELOnWrapped bool   // a wrapped row carried ESC[K (would kill the P6 hint)
}

// AnalyzeLine finds the logical line that holds `marker` and reads how its
// rows ended. It takes the *last* occurrence and then extends through wrapped
// neighbours, because the same characters appear twice on screen: once in
// ConPTY's echo of the typed command, once in the command's own output, and
// no marker can tell those apart -- only their position can.
func AnalyzeLine(g *Grid, marker string) LineVerdict {
	var v LineVerdict
	rows := g.Rows()
	last := -1
	for i, r := range rows {
		if strings.Contains(r.Text, marker) {
			last = i
		}
	}
	if last < 0 {
		return v
	}
	start := last
	for start > 0 && rows[start-1].EndedBy == EndWrap {
		start--
	}
	end := last
	for end+1 < len(rows) && rows[end].EndedBy == EndWrap {
		end++
	}
	v.FirstEnd = rows[start].EndedBy
	for i := start; i <= end; i++ {
		r := rows[i]
		v.Rows++
		switch r.EndedBy {
		case EndWrap:
			v.SoftWrap = true
			if r.EL {
				v.ELOnWrapped = true
			}
		case EndLF:
			if r.Full(g.W) {
				v.HardCRLF = true
			}
			v.ELOnBreak = r.EL
		default:
			v.ELOnBreak = r.EL
		}
	}
	return v
}

func endsList(g *Grid, marker string) string {
	var out []string
	for _, r := range g.Rows() {
		if strings.Contains(r.Text, marker) {
			e := r.EndedBy
			if e == EndNone {
				e = "-"
			}
			out = append(out, e)
		}
	}
	if len(out) == 0 {
		return "(marker not found)"
	}
	return strings.Join(out, ",")
}
