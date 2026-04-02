package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/unxed/vtinput"
	"github.com/unxed/vtui"
)

var MacroMgr *MacroManager

// MacroManager handles recording, playback and storage of simple keyboard macros.
type MacroManager struct {
	Macros    map[string][]*vtinput.InputEvent
	Recording bool
	Buffer    []*vtinput.InputEvent
	iniPath   string
}

func NewMacroManager(iniPath string) *MacroManager {
	mgr := &MacroManager{
		Macros:  make(map[string][]*vtinput.InputEvent),
		iniPath: iniPath,
	}
	mgr.Load()
	return mgr
}

func normalizeMods(mods uint32) uint32 {
	var n uint32
	if mods&(vtinput.LeftCtrlPressed|vtinput.RightCtrlPressed) != 0 {
		n |= vtinput.LeftCtrlPressed
	}
	if mods&(vtinput.LeftAltPressed|vtinput.RightAltPressed) != 0 {
		n |= vtinput.LeftAltPressed
	}
	if mods&vtinput.ShiftPressed != 0 {
		n |= vtinput.ShiftPressed
	}
	return n
}

func KeyStr(vk uint16, mods uint32) string {
	return fmt.Sprintf("%X:%X", vk, normalizeMods(mods))
}

// Filter is hooked into FrameManager. Returns true if the event was consumed.
func (m *MacroManager) Filter(e *vtinput.InputEvent) bool {
	if e.Type != vtinput.KeyEventType {
		return false
	}

	// Ctrl+. toggles recording. We check both VK and Char for better terminal compatibility.
	isCtrlDot := (e.VirtualKeyCode == vtinput.VK_OEM_PERIOD || e.Char == '.') &&
		(e.ControlKeyState&(vtinput.LeftCtrlPressed|vtinput.RightCtrlPressed)) != 0

	if isCtrlDot {
		if !e.KeyDown {
			return true // Consume KeyUp of the trigger
		}
		vtui.DebugLog("MACRO: Ctrl+. intercepted. Previous recording state: %v", m.Recording)
		if m.Recording {
			m.Recording = false
			vtui.FrameManager.PostTask(func() {
				m.showAssignDialog()
			})
		} else {
			m.Recording = true
			m.Buffer = make([]*vtinput.InputEvent, 0)
			vtui.DebugLog("MACRO: Started recording")
		}
		vtui.DebugLog("MACRO: Current Recording state: %v", m.Recording)
		vtui.FrameManager.Redraw()
		return true // Trigger is ALWAYS consumed
	}
	
	if m.Recording {
		if e.KeyDown {
			m.Buffer = append(m.Buffer, e)
		}
		return false // Let it pass to the UI so user sees what they type
	}

	if !e.KeyDown {
		return false
	}

	// Check if this key triggers a macro
	if seq, ok := m.Macros[KeyStr(e.VirtualKeyCode, e.ControlKeyState)]; ok {
		vtui.DebugLog("MACRO: Playing back macro for %s", KeyStr(e.VirtualKeyCode, e.ControlKeyState))
		vtui.FrameManager.InjectEvents(seq)
		return true
	}

	return false
}

func (m *MacroManager) showAssignDialog() {
	frame := &MacroAssignFrame{mgr: m}
	vtui.FrameManager.Push(frame)
}

func (m *MacroManager) Load() {
	vtui.DebugLog("MACRO: Loading macros from %s", m.iniPath)
	m.Macros = make(map[string][]*vtinput.InputEvent)
	ini := LoadIni(m.iniPath)
	if sec, ok := ini.data["Macros"]; ok {
		for key, val := range sec {
			parts := strings.Split(val, ",")
			var events []*vtinput.InputEvent
			for _, p := range parts {
				fields := strings.Split(p, ":")
				if len(fields) == 3 {
					char, _ := strconv.Atoi(fields[0])
					vk, _ := strconv.Atoi(fields[1])
					mods, _ := strconv.Atoi(fields[2])
					events = append(events, &vtinput.InputEvent{
						Type:            vtinput.KeyEventType,
						KeyDown:         true,
						Char:            rune(char),
						VirtualKeyCode:  uint16(vk),
						ControlKeyState: uint32(mods),
					})
				}
			}
			m.Macros[key] = events
		}
	}
}

func (m *MacroManager) Save() {
	vtui.DebugLog("MACRO: Saving macros to %s", m.iniPath)
	f, err := os.Create(m.iniPath)
	if err != nil {
		return
	}
	defer f.Close()

	fmt.Fprintln(f, "[Macros]")
	for key, seq := range m.Macros {
		var parts []string
		for _, e := range seq {
			parts = append(parts, fmt.Sprintf("%d:%d:%d", e.Char, e.VirtualKeyCode, normalizeMods(e.ControlKeyState)))
		}
		fmt.Fprintf(f, "%s=%s\n", key, strings.Join(parts, ","))
	}
}

// MacroAssignFrame is a modal frame that captures a key combination to assign a macro.
type MacroAssignFrame struct {
	vtui.BaseFrame
	mgr  *MacroManager
}

func (f *MacroAssignFrame) Show(scr *vtui.ScreenBuf) {
	f.BaseFrame.Show(scr)

	w, h := 42, 5
	x := (scr.Width() - w) / 2
	y := (scr.Height() - h) / 2

	box := vtui.NewBorderedFrame(x, y, x+w-1, y+h-1, vtui.DoubleBox, Msg("Macro.AssignTitle"))
	box.ColorBoxIdx = ColPanelBox
	box.ColorTitleIdx = ColPanelTitle
	box.DisplayObject(scr)

	msg := Msg("Macro.AssignPrompt")
	scr.Write(x+(w-len(msg))/2, y+2, vtui.StringToCharInfo(msg, vtui.Palette[ColPanelText]))
}

func (f *MacroAssignFrame) ProcessKey(e *vtinput.InputEvent) bool {
	if !e.KeyDown {
		return false
	}

	// Ignore standalone modifiers
	switch e.VirtualKeyCode {
	case vtinput.VK_SHIFT, vtinput.VK_LSHIFT, vtinput.VK_RSHIFT,
		vtinput.VK_CONTROL, vtinput.VK_LCONTROL, vtinput.VK_RCONTROL,
		vtinput.VK_MENU, vtinput.VK_LMENU, vtinput.VK_RMENU,
		vtinput.VK_CAPITAL, vtinput.VK_NUMLOCK, vtinput.VK_SCROLL:
		return false
	case vtinput.VK_ESCAPE:
		f.SetExitCode(-1)
		return true
	}

	key := KeyStr(e.VirtualKeyCode, e.ControlKeyState)
	f.mgr.Macros[key] = f.mgr.Buffer
	f.mgr.Buffer = nil
	f.mgr.Save()
	f.SetExitCode(0)
	vtui.FrameManager.Redraw()
	return true
}

func (f *MacroAssignFrame) ProcessMouse(e *vtinput.InputEvent) bool {
	return true // Block clicks from falling through
}
func (f *MacroAssignFrame) GetType() vtui.FrameType { return vtui.TypeDialog }
func (f *MacroAssignFrame) IsModal() bool           { return true }
func (f *MacroAssignFrame) GetTitle() string        { return "Macro Assign" }
