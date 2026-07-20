package main

import (
	"fmt"
	"os"
	"path/filepath"
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

func normalizeMods(mods vtinput.ControlKeyState) vtinput.ControlKeyState {
	var n vtinput.ControlKeyState
	if mods.Contains(vtinput.LeftCtrlPressed | vtinput.RightCtrlPressed) {
		n |= vtinput.LeftCtrlPressed
	}

	if mods.Contains(vtinput.LeftAltPressed | vtinput.RightAltPressed) {
		n |= vtinput.LeftAltPressed
	}

	if mods.Contains(vtinput.ShiftPressed) {
		n |= vtinput.ShiftPressed
	}
	return n
}

func KeyStr(vk uint16, mods vtinput.ControlKeyState) string {
	return fmt.Sprintf("%X:%X", vk, uint32(normalizeMods(mods)))
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
	frame := NewMacroAssignFrame(m)
	vtui.FrameManager.Push(frame)
}

func (m *MacroManager) Load() {
	vtui.DebugLog("MACRO: Loading macros from %s", m.iniPath)
	newMacros := make(map[string][]*vtinput.InputEvent)
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
						ControlKeyState: vtinput.ControlKeyState(mods),
					})
				}
			}
			newMacros[key] = events
		}
	}
	m.Macros = newMacros
}

func (m *MacroManager) Save() {
	vtui.DebugLog("MACRO: Saving macros to %s", m.iniPath)

	var sb strings.Builder
	sb.WriteString("[Macros]\n")
	for key, seq := range m.Macros {
		var parts []string
		for _, e := range seq {
			parts = append(parts, fmt.Sprintf("%d:%d:%d", e.Char, e.VirtualKeyCode, normalizeMods(e.ControlKeyState)))
		}
		sb.WriteString(fmt.Sprintf("%s=%s\n", key, strings.Join(parts, ",")))
	}

	os.MkdirAll(filepath.Dir(m.iniPath), 0755)
	err := os.WriteFile(m.iniPath, []byte(sb.String()), 0644)
	if err != nil {
		vtui.DebugLog("MACRO: Failed to save: %v", err)
	}
}

// MacroAssignFrame is a modal frame that captures a key combination to assign a macro.
type MacroAssignFrame struct {
	vtui.Window
	mgr *MacroManager
}

func NewMacroAssignFrame(m *MacroManager) *MacroAssignFrame {
	width, height := 42, 7
	base := vtui.NewCenteredDialog(width, height, Msg("Macro.AssignTitle"))
	f := &MacroAssignFrame{
		Window: *base,
		mgr:    m,
	}

	prompt := vtui.NewText(0, 0, Msg("Macro.AssignPrompt"), vtui.Palette[vtui.ColDialogText])
	f.AddItem(prompt)

	cancelPrompt := vtui.NewText(0, 0, Msg("Macro.AssignCancel"), vtui.Palette[vtui.ColDialogText])
	f.AddItem(cancelPrompt)

	vbox := vtui.NewVBoxLayout(f.X1+2, f.Y1+2, width-4, height-4)
	vbox.Add(prompt, vtui.Margins{}, vtui.AlignCenter)
	vbox.Add(cancelPrompt, vtui.Margins{Top: 1}, vtui.AlignCenter)
	vbox.Apply()

	return f
}

func (f *MacroAssignFrame) ProcessKey(e *vtinput.InputEvent) bool {
	if e.Type == vtinput.FocusEventType {
		return f.Window.ProcessKey(e)
	}

	if !e.KeyDown {
		return false
	}

	if e.VirtualKeyCode == vtinput.VK_ESCAPE {
		f.mgr.Buffer = nil
		f.SetExitCode(-1)
		vtui.FrameManager.Redraw()
		return true
	}

	// Only ignore "pure" modifiers without any other key.
	// Everything else (including Esc and Alt-combos) can be a macro.
	switch e.VirtualKeyCode {
	case vtinput.VK_SHIFT, vtinput.VK_LSHIFT, vtinput.VK_RSHIFT,
		vtinput.VK_CONTROL, vtinput.VK_LCONTROL, vtinput.VK_RCONTROL,
		vtinput.VK_MENU, vtinput.VK_LMENU, vtinput.VK_RMENU,
		vtinput.VK_CAPITAL, vtinput.VK_NUMLOCK, vtinput.VK_SCROLL:
		return false
	}

	key := KeyStr(e.VirtualKeyCode, e.ControlKeyState)
	if f.mgr.Macros == nil {
		f.mgr.Macros = make(map[string][]*vtinput.InputEvent)
	}
	if len(f.mgr.Buffer) == 0 {
		delete(f.mgr.Macros, key)
	} else {
		f.mgr.Macros[key] = f.mgr.Buffer
	}
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
