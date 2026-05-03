package chroma

import (
	"github.com/alecthomas/chroma/v2"
	"github.com/unxed/vtui"
	"testing"
)

func TestGetSyntaxAttr_Fallbacks(t *testing.T) {
	vtui.SetDefaultPalette()
	base := uint64(0)

	// 1. Exact match (Keyword)
	attr := GetSyntaxAttr(chroma.Keyword, base)
	if vtui.GetRGBFore(attr) != SyntaxMap[chroma.Keyword] {
		t.Errorf("Expected keyword color, got %06X", vtui.GetRGBFore(attr))
	}

	// 2. Inheritance (KeywordConstant -> Keyword)
	attrSub := GetSyntaxAttr(chroma.KeywordConstant, base)
	if vtui.GetRGBFore(attrSub) != SyntaxMap[chroma.Keyword] {
		t.Errorf("Expected inherited keyword color for KeywordConstant, got %06X", vtui.GetRGBFore(attrSub))
	}

	// 3. No match -> return base
	attrNone := GetSyntaxAttr(chroma.Text, base)
	if attrNone != base {
		t.Error("Expected base attribute for unknown token type")
	}
}

func TestChromaHighlighter_HighlightLogic(t *testing.T) {
	provider := &ChromaProvider{}
	// Create highlighter for Go
	h := provider.Create("test.go", "package main")

	line := "func main() {"
	attrs, nextState := h.Highlight(line, nil, 0)

	if len(attrs) != len([]rune(line)) {
		t.Errorf("Attributes length mismatch: expected %d, got %d", len(line), len(attrs))
	}

	// First 4 chars ("func") should be highlighted as Keyword
	kwColor := SyntaxMap[chroma.Keyword]
	for i := 0; i < 4; i++ {
		if vtui.GetRGBFore(attrs[i]) != kwColor {
			t.Errorf("Char %d ('%c') should be keyword color, got %06X", i, line[i], vtui.GetRGBFore(attrs[i]))
		}
	}

	if nextState != nil {
		t.Log("Chroma highlighter returned a state")
	}
}
