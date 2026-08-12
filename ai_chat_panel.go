package main

import (
	"strings"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
	"github.com/unxed/f4/vtvibe"
	"github.com/unxed/vtinput"
	"github.com/unxed/vtui"
)

// AIChatPanel implements AltPanel interface. It perfectly overlays
// the logical AI FileSystemPanel without modifying core file listing mechanics.
type AIChatPanel struct {
	vtui.ScreenObject
	src     *FileSystemPanel
	frame   *vtui.BorderedFrame
	input   *vtui.MultiLineEdit
	focused bool
	topPos  int
	lines   []chatLine
}

type chatLine struct {
	text string
	attr uint64
}

func NewAIChatPanel(src *FileSystemPanel) *AIChatPanel {
	x1, y1, x2, y2 := src.GetPosition()
	cp := &AIChatPanel{
		src:   src,
		frame: vtui.NewBorderedFrame(x1, y1, x2, y2, vtui.SingleBox, Msg("AI.ChatTitle")),
		input: vtui.NewMultiLineEdit(0, 0, 10, 3, ""),
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

func (cp *AIChatPanel) ProcessKey(e *vtinput.InputEvent) bool {
	if !e.KeyDown || !cp.focused {
		return false
	}

	ctrl := (e.ControlKeyState & (vtinput.LeftCtrlPressed | vtinput.RightCtrlPressed)) != 0
	alt := (e.ControlKeyState & (vtinput.LeftAltPressed | vtinput.RightAltPressed)) != 0
	shift := (e.ControlKeyState & vtinput.ShiftPressed) != 0

	if e.VirtualKeyCode == vtinput.VK_RETURN && ctrl && !alt && !shift {
		text := cp.input.GetText()
		if strings.TrimSpace(text) != "" {
			aiSend(findPanelsFrameAnyScreen(), text)
			cp.input.SetText("")
		}
		return true
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
			row, _ := cp.input.CursorPos()
			if row == 0 && cp.topPos > 0 {
				cp.topPos--
				vtui.FrameManager.Redraw()
				return true
			}
		}
	case vtinput.VK_DOWN:
		if !ctrl && !alt && !shift {
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

	if cp.input.ProcessKey(e) {
		return true
	}

	return false
}

func (cp *AIChatPanel) ProcessMouse(e *vtinput.InputEvent) bool {
	if cp.input.ProcessMouse(e) {
		return true
	}
	if e.Type == vtinput.MouseEventType && e.WheelDirection != 0 {
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
	return false
}

func (cp *AIChatPanel) updateLines() {
	w := cp.X2 - cp.X1 - 1
	if w <= 0 {
		return
	}

	var lines []chatLine
	aiVfs, ok := cp.src.vfs.(*vtvibe.AIVFS)
	if !ok {
		return
	}
	session := aiVfs.Session()
	turns := session.Turns()

	attr := vtui.Palette[ColPanelText]
	headerAttr := vtui.Palette[ColPanelTitle]

	for _, t := range turns {
		if t.Role == "user" {
			lines = append(lines, chatLine{text: "▸ " + Msg("AI.ChatYou") + "  " + t.Time.Format("15:04"), attr: headerAttr})
		} else {
			lines = append(lines, chatLine{text: "▾ " + Msg("AI.ChatModel") + "  " + t.Time.Format("15:04"), attr: headerAttr})
		}

		for _, p := range strings.Split(t.Text, "\n") {
			for len(p) > 0 {
				cut := cellCutChat(p, w-2)
				if cut == 0 {
					cut = len(p)
				}
				lines = append(lines, chatLine{text: "  " + p[:cut], attr: attr})
				p = p[cut:]
			}
		}
		lines = append(lines, chatLine{text: "", attr: attr})
	}

	if session.Busy() {
		lines = append(lines, chatLine{text: "▸ " + Msg("AI.ChatTyping"), attr: headerAttr})
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

		for i := 0; i < h; i++ {
			idx := cp.topPos + i
			if idx >= len(cp.lines) {
				break
			}
			line := cp.lines[idx]

			vtui.NewPainter(scr).Fill(x1, y1+i, x2, y1+i, ' ', line.attr)
			str := runewidth.Truncate(line.text, x2-x1+1, "")
			scr.Write(x1, y1+i, vtui.StringToCharInfo(str, line.attr))
		}
	}

	cp.input.SetFocus(cp.focused)
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
