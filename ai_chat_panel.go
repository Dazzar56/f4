package main

import (
	"github.com/mattn/go-runewidth"
	"github.com/unxed/f4/vtvibe"
	"unicode/utf8"

	"github.com/unxed/vtinput"
	"github.com/unxed/vtui"
	"strings"
)

type AIChatPanel struct {
	vtui.ScreenObject
	src     *FileSystemPanel
	frame   *vtui.BorderedFrame
	input   *vtui.MultiLineEdit
	focused bool
	topPos  int
	lines   []chatLine

	visibleLinks   []chatLink
	focusedLinkIdx int
}

type chatLine struct {
	cells   []vtui.CharInfo
	targets []string
}

type chatLink struct {
	row    int
	col    int
	width  int
	target string
}

func NewAIChatPanel(src *FileSystemPanel) *AIChatPanel {
	x1, y1, x2, y2 := src.GetPosition()
	cp := &AIChatPanel{
		src:            src,
		frame:          vtui.NewBorderedFrame(x1, y1, x2, y2, vtui.SingleBox, Msg("AI.ChatTitle")),
		input:          vtui.NewMultiLineEdit(0, 0, 10, 3, ""),
		focusedLinkIdx: -1,
	}
	cp.frame.ColorBoxIdx = ColPanelBox
	cp.frame.ColorTitleIdx = ColPanelTitle
	cp.frame.ColorBackgroundIdx = ColPanelText
	cp.input.ColorTextIdx = ColPanelText
	cp.SetPosition(x1, y1, x2, y2)
	return cp
}

func (cp *AIChatPanel) Kind() string             { return "ai_chat" }
func (cp *AIChatPanel) Source() *FileSystemPanel { return cp.src }
func (cp *AIChatPanel) IsFocused() bool          { return cp.focused }

func (cp *AIChatPanel) SetFocus(f bool) {
	cp.focused = f
	if f {
		cp.frame.ColorTitleIdx = ColPanelSelectedTitle
	} else {
		cp.frame.ColorTitleIdx = ColPanelTitle
	}
	if f && cp.focusedLinkIdx == -1 {
		cp.input.SetFocus(true)
	} else {
		cp.input.SetFocus(false)
	}
}

func (cp *AIChatPanel) GetSelectedName() string {
	if cp.src == nil {
		return ""
	}
	return cp.src.GetSelectedName()
}

func (cp *AIChatPanel) SetPosition(x1, y1, x2, y2 int) {
	cp.ScreenObject.SetPosition(x1, y1, x2, y2)
	cp.frame.SetPosition(x1, y1, x2, y2)

	inputH := 4
	if y2-y1 < 10 {
		inputH = 2
	}
	cp.input.SetPosition(x1+1, y2-inputH, x2-1, y2-1)
}

func (cp *AIChatPanel) ScrollToBottom() {
	cp.updateLines()
	h := cp.input.Y1 - cp.Y1 - 2
	maxTop := len(cp.lines) - h
	if maxTop > 0 {
		cp.topPos = maxTop
	}
}

func (cp *AIChatPanel) navigateToTarget(target string) {
	pf := findPanelsFrameAnyScreen()
	if pf == nil {
		return
	}
	if strings.HasPrefix(target, "ai://") {
		if strings.HasPrefix(target, "ai://out/") || strings.HasPrefix(target, "ai://ctx/") {
			actionOpenViewer(pf, cp.src.vfs, target)
		} else {
			AiSetViewModePanel(pf, pf.activeIdx, target, false)
		}
	}
}

func (cp *AIChatPanel) ProcessKey(e *vtinput.InputEvent) bool {
	if !e.KeyDown || !cp.focused {
		return false
	}

	ctrl := (e.ControlKeyState & (vtinput.LeftCtrlPressed | vtinput.RightCtrlPressed)) != 0
	alt := (e.ControlKeyState & (vtinput.LeftAltPressed | vtinput.RightAltPressed)) != 0
	shift := (e.ControlKeyState & vtinput.ShiftPressed) != 0

	// Handle Tab focus cycling
	if e.VirtualKeyCode == vtinput.VK_TAB && !ctrl && !alt {
		if shift {
			cp.focusedLinkIdx--
			if cp.focusedLinkIdx < -1 {
				cp.focusedLinkIdx = len(cp.visibleLinks) - 1
			}
		} else {
			cp.focusedLinkIdx++
			if cp.focusedLinkIdx >= len(cp.visibleLinks) {
				cp.focusedLinkIdx = -1
			}
		}
		cp.input.SetFocus(cp.focusedLinkIdx == -1)
		vtui.FrameManager.Redraw()
		return true
	}

	if cp.focusedLinkIdx != -1 {
		// We are focusing a link
		if e.VirtualKeyCode == vtinput.VK_RETURN {
			if cp.focusedLinkIdx >= 0 && cp.focusedLinkIdx < len(cp.visibleLinks) {
				cp.navigateToTarget(cp.visibleLinks[cp.focusedLinkIdx].target)
			}
			return true
		}
		if e.VirtualKeyCode == vtinput.VK_ESCAPE {
			cp.focusedLinkIdx = -1
			cp.input.SetFocus(true)
			vtui.FrameManager.Redraw()
			return true
		}
	}

	if e.VirtualKeyCode == vtinput.VK_RETURN && !ctrl && !alt && cp.focusedLinkIdx == -1 {
		if shift {
			ePlain := *e
			ePlain.ControlKeyState &^= vtinput.ShiftPressed
			return cp.input.ProcessKey(&ePlain)
		} else {
			text := cp.input.GetText()
			if strings.TrimSpace(text) != "" {
				aiSend(findPanelsFrameAnyScreen(), text)
				cp.input.SetText("")
				cp.ScrollToBottom()
			}
			return true
		}
	}

	h := cp.input.Y1 - cp.Y1 - 2
	if h < 1 {
		h = 1
	}

	switch e.VirtualKeyCode {
	case vtinput.VK_PRIOR:
		cp.topPos -= h
		if cp.topPos < 0 {
			cp.topPos = 0
		}
		vtui.FrameManager.Redraw()
		return true
	case vtinput.VK_NEXT:
		cp.topPos += h
		maxTop := len(cp.lines) - h
		if maxTop < 0 {
			maxTop = 0
		}
		if cp.topPos > maxTop {
			cp.topPos = maxTop
		}
		vtui.FrameManager.Redraw()
		return true
	case vtinput.VK_UP:
		if !ctrl && !alt && !shift {
			if cp.focusedLinkIdx == -1 {
				row, _ := cp.input.CursorPos()
				if row == 0 && cp.topPos > 0 {
					cp.topPos--
					vtui.FrameManager.Redraw()
					return true
				}
			}
		}
	case vtinput.VK_DOWN:
		if !ctrl && !alt && !shift {
			if cp.focusedLinkIdx == -1 {
				row, _ := cp.input.CursorPos()
				if row == cp.input.LineCount()-1 {
					maxTop := len(cp.lines) - h
					if maxTop < 0 {
						maxTop = 0
					}
					if cp.topPos < maxTop {
						cp.topPos++
						vtui.FrameManager.Redraw()
						return true
					}
				}
			}
		}
	case vtinput.VK_LEFT, vtinput.VK_RIGHT:
		if shift {
			if cp.focusedLinkIdx == -1 {
				cp.input.ProcessKey(e)
			}
			return true
		}
	}

	if cp.focusedLinkIdx == -1 {
		if cp.input.ProcessKey(e) {
			return true
		}
	}

	return false
}

func (cp *AIChatPanel) ProcessMouse(e *vtinput.InputEvent) bool {
	if cp.input.ProcessMouse(e) {
		cp.focusedLinkIdx = -1
		cp.input.SetFocus(true)
		return true
	}
	if e.Type == vtinput.MouseEventType {
		if e.WheelDirection != 0 {
			if e.WheelDirection > 0 {
				cp.topPos -= 3
				if cp.topPos < 0 {
					cp.topPos = 0
				}
			} else {
				cp.topPos += 3
				h := cp.input.Y1 - cp.Y1 - 2
				maxTop := len(cp.lines) - h
				if maxTop < 0 {
					maxTop = 0
				}
				if cp.topPos > maxTop {
					cp.topPos = maxTop
				}
			}
			vtui.FrameManager.Redraw()
			return true
		}
		if e.ButtonState == vtinput.FromLeft1stButtonPressed && e.KeyDown {
			mx, my := int(e.MouseX), int(e.MouseY)
			if mx > cp.X1 && mx < cp.X2 && my > cp.Y1 && my < cp.input.Y1-1 {
				// Clicked in chat area
				cp.input.SetFocus(false)
				cp.focusedLinkIdx = -1
				row := my - cp.Y1 - 1
				col := mx - cp.X1 - 1
				for i, link := range cp.visibleLinks {
					if link.row == row && col >= link.col && col < link.col+link.width {
						cp.focusedLinkIdx = i
						if e.MouseEventFlags&vtinput.DoubleClick != 0 {
							cp.navigateToTarget(link.target)
						}
						break
					}
				}
				vtui.FrameManager.Redraw()
				return true
			}
		}
	}
	return false
}

func (cp *AIChatPanel) updateLines() {
	w := cp.X2 - cp.X1 - 1
	if w <= 0 {
		return
	}

	var lines []chatLine
	var session *vtvibe.Session
	if w, ok := cp.src.vfs.(*aiVFSWrapper); ok {
		session = w.Session()
	} else if a, ok := cp.src.vfs.(*vtvibe.AIVFS); ok {
		session = a.Session()
	} else {
		session = aiSession()
	}
	turns := session.Turns()

	attr := vtui.Palette[ColPanelText]
	headerAttr := vtui.Palette[ColPanelTitle]
	linkAttr := vtui.Palette[vtui.ColMenuHighlight]

	highlighter := vtui.GetHighlighter("chat.md", "")
	var hlState any
	bgAttr := vtui.Palette[ColPanelText]

	appendWrapped := func(runes []rune, attrs []uint64, targets []string) {
		col := 0
		var currentCells []vtui.CharInfo
		var currentTargets []string

		for i := 0; i < len(runes); i++ {
			r := runes[i]
			rw := runewidth.RuneWidth(r)
			if rw <= 0 {
				rw = 1
			}

			if col+rw > w-2 {
				lines = append(lines, chatLine{cells: currentCells, targets: currentTargets})
				currentCells = nil
				currentTargets = nil
				col = 0
			}

			charVal := uint64(r)
			for j := 0; j < rw; j++ {
				currentCells = append(currentCells, vtui.CharInfo{Char: charVal, Attributes: attrs[i]})
				currentTargets = append(currentTargets, targets[i])
				charVal = uint64(vtui.WideCharFiller)
			}
			col += rw
		}
		if len(currentCells) > 0 {
			lines = append(lines, chatLine{cells: currentCells, targets: currentTargets})
		}
	}

	makePlainLine := func(text string, attr uint64) chatLine {
		var cells []vtui.CharInfo
		var targets []string
		for _, r := range text {
			rw := runewidth.RuneWidth(r)
			if rw <= 0 {
				rw = 1
			}
			charVal := uint64(r)
			for j := 0; j < rw; j++ {
				cells = append(cells, vtui.CharInfo{Char: charVal, Attributes: attr})
				targets = append(targets, "")
				charVal = uint64(vtui.WideCharFiller)
			}
		}
		return chatLine{cells: cells, targets: targets}
	}

	for _, t := range turns {
		if t.Role == "user" {
			lines = append(lines, makePlainLine("▸ "+Msg("AI.ChatYou")+"  "+t.Time.Format("15:04"), headerAttr))
		} else {
			lines = append(lines, makePlainLine("▾ "+Msg("AI.ChatModel")+"  "+t.Time.Format("15:04"), headerAttr))
		}

		for _, p := range strings.Split(t.Text, "\n") {
			var lineSyntax []uint64
			if highlighter != nil {
				lineSyntax, hlState = highlighter.Highlight(p, hlState, bgAttr)
			}

			runesSrc := []rune("  " + p)
			syntaxPad := []uint64{attr, attr}
			fullSyntax := append(syntaxPad, lineSyntax...)

			var runes []rune
			var attrs []uint64
			var targets []string

			i := 0
			for i < len(runesSrc) {
				// Parse markdown inline links [text](url)
				if runesSrc[i] == '[' {
					closeBracket := -1
					for j := i + 1; j < len(runesSrc); j++ {
						if runesSrc[j] == ']' {
							closeBracket = j
							break
						}
					}
					if closeBracket != -1 && closeBracket+1 < len(runesSrc) && runesSrc[closeBracket+1] == '(' {
						closeParen := -1
						for j := closeBracket + 2; j < len(runesSrc); j++ {
							if runesSrc[j] == ')' {
								closeParen = j
								break
							}
						}
						if closeParen != -1 {
							text := string(runesSrc[i+1 : closeBracket])
							target := string(runesSrc[closeBracket+2 : closeParen])
							for _, r := range text {
								runes = append(runes, r)
								attrs = append(attrs, linkAttr)
								targets = append(targets, target)
							}
							i = closeParen + 1
							continue
						}
					}
				}

				// Parse raw ai:// URLs
				if i+5 <= len(runesSrc) && string(runesSrc[i:i+5]) == "ai://" {
					end := i
					for end < len(runesSrc) && runesSrc[end] > 32 && runesSrc[end] != ')' && runesSrc[end] != ']' {
						end++
					}
					target := string(runesSrc[i:end])
					for _, r := range target {
						runes = append(runes, r)
						attrs = append(attrs, linkAttr)
						targets = append(targets, target)
					}
					i = end
					continue
				}

				// Default syntax mapping
				curAttr := attr
				if i < len(fullSyntax) {
					curAttr = fullSyntax[i]
					// If Colorer applied default bg, ensure it blends with panel text bg
					if curAttr&vtui.IsBgRGB == 0 && vtui.GetIndexBack(curAttr) == vtui.GetIndexBack(vtui.Palette[ColEditorText]) {
						curAttr = vtui.SetIndexBack(curAttr, vtui.GetIndexBack(attr))
					}
				}
				runes = append(runes, runesSrc[i])
				attrs = append(attrs, curAttr)
				targets = append(targets, "")
				i++
			}

			// Add virtual link for explicit file output markers (```go:filename)
			pStr := strings.TrimSpace(p)
			if strings.HasPrefix(pStr, "```") {
				colon := strings.Index(pStr, ":")
				if colon != -1 {
					filename := strings.TrimSpace(pStr[colon+1:])
					if filename != "" {
						target := "ai://out/" + filename
						tr := []rune("  " + target)
						for j := 0; j < len(tr); j++ {
							if j < 2 {
								runes = append(runes, tr[j])
								attrs = append(attrs, attr)
								targets = append(targets, "")
							} else {
								runes = append(runes, tr[j])
								attrs = append(attrs, linkAttr)
								targets = append(targets, target)
							}
						}
					}
				}
			}

			appendWrapped(runes, attrs, targets)
		}
		lines = append(lines, chatLine{})
	}

	if session.Busy() {
		lines = append(lines, makePlainLine("▸ "+Msg("AI.ChatTyping"), headerAttr))
	}

	cp.lines = lines
}

func (cp *AIChatPanel) Show(scr *vtui.ScreenBuf) {
	cp.frame.Show(scr)
	cp.updateLines()

	x1, y1 := cp.X1+1, cp.Y1+1
	x2 := cp.X2 - 1

	attrBox := vtui.Palette[ColPanelBox]
	vtui.NewPainter(scr).DrawLine(cp.X1+1, cp.input.Y1-1, cp.X2-1, cp.input.Y1-1, '─', attrBox, false, false)
	scr.Write(cp.X1, cp.input.Y1-1, vtui.StringToCharInfo("├", attrBox))
	scr.Write(cp.X2, cp.input.Y1-1, vtui.StringToCharInfo("┤", attrBox))

	h := cp.input.Y1 - cp.Y1 - 2
	if h > 0 {
		maxTop := len(cp.lines) - h
		if maxTop < 0 {
			maxTop = 0
		}
		if cp.topPos > maxTop {
			cp.topPos = maxTop
		}

		cp.visibleLinks = nil
		for i := 0; i < h; i++ {
			idx := cp.topPos + i
			if idx >= len(cp.lines) {
				break
			}
			line := cp.lines[idx]

			vtui.NewPainter(scr).Fill(x1, y1+i, x2, y1+i, ' ', vtui.Palette[ColPanelText])

			col := 0
			for j := 0; j < len(line.cells); j++ {
				target := line.targets[j]

				if target != "" {
					startCol := col
					for j < len(line.cells) && line.targets[j] == target {
						if cp.focused && cp.focusedLinkIdx == len(cp.visibleLinks) {
							line.cells[j].Attributes = vtui.SetIndexBack(line.cells[j].Attributes, vtui.GetIndexBack(vtui.Palette[ColPanelCursor]))
						}
						j++
						col++
					}
					cp.visibleLinks = append(cp.visibleLinks, chatLink{
						row: i, col: startCol, width: col - startCol, target: target,
					})
					j-- // Step back since the outer loop will increment
				} else {
					col++
				}
			}

			writeLen := len(line.cells)
			if writeLen > x2-x1+1 {
				writeLen = x2 - x1 + 1
			}
			scr.Write(x1, y1+i, line.cells[:writeLen])
		}
	}

	if cp.focusedLinkIdx >= len(cp.visibleLinks) {
		cp.focusedLinkIdx = -1
		if cp.focused {
			cp.input.SetFocus(true)
		}
	}

	cp.input.Show(scr)
}
func cellCutChat(s string, width int) int {
	if width <= 0 || s == "" {
		return len(s)
	}
	used := 0
	for i := 0; i < len(s); {
		r, sz := utf8.DecodeRuneInString(s[i:])
		w := runewidth.RuneWidth(r)
		if used+w > width {
			return i
		}
		used += w
		i += sz
	}
	return len(s)
}
