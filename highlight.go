package main

import (
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/unxed/vtui"
)

// SyntaxMap связывает типы токенов Chroma с цветами f4
var SyntaxMap = map[chroma.TokenType]uint32{
	chroma.Comment:        0x555753, // Gray
	chroma.Keyword:        0x729FCF, // Light Blue
	chroma.String:         0x8AE234, // Green
	chroma.Number:         0xAD7FA8, // Purple
	chroma.Operator:       0xFFFFFF, // White
	chroma.NameFunction:   0xFCE94F, // Yellow
	chroma.NameVariable:   0xEEEEEC, // Near White
	chroma.GenericHeading: 0x729FCF,
}

// GetSyntaxAttr возвращает атрибуты vtui для конкретного типа токена
func GetSyntaxAttr(t chroma.TokenType, baseAttr uint64) uint64 {
	// Ищем наиболее точное совпадение (Keyword -> KeywordConstant и т.д.)
	for t != chroma.None {
		if color, ok := SyntaxMap[t]; ok {
			return vtui.SetRGBFore(baseAttr, color)
		}
		p := t.Parent()
		if p == t {
			break
		}
		t = p
	}
	return baseAttr
}

// ChromaHighlighter implements vfs.Highlighter using the chroma library.
// This is now an internal plugin capability.
type ChromaHighlighter struct{}

func (c *ChromaHighlighter) Name() string { return "Chroma" }

func (c *ChromaHighlighter) CanHighlight(filename string, content string) bool {
	return lexers.Get(filename) != nil || lexers.Analyse(content) != nil
}

func (c *ChromaHighlighter) Highlight(line string, prevState any, baseAttr uint64) ([]uint64, any) {
	// Chroma is technically stateless per-line in this simple implementation,
	// but we fulfill the interface.
	lexer := lexers.Analyse(line)
	if lexer == nil {
		lexer = lexers.Fallback
	}
	lexer = chroma.Coalesce(lexer)

	iterator, err := lexer.Tokenise(nil, line)
	if err != nil {
		return nil, nil
	}

	var attrs []uint64
	for _, token := range iterator.Tokens() {
		attr := GetSyntaxAttr(token.Type, baseAttr)
		runes := []rune(token.Value)
		for range runes {
			attrs = append(attrs, attr)
		}
	}
	return attrs, nil
}