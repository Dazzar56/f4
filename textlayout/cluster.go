package textlayout

import (
	"unicode"
	"unicode/utf8"

	"github.com/unxed/vtui"
)

// VisualCluster is a terminal-facing grapheme cluster with byte boundaries in
// the original string.
type VisualCluster struct {
	Text       string
	Width      int
	Start, End int
}

// VisualClusters segments a string once and returns its visual clusters. It
// deliberately uses vtui's callback API rather than calling NextCluster for
// every suffix: the latter would rescan the remaining string for every ASCII
// character and turn long-line layout into quadratic work.
func VisualClusters(s string) []VisualCluster {
	clusters := make([]VisualCluster, 0, len(s))
	previousStart, previousWidth := 0, 0
	havePrevious := false

	emit := func(start, end, width int) {
		if start >= end {
			return
		}
		raw := s[start:end]
		if len(clusters) > 0 && endsInIndicVirama(clusters[len(clusters)-1].Text) && startsWithLetter(raw) {
			last := &clusters[len(clusters)-1]
			last.Text = s[last.Start:end]
			last.End = end
			last.Width = vtui.ClusterWidth(last.Text)
			return
		}
		clusters = append(clusters, VisualCluster{Text: raw, Width: width, Start: start, End: end})
	}

	vtui.ForEachClusterAt(s, func(_ string, width, offset, _ int) {
		if havePrevious {
			emit(previousStart, offset, previousWidth)
		}
		previousStart, previousWidth = offset, width
		havePrevious = true
	})
	if havePrevious {
		emit(previousStart, len(s), previousWidth)
	}
	return clusters
}

// NextVisualCluster returns the next cluster as it is treated by a terminal
// text editor. UAX #29 (which vtui implements) handles combining marks, emoji,
// and bidi marks, while the extra virama join handles Indic conjuncts that
// older versions of the Unicode grapheme tables split between the virama and
// the following consonant. Keeping this rule here makes wrapping, cursor
// movement, and deletion agree on the same byte boundaries.
func NextVisualCluster(s string) (cluster string, width int, size int) {
	clusters := VisualClusters(s)
	if len(clusters) == 0 {
		return "", 0, 0
	}
	first := clusters[0]
	return first.Text, first.Width, first.End
}

func startsWithLetter(s string) bool {
	r, _ := utf8.DecodeRuneInString(s)
	return unicode.IsLetter(r)
}

func endsInIndicVirama(s string) bool {
	r, _ := utf8.DecodeLastRuneInString(s)
	switch r {
	case
		'\u094D',                     // Devanagari
		'\u09CD',                     // Bengali
		'\u0A4D',                     // Gurmukhi
		'\u0ACD',                     // Gujarati
		'\u0B4D',                     // Oriya
		'\u0BCD',                     // Tamil
		'\u0C4D',                     // Telugu
		'\u0CCD',                     // Kannada
		'\u0D3B', '\u0D3C', '\u0D4D', // Malayalam
		'\u0DCA', // Sinhala
		'\u1039', // Myanmar
		'\u1714', // Tagalog
		'\u17D2', // Khmer
		'\u1A60', // Tai Tham
		'\u1BAA', // Sundanese
		'\uA806', // Syloti Nagri
		'\uA8C4', // Saurashtra
		'\uA953', // Rejang
		'\uA9C0', // Javanese
		'\uAAF6', // Meetei Mayek
		'\uABED': // Meetei Mayek
		return true
	default:
		return false
	}
}
