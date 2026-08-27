package main

import (
	"fmt"
	"strconv"
	"strings"
)

// Cell geometry: which source may be trusted for "how many pixels is one
// character cell", and what each source actually answered.
//
// This exists because the two sources disagree in a way that is not a rounding
// difference. On 10.0.22000 under Windows Terminal, GetCurrentConsoleFontEx
// reports a cell **0 pixels wide** (docs/WINCON_805_HANDOVER.md F17) while the
// terminal itself answers CSI 16 t with 20x10 -- the virtual sixel cell the
// encoder is expected to use (F16). Any geometry computed by dividing by the
// Win32 width therefore divides by zero behind a pseudo console. The handover's
// step 3a fixes the order of sources; this file is that order, written once,
// testable off Windows, and reported by the probe so a field run says which
// source a given build actually answers.
//
// The logic is deliberately separate from the syscalls: every mistake this file
// guards against is an arithmetic one.

// xtwinopsReport is one CSI <n> t answer: ESC [ kind ; a ; b t.
type xtwinopsReport struct {
	kind int // 4 = text area in pixels, 6 = cell size, 8 = text area in cells
	a, b int // for kind 4/6: a = height, b = width; for kind 8: a = rows, b = cols
	ok   bool
}

// parseXTWINOPS reads an answer such as ESC[6;20;10t. Anything that is not a
// complete three-parameter report is reported as not ok rather than guessed at;
// a partially read answer must never become a pixel count.
func parseXTWINOPS(answer string) xtwinopsReport {
	i := strings.IndexByte(answer, '[')
	j := strings.LastIndexByte(answer, 't')
	if i < 0 || j < i {
		return xtwinopsReport{}
	}
	parts := strings.Split(answer[i+1:j], ";")
	if len(parts) != 3 {
		return xtwinopsReport{}
	}
	nums := make([]int, 3)
	for k, p := range parts {
		v, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			return xtwinopsReport{}
		}
		nums[k] = v
	}
	return xtwinopsReport{kind: nums[0], a: nums[1], b: nums[2], ok: true}
}

// cellSize is the answer, with the source that produced it kept alongside so
// the log never presents a derived number as a measured one.
type cellSize struct {
	Width  int
	Height int
	Source string
	Note   string
}

func (c cellSize) Usable() bool { return c.Width > 0 && c.Height > 0 }

func (c cellSize) String() string {
	if !c.Usable() {
		return "unusable (" + c.Source + ")"
	}
	return fmt.Sprintf("%dx%d px from %s", c.Width, c.Height, c.Source)
}

// resolveCellSize applies the source order of handover step 3a:
//
//  1. CSI 16 t, the terminal's own cell report. Behind a pseudo console this is
//     the only honest source, and under Windows Terminal it is the virtual
//     sixel cell the encoder must match (F16).
//  2. CSI 14 t (text area in pixels) divided by CSI 18 t (text area in cells).
//     Derived, but derived from two numbers the same terminal reported.
//  3. GetCurrentConsoleFontEx, and only for a real ConsoleWindowClass window:
//     under ConPTY it answers a zero width (F17).
//
// A zero or negative dimension from any source disqualifies that source rather
// than propagating into a division.
func resolveCellSize(cell16, area14, cells18 xtwinopsReport, winFontW, winFontH int, classicConsole bool) cellSize {
	if cell16.ok && cell16.kind == 6 && cell16.b > 0 && cell16.a > 0 {
		return cellSize{Width: cell16.b, Height: cell16.a, Source: "CSI 16 t (terminal cell report)"}
	}
	if area14.ok && area14.kind == 4 && cells18.ok && cells18.kind == 8 &&
		area14.b > 0 && area14.a > 0 && cells18.b > 0 && cells18.a > 0 {
		return cellSize{
			Width:  area14.b / cells18.b,
			Height: area14.a / cells18.a,
			Source: "CSI 14 t / CSI 18 t (text area divided by the cell grid)",
		}
	}
	if !classicConsole {
		return cellSize{
			Source: "none",
			Note: "the terminal answered no size query and this is not a classic console window, " +
				"so GetCurrentConsoleFontEx must not be used (it reports a zero width under ConPTY)",
		}
	}
	if winFontW > 0 && winFontH > 0 {
		return cellSize{
			Width:  winFontW,
			Height: winFontH,
			Source: "GetCurrentConsoleFontEx (classic console window)",
		}
	}
	return cellSize{
		Source: "none",
		Note:   "GetCurrentConsoleFontEx returned a non-positive cell size",
	}
}

// consoleFontTrust states, for the log, whether the Win32 font size may be used
// on this host at all, and why. The wrong answer here is what produces either a
// division by zero or an image scaled to a cell no terminal is using.
func consoleFontTrust(classicConsole bool, winFontW, winFontH int) string {
	switch {
	case !classicConsole:
		return "must not be trusted: behind a pseudo console the width is reported as 0 (handover F17)"
	case winFontW <= 0 || winFontH <= 0:
		return fmt.Sprintf("must not be trusted: a classic console reported a non-positive cell %dx%d", winFontW, winFontH)
	default:
		return "usable: a classic console window reports a real font cell"
	}
}
