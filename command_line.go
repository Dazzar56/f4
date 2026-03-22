package main

import (
	"github.com/unxed/vtinput"
	"github.com/unxed/vtui"
)

// CommandLine is a simplified Edit control used for shell input.
type CommandLine struct {
	vtui.ScreenObject
	Edit       *vtui.Edit
	Prompt     string
	RichPrompt []vtui.CharInfo
	History    []string
	historyPos int
}

func NewCommandLine(prompt string) *CommandLine {
	cl := &CommandLine{
		Prompt:     prompt,
		Edit:       vtui.NewEdit(0, 0, 10, ""),
		historyPos: -1,
	}
	cl.Edit.ColorTextIdx = ColCommandLineText
	cl.Edit.ColorUnchangedIdx = ColCommandLineText
	cl.Edit.ColorSelectedIdx = ColCommandLineSelectedText
	cl.Edit.SetCanFocus(true)
	return cl
}

func (cl *CommandLine) SetPosition(x1, y1, x2, y2 int) {
	cl.ScreenObject.SetPosition(x1, y1, x2, y2)
	promptLen := 0
	if len(cl.RichPrompt) > 0 {
		for _, c := range cl.RichPrompt {
			if c.Char != vtui.WideCharFiller {
				promptLen++
			}
		}
	} else {
		promptLen = len(cl.Prompt)
	}
	cl.Edit.SetPosition(x1+promptLen, y1, x2, y2)
}

func (cl *CommandLine) SetFocus(f bool) {
	cl.ScreenObject.SetFocus(f)
	cl.Edit.SetFocus(f)
}
func (cl *CommandLine) SetPrompt(prompt string) {
	cl.RichPrompt = nil
	if cl.Prompt == prompt { return }
	cl.Prompt = prompt
	// Trigger reposition of Edit control
	cl.SetPosition(cl.X1, cl.Y1, cl.X2, cl.Y2)
}

func (cl *CommandLine) SetRichPrompt(prompt []vtui.CharInfo) {
	cl.RichPrompt = prompt
	cl.Prompt = ""
	cl.SetPosition(cl.X1, cl.Y1, cl.X2, cl.Y2)
}

func (cl *CommandLine) Show(scr *vtui.ScreenBuf) {
	cl.ScreenObject.Show(scr)
	cl.DisplayObject(scr)
}

func (cl *CommandLine) DisplayObject(scr *vtui.ScreenBuf) {
	if !cl.IsVisible() { return }

	// 1. Draw Prompt
	if len(cl.RichPrompt) > 0 {
		scr.Write(cl.X1, cl.Y1, cl.RichPrompt)
	} else if cl.Prompt != "" {
		scr.Write(cl.X1, cl.Y1, vtui.StringToCharInfo(cl.Prompt, vtui.Palette[ColCommandLinePrompt]))
	}

	// 2. Draw Edit (input field)
	cl.Edit.Show(scr)
}

func (cl *CommandLine) ProcessKey(e *vtinput.InputEvent) bool {
	handled := cl.Edit.ProcessKey(e)
	if handled && cl.historyPos != -1 {
		// If a key was handled by the edit control, it means the text was modified.
		// We should exit history browsing mode.
		// We exclude simple cursor movements from this logic.
		isNav := false
		switch e.VirtualKeyCode {
		case vtinput.VK_LEFT, vtinput.VK_RIGHT, vtinput.VK_HOME, vtinput.VK_END:
			isNav = true
		}
		if !isNav {
			cl.historyPos = -1
		}
	}
	return handled
}

func (cl *CommandLine) ProcessMouse(e *vtinput.InputEvent) bool {
	return cl.Edit.ProcessMouse(e)
}
// Clear empties the command line text.
func (cl *CommandLine) Clear() {
	cl.Edit.SetText("")
}

// IsEmpty returns true if there is no text in the command line.
func (cl *CommandLine) IsEmpty() bool {
	return cl.Edit.GetText() == ""
}
// InsertString adds text to the command line.
func (cl *CommandLine) InsertString(text string) {
	cl.Edit.InsertString(text)
}
// AddHistory adds a new command to the top of the history list.
func (cl *CommandLine) AddHistory(cmd string) {
	if cmd == "" {
		return
	}
	// Avoid adding duplicates at the top
	if len(cl.History) > 0 && cl.History[0] == cmd {
		return
	}
	cl.History = append([]string{cmd}, cl.History...)
	// Limit history size to 100 entries
	if len(cl.History) > 100 {
		cl.History = cl.History[:100]
	}
}

// HistoryUp moves to the previous command in history.
func (cl *CommandLine) HistoryUp() {
	if len(cl.History) == 0 {
		return
	}
	if cl.historyPos < len(cl.History)-1 {
		cl.historyPos++
		cl.Edit.SetText(cl.History[cl.historyPos])
	}
}

// HistoryDown moves to the next command in history, or clears the line.
func (cl *CommandLine) HistoryDown() {
	if cl.historyPos > 0 {
		cl.historyPos--
		cl.Edit.SetText(cl.History[cl.historyPos])
	} else if cl.historyPos == 0 {
		cl.historyPos = -1
		cl.Edit.SetText("")
	}
}
