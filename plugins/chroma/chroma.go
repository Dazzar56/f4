package chroma

import (
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/unxed/f4/vfs"
	"github.com/unxed/vtui"
)

// Plugin is the internal plugin wrapper for the Chroma syntax highlighter.
type Plugin struct{}

func (p *Plugin) Init(api vfs.HostAPI) error {
	api.RegisterHighlighter(&ChromaProvider{})
	return nil
}

func (p *Plugin) Close() error { return nil }
func (p *Plugin) GetName() string { return "Internal Syntax Highlighter (Chroma)" }

// SyntaxMap links Chroma token types to f4 colors
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

// GetSyntaxAttr returns vtui attributes for a specific token type
func GetSyntaxAttr(t chroma.TokenType, baseAttr uint64) uint64 {
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

// ChromaProvider implements vtui.HighlighterProvider using the chroma library.
type ChromaProvider struct{}

func (p *ChromaProvider) Name() string { return "Chroma" }

func (p *ChromaProvider) Match(filename string, content string) bool {
	return lexers.Match(filename) != nil || lexers.Analyse(content) != nil
}

func (p *ChromaProvider) Create(filename string, content string) vtui.Highlighter {
	lexer := lexers.Match(filename)
	if lexer == nil {
		lexer = lexers.Analyse(content)
	}
	if lexer == nil {
		lexer = lexers.Fallback
	}
	lexer = chroma.Coalesce(lexer)
	return &ChromaHighlighter{lexer: lexer}
}

// ChromaHighlighter implements vtui.Highlighter.
type ChromaHighlighter struct {
	lexer chroma.Lexer
}

func (c *ChromaHighlighter) Highlight(line string, prevState any, baseAttr uint64) ([]uint64, any) {
	iterator, err := c.lexer.Tokenise(nil, line)
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