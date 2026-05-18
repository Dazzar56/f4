package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// FarMenu.ini text format, as written by far2l (usermenu.cpp:103):
//
//	HotKey:  Label\r\n
//	    Command1\r\n
//	    Command2\r\n
//
// For submenu items, the line right after the header is "{\r\n", and the
// nested level is terminated by "}\r\n". Separator items use HotKey "--"
// and an empty label.
//
// Encoding: far2l writes UTF-16LE with a BOM (SIGN_WIDE_LE), but its
// reader (GetFileFormat) also accepts UTF-8 with or without BOM. We
// always write UTF-8 without BOM (round-trips cleanly through far2l)
// and read all of UTF-16LE/BE/UTF-8±BOM/plain UTF-8 on input.

const (
	farMenuColonSep      = ":  " // colon + two spaces between HotKey and Label
	farMenuCommandIndent = "    "
	farMenuLineEnding    = "\r\n"
)

// ParseFarMenu reads a FarMenu.ini text file and returns its items.
func ParseFarMenu(r io.Reader) ([]UserMenuItem, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	text, err := decodeFarMenuBytes(raw)
	if err != nil {
		return nil, err
	}
	p := &farMenuParser{lines: splitTextLines(text)}
	items := p.parseLevel()
	if items == nil {
		items = []UserMenuItem{}
	}
	return items, nil
}

// WriteFarMenu writes items to w in far2l-compatible text format.
// Output is UTF-8 without BOM with CRLF line endings.
func WriteFarMenu(w io.Writer, items []UserMenuItem) error {
	var buf bytes.Buffer
	writeFarMenuLevel(&buf, items)
	_, err := w.Write(buf.Bytes())
	return err
}

func decodeFarMenuBytes(b []byte) (string, error) {
	switch {
	case len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF:
		return string(b[3:]), nil
	case len(b) >= 2 && b[0] == 0xFF && b[1] == 0xFE:
		return decodeUTF16Bytes(b[2:], binary.LittleEndian), nil
	case len(b) >= 2 && b[0] == 0xFE && b[1] == 0xFF:
		return decodeUTF16Bytes(b[2:], binary.BigEndian), nil
	}
	if !utf8.Valid(b) {
		return "", errors.New("FarMenu.ini: input is neither valid UTF-8 nor BOM-marked UTF-16")
	}
	return string(b), nil
}

func decodeUTF16Bytes(b []byte, bo binary.ByteOrder) string {
	if len(b)%2 != 0 {
		b = b[:len(b)-1]
	}
	u16 := make([]uint16, len(b)/2)
	for i := range u16 {
		u16[i] = bo.Uint16(b[i*2 : i*2+2])
	}
	return string(utf16.Decode(u16))
}

func splitTextLines(s string) []string {
	// Normalize CRLF and lone CR to LF, then split. Trailing empty string
	// from a final newline is fine; the parser skips blank lines.
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.Split(s, "\n")
}

type farMenuParser struct {
	lines []string
	pos   int
}

// parseLevel parses items until it hits "}" or EOF. The opening "{" of
// a nested level (if any) must be consumed by the caller before calling.
func (p *farMenuParser) parseLevel() []UserMenuItem {
	var items []UserMenuItem
	for p.pos < len(p.lines) {
		line := strings.TrimRight(p.lines[p.pos], " \t")
		if line == "" {
			p.pos++
			continue
		}
		if line == "}" {
			p.pos++
			return items
		}
		if line == "{" {
			// Stray opening before any item — far2l ignores it.
			p.pos++
			continue
		}
		if isFarMenuSpaceByte(line[0]) {
			// Indented line with no item to attach to — drop it (matches
			// far2l, which would attach to the previous item's command
			// list at KeyNumber < 0 and silently no-op).
			p.pos++
			continue
		}
		colon := strings.IndexByte(line, ':')
		if colon < 0 {
			p.pos++
			continue
		}
		item := UserMenuItem{
			HotKey: line[:colon],
			Label:  strings.TrimLeft(line[colon+1:], " \t"),
		}
		p.pos++
		if p.peekIsSubmenuOpen() {
			// Skip blank lines, then consume the "{".
			for p.pos < len(p.lines) {
				cur := strings.TrimRight(p.lines[p.pos], " \t")
				p.pos++
				if cur == "{" {
					break
				}
			}
			children := p.parseLevel()
			if children == nil {
				children = []UserMenuItem{}
			}
			item.Submenu = children
		} else {
			for p.pos < len(p.lines) {
				cl := strings.TrimRight(p.lines[p.pos], " \t")
				if cl == "" {
					p.pos++
					continue
				}
				if cl == "}" || !isFarMenuSpaceByte(cl[0]) {
					break
				}
				item.Commands = append(item.Commands, strings.TrimLeft(cl, " \t"))
				p.pos++
			}
		}
		items = append(items, item)
	}
	return items
}

func (p *farMenuParser) peekIsSubmenuOpen() bool {
	for i := p.pos; i < len(p.lines); i++ {
		line := strings.TrimRight(p.lines[i], " \t")
		if line == "" {
			continue
		}
		return line == "{"
	}
	return false
}

func isFarMenuSpaceByte(c byte) bool { return c == ' ' || c == '\t' }

func writeFarMenuLevel(buf *bytes.Buffer, items []UserMenuItem) {
	for i := range items {
		it := &items[i]
		buf.WriteString(it.HotKey)
		buf.WriteString(farMenuColonSep)
		buf.WriteString(it.Label)
		buf.WriteString(farMenuLineEnding)
		if it.IsSubmenu() {
			buf.WriteString("{")
			buf.WriteString(farMenuLineEnding)
			writeFarMenuLevel(buf, it.Submenu)
			buf.WriteString("}")
			buf.WriteString(farMenuLineEnding)
		} else {
			for _, cmd := range it.Commands {
				buf.WriteString(farMenuCommandIndent)
				buf.WriteString(cmd)
				buf.WriteString(farMenuLineEnding)
			}
		}
	}
}
