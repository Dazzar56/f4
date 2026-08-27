package main

import (
	"bytes"
	"strings"
)

type markerObservation struct {
	whole bool
	split bool
}

func inspectMarker(raw []byte, marker string) markerObservation {
	if bytes.Contains(raw, []byte(marker)) {
		return markerObservation{whole: true}
	}
	beginAt := strings.Index(marker, "_BEGIN")
	endAt := strings.Index(marker, "_END")
	if beginAt < 0 || endAt < 0 || endAt <= beginAt {
		return markerObservation{}
	}
	prefix := marker[:beginAt+len("_BEGIN")]
	suffix := marker[endAt:]
	if bytes.Contains(raw, []byte(prefix)) && bytes.Contains(raw, []byte(suffix)) {
		return markerObservation{split: true}
	}
	return markerObservation{}
}

// wideGrid models only the VT operations needed to tell whether a marker
// occupies one row or crosses a deferred terminal wrap.
type wideGrid struct {
	width   int
	rows    [][]byte
	x, y    int
	pending bool
	savedX  int
	savedY  int
}

func newWideGrid(width int) *wideGrid { return &wideGrid{width: width} }

func (g *wideGrid) blankRow() []byte { return bytes.Repeat([]byte{' '}, g.width) }

func (g *wideGrid) ensureRow(y int) {
	for len(g.rows) <= y {
		g.rows = append(g.rows, g.blankRow())
	}
}

func (g *wideGrid) clearAll() {
	for i := range g.rows {
		g.rows[i] = g.blankRow()
	}
}

func (g *wideGrid) clearToEnd() {
	if g.y < 0 || g.y >= len(g.rows) || g.x >= g.width {
		return
	}
	for i := g.x; i < g.width; i++ {
		g.rows[g.y][i] = ' '
	}
}

func (g *wideGrid) nextRow() {
	g.y++
	g.ensureRow(g.y)
}

func (g *wideGrid) put(c byte) {
	if g.width <= 0 {
		return
	}
	if g.pending {
		g.nextRow()
		g.x = 0
		g.pending = false
	}
	g.ensureRow(g.y)
	if g.x < 0 {
		g.x = 0
	}
	if g.x >= g.width {
		g.x = g.width - 1
	}
	g.rows[g.y][g.x] = c
	if g.x == g.width-1 {
		g.pending = true
	} else {
		g.x++
	}
}

func csiParams(raw string) []int {
	raw = strings.TrimLeft(raw, "?>")
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ";")
	params := make([]int, len(parts))
	for i, part := range parts {
		if part == "" {
			continue
		}
		value := 0
		for _, c := range part {
			if c < '0' || c > '9' {
				value = 0
				break
			}
			value = value*10 + int(c-'0')
		}
		params[i] = value
	}
	return params
}

func csiDefault(params []int, index, fallback int) int {
	if index >= len(params) || params[index] == 0 {
		return fallback
	}
	return params[index]
}

func (g *wideGrid) csi(raw string, final byte) {
	p := csiParams(raw)
	move := func(x, y int) {
		g.x, g.y = x, y
		if g.x < 0 {
			g.x = 0
		}
		if g.y < 0 {
			g.y = 0
		}
		g.ensureRow(g.y)
		g.pending = false
	}
	switch final {
	case 'A':
		g.y -= csiDefault(p, 0, 1)
		if g.y < 0 {
			g.y = 0
		}
		g.pending = false
	case 'B', 'e':
		g.y += csiDefault(p, 0, 1)
		g.ensureRow(g.y)
		g.pending = false
	case 'C', 'a':
		g.x += csiDefault(p, 0, 1)
		g.pending = false
	case 'D':
		g.x -= csiDefault(p, 0, 1)
		if g.x < 0 {
			g.x = 0
		}
		g.pending = false
	case 'G', '`':
		move(csiDefault(p, 0, 1)-1, g.y)
	case 'd':
		move(g.x, csiDefault(p, 0, 1)-1)
	case 'H', 'f':
		move(csiDefault(p, 1, 1)-1, csiDefault(p, 0, 1)-1)
	case 'J':
		if len(p) == 0 || p[0] == 0 || p[0] == 2 || p[0] == 3 {
			g.clearAll()
		}
	case 'K':
		g.clearToEnd()
	case 's':
		g.savedX, g.savedY = g.x, g.y
	case 'u':
		move(g.savedX, g.savedY)
	}
}

func (g *wideGrid) escape(raw []byte) int {
	if len(raw) < 2 {
		return len(raw)
	}
	switch raw[1] {
	case '[':
		i := 2
		for i < len(raw) && raw[i] >= 0x20 && raw[i] <= 0x3f {
			i++
		}
		if i >= len(raw) {
			return len(raw)
		}
		g.csi(string(raw[2:i]), raw[i])
		return i + 1
	case ']':
		for i := 2; i < len(raw); i++ {
			if raw[i] == 0x07 {
				return i + 1
			}
			if raw[i] == 0x1b && i+1 < len(raw) && raw[i+1] == '\\' {
				return i + 2
			}
		}
		return len(raw)
	case '7':
		g.savedX, g.savedY = g.x, g.y
		return 2
	case '8':
		g.x, g.y = g.savedX, g.savedY
		g.ensureRow(g.y)
		g.pending = false
		return 2
	default:
		return 2
	}
}

func (g *wideGrid) feed(raw []byte) {
	for i := 0; i < len(raw); i++ {
		switch raw[i] {
		case 0x1b:
			i += g.escape(raw[i:]) - 1
		case '\r':
			g.x = 0
			g.pending = false
		case '\n':
			g.nextRow()
			g.pending = false
		case '\b':
			if g.x > 0 {
				g.x--
			}
			g.pending = false
		case '\t':
			next := ((g.x / 8) + 1) * 8
			if next >= g.width {
				g.pending = true
			} else {
				g.x = next
			}
		default:
			if raw[i] >= 0x20 && raw[i] != 0x7f {
				g.put(raw[i])
			}
		}
	}
}

func (g *wideGrid) rowsContaining(text string) int {
	count := 0
	for _, row := range g.rows {
		if bytes.Contains(row, []byte(text)) {
			count++
		}
	}
	return count
}

func lineRows(raw []byte, width int, text string) int {
	g := newWideGrid(width)
	g.feed(raw)
	return g.rowsContaining(text)
}
