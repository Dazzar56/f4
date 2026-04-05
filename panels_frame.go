package main

import (
	"os"
	"fmt"
	"time"
	"github.com/unxed/f4/vfs"
	"sync"
	"os/user"
	"strings"
	"context"

	"github.com/mattn/go-runewidth"

	"github.com/unxed/vtinput"
	"github.com/unxed/vtui"
)

type DriveEntry struct {
	Name    string
	Factory func() vfs.VFS
}
var DriveRegistry []DriveEntry

func RegisterDrive(name string, factory func() vfs.VFS) {
	DriveRegistry = append(DriveRegistry, DriveEntry{Name: name, Factory: factory})
}

type HotkeyEntry struct {
	VK      uint16
	Mods    uint32
	Handler func(app vfs.App)
}
var GlobalHotkeys []HotkeyEntry

func RegisterGlobalHotkey(vk uint16, mods uint32, handler func(app vfs.App)) {
	GlobalHotkeys = append(GlobalHotkeys, HotkeyEntry{VK: vk, Mods: mods, Handler: handler})
}
func (pf *PanelsFrame) GetActivePanelVFS() vfs.VFS  { return pf.Active().(*FileSystemPanel).vfs }
func (pf *PanelsFrame) GetPassivePanelVFS() vfs.VFS { return pf.Passive().(*FileSystemPanel).vfs }
func (pf *PanelsFrame) GetSelectedNames() []string { return pf.Active().(*FileSystemPanel).GetSelectedNames() }
func (pf *PanelsFrame) GetSelectedName() string   { return pf.Active().(*FileSystemPanel).GetSelectedName() }

type PanelController interface {
	ProcessPanelKey(pf *PanelsFrame, e *vtinput.InputEvent) bool
}

// A Panel is an interface for any content that can be placed in the "half" of the manager.
// This could be a file list, a folder tree, or even a quick view panel (Viewer).
type Panel interface {
	Show(scr *vtui.ScreenBuf)
	ProcessKey(e *vtinput.InputEvent) bool
	ProcessMouse(e *vtinput.InputEvent) bool
	SetFocus(f bool)
	IsFocused() bool
	SetPosition(x1, y1, x2, y2 int)
	GetPosition() (int, int, int, int)
	GetSelectedName() string
}
// PanelsFrame is the main frame of the f4 manager, containing left and right panels.
type PanelsFrame struct {
	vtui.BaseFrame
	panels    [2]Panel
	activeIdx int // 0 for left, 1 for right
	executing bool

	menuBar   *vtui.MenuBar
	cmdLine   *CommandLine
	keyBar    *vtui.KeyBar

	showKeyBar bool
	showPanels bool
	lastW      int
	lastH      int

	// Integrated Terminal
	pty        PtyBackend
	remotePtys map[vfs.VFS]PtyBackend
	ptyMutex   sync.Mutex
	termView   *TerminalView
	parser     *AnsiParser

	lastAlt   bool
}
func (pf *PanelsFrame) Left() Panel  { return pf.panels[0] }
func (pf *PanelsFrame) Right() Panel { return pf.panels[1] }
func (pf *PanelsFrame) Active() Panel  { return pf.panels[pf.activeIdx] }
func (pf *PanelsFrame) Passive() Panel { return pf.panels[1-pf.activeIdx] }

func NewPanelsFrame() *PanelsFrame {
	pf := &PanelsFrame{activeIdx: 1}
	pf.SetHelp("Panels")
	pf.showKeyBar = true
	pf.showPanels = true

	pf.menuBar = vtui.NewMenuBar(nil)
	pf.menuBar.SetOwner(pf)
	pf.menuBar.Items = []vtui.MenuBarItem{
		// Using Command routing (TV style) instead of hardcoded indices
		{Label: "&" + Msg("Menu.Left"), SubItems: []vtui.MenuItem{
			{Text: "&" + Msg("Menu.Left.Medium"), Command: CmLeftMedium},
			{Text: "&" + Msg("Menu.Left.Detailed"), Command: CmLeftDetailed},
			{Separator: true},
			{Text: "&" + Msg("Menu.SortName"), Shortcut: "Ctrl+F3", Command: CmLeftSortName},
			{Text: "&" + Msg("Menu.SortExt"), Shortcut: "Ctrl+F4", Command: CmLeftSortExt},
			{Text: "&" + Msg("Menu.SortTime"), Shortcut: "Ctrl+F5", Command: CmLeftSortTime},
			{Text: "&" + Msg("Menu.SortSize"), Shortcut: "Ctrl+F6", Command: CmLeftSortSize},
			{Text: "&" + Msg("Menu.SortUnsorted"), Shortcut: "Ctrl+F7", Command: CmLeftSortUnsorted},
			{Separator: true},
			{Text: "Bac&kground", Command: CmBackground},
			{Text: Msg("Menu.Exit"), Command: vtui.CmQuit},
		}},
		{Label: "&" + Msg("Menu.Files"), SubItems: []vtui.MenuItem{
			{Text: "&" + Msg("Menu.Files.View"), Shortcut: "F3", Command: CmView},
			{Text: "&" + Msg("Menu.Files.Edit"), Shortcut: "F4", Command: CmEdit},
			{Text: "&" + Msg("Menu.Files.Copy"), Shortcut: "F5", Command: CmCopy},
			{Text: "&" + Msg("Menu.Files.RenMov"), Shortcut: "F6", Command: CmMove},
			{Text: "&" + Msg("Menu.Files.MkDir"), Shortcut: "F7", Command: CmMkDir},
			{Text: "&" + Msg("Menu.Files.Delete"), Shortcut: "F8", Command: CmDelete},
		}},
		{Label: "&" + Msg("Menu.Commands"), SubItems: []vtui.MenuItem{
			{Text: "&" + Msg("Menu.Commands.FindFile"), Shortcut: "Alt+F7", Command: CmFindFile},
		}},
		{Label: "&" + Msg("Menu.Options"), SubItems: []vtui.MenuItem{{Text: "Placeholder"}}},
		{Label: "&" + Msg("Menu.Right"), SubItems: []vtui.MenuItem{
			{Text: "&" + Msg("Menu.Left.Medium"), Command: CmRightMedium},
			{Text: "&" + Msg("Menu.Left.Detailed"), Command: CmRightDetailed},
			{Separator: true},
			{Text: "&" + Msg("Menu.SortName"), Shortcut: "Ctrl+F3", Command: CmRightSortName},
			{Text: "&" + Msg("Menu.SortExt"), Shortcut: "Ctrl+F4", Command: CmRightSortExt},
			{Text: "&" + Msg("Menu.SortTime"), Shortcut: "Ctrl+F5", Command: CmRightSortTime},
			{Text: "&" + Msg("Menu.SortSize"), Shortcut: "Ctrl+F6", Command: CmRightSortSize},
			{Text: "&" + Msg("Menu.SortUnsorted"), Shortcut: "Ctrl+F7", Command: CmRightSortUnsorted},
		}},
	}
	// We no longer need pf.menuBar.OnCommand for routing!
	pf.cmdLine = NewCommandLine(Msg("Panels.Prompt"))
	pf.keyBar = vtui.NewKeyBar()
	pf.keyBar.SetOwner(pf)

	pf.termView = NewTerminalView(80, 24)
	// Parser will be fully initialized in initPTY once pty is ready
	pf.initPTY()


	return pf
}

func getMenuText(current, target ViewMode, label string) string {
	if current == target { return "√" + label }
	return " " + label
}

func getSortMenuText(current, target SortMode, label string) string {
	if current == target { return "√" + label }
	return " " + label
}

func (pf *PanelsFrame) updateMenuCheckmarks() {
	if pf.panels[0] == nil || pf.panels[1] == nil { return }

	lMode, rMode := ViewModeMedium, ViewModeMedium
	lSort, rSort := SortName, SortName
	if fsp, ok := pf.panels[0].(*FileSystemPanel); ok { lMode = fsp.viewMode; lSort = fsp.sortMode }
	if fsp, ok := pf.panels[1].(*FileSystemPanel); ok { rMode = fsp.viewMode; rSort = fsp.sortMode }

	pf.menuBar.Items[0].SubItems[0].Text = getMenuText(lMode, ViewModeMedium, "&"+Msg("Menu.Left.Medium"))
	pf.menuBar.Items[0].SubItems[1].Text = getMenuText(lMode, ViewModeDetailed, "&"+Msg("Menu.Left.Detailed"))
	pf.menuBar.Items[0].SubItems[3].Text = getSortMenuText(lSort, SortName, "&"+Msg("Menu.SortName"))
	pf.menuBar.Items[0].SubItems[4].Text = getSortMenuText(lSort, SortExt, "&"+Msg("Menu.SortExt"))
	pf.menuBar.Items[0].SubItems[5].Text = getSortMenuText(lSort, SortTime, "&"+Msg("Menu.SortTime"))
	pf.menuBar.Items[0].SubItems[6].Text = getSortMenuText(lSort, SortSize, "&"+Msg("Menu.SortSize"))
	pf.menuBar.Items[0].SubItems[7].Text = getSortMenuText(lSort, SortUnsorted, "&"+Msg("Menu.SortUnsorted"))

	pf.menuBar.Items[4].SubItems[0].Text = getMenuText(rMode, ViewModeMedium, "&"+Msg("Menu.Left.Medium"))
	pf.menuBar.Items[4].SubItems[1].Text = getMenuText(rMode, ViewModeDetailed, "&"+Msg("Menu.Left.Detailed"))
	pf.menuBar.Items[4].SubItems[3].Text = getSortMenuText(rSort, SortName, "&"+Msg("Menu.SortName"))
	pf.menuBar.Items[4].SubItems[4].Text = getSortMenuText(rSort, SortExt, "&"+Msg("Menu.SortExt"))
	pf.menuBar.Items[4].SubItems[5].Text = getSortMenuText(rSort, SortTime, "&"+Msg("Menu.SortTime"))
	pf.menuBar.Items[4].SubItems[6].Text = getSortMenuText(rSort, SortSize, "&"+Msg("Menu.SortSize"))
	pf.menuBar.Items[4].SubItems[7].Text = getSortMenuText(rSort, SortUnsorted, "&"+Msg("Menu.SortUnsorted"))
}

func (pf *PanelsFrame) buildPrompt() []vtui.CharInfo {
	var path string
	if fsp, ok := pf.Active().(*FileSystemPanel); ok {
		path = fsp.vfs.GetPath()
	}

	usr, _ := user.Current()
	username := "user"
	home := ""
	if usr != nil {
		username = usr.Username
		home = usr.HomeDir
	}

	host, _ := os.Hostname()
	if host == "" { host = "localhost" }

	displayPath := path
	if home != "" && strings.HasPrefix(displayPath, home) {
		displayPath = "~" + displayPath[len(home):]
	}

	baseAttr := vtui.Palette[ColCommandLineUserScreen]
	// Use colors as close as possible to classic bash, while keeping the base background
	greenAttr := vtui.SetRGBFore(baseAttr, 0x8AE234) // Bright green
	blueAttr := vtui.SetRGBFore(baseAttr, 0x729FCF)  // Bright blue
	defAttr := vtui.SetRGBFore(baseAttr, 0xFFFFFF)   // White

	var prompt []vtui.CharInfo
	prompt = append(prompt, vtui.StringToCharInfo(username+"@"+host, greenAttr)...)
	prompt = append(prompt, vtui.StringToCharInfo(":", defAttr)...)
	prompt = append(prompt, vtui.StringToCharInfo(displayPath, blueAttr)...)
	prompt = append(prompt, vtui.StringToCharInfo("$ ", defAttr)...)

	return prompt
}

func (pf *PanelsFrame) initPTY() {
	p, err := NewPTY()
	if err != nil {
		return
	}
	pf.pty = p
	pf.parser = NewAnsiParser(pf.termView, pf.pty)
	shell := GetSystemShell()
	pf.pty.Run(shell)

	// Локальный PTY имеет свой выделенный цикл чтения.
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := pf.pty.Read(buf)
			if err != nil { return }

			pf.ptyMutex.Lock()
			// Отправляем вывод в терминал, только если локальный PTY сейчас активен
			if pf.getActivePTYUnsafe() == pf.pty {
				pf.parser.Process(buf[:n])
				vtui.FrameManager.PostTask(vtui.FrameManager.Redraw)
			}
			pf.ptyMutex.Unlock()
		}
	}()
}


func (pf *PanelsFrame) ResizeConsole(w, h int) {
	pf.lastW, pf.lastH = w, h
	pf.SetPosition(0, 0, w-1, h-1) // Update hit-box for FrameManager hit-testing
	pf.menuBar.SetPosition(0, 0, w-1, 0)

	contentY1 := 0

	// 1. Terminal Area: Fills everything except KeyBar
	termY2 := h - 1
	// KeyBar only takes space if it's actually visible (not in AltScreen)
	if pf.showKeyBar && !pf.termView.UseAltScreen {
		termY2 = h - 2
	}
	termH := termY2 - contentY1 + 1
	if termH < 0 { termH = 0 }

	if pf.pty != nil {
		pf.ptyMutex.Lock()
		pf.pty.SetSize(w, termH)
		for _, remotePty := range pf.remotePtys {
			remotePty.SetSize(w, termH)
		}
		pf.ptyMutex.Unlock()

		pf.termView.SetPosition(0, contentY1, w-1, termY2)
		pf.termView.Resize(w, termH)
	}

	// 2. Panel Area: Leaves one additional line for the f4 CommandLine
	panelY2 := h - 2
	if pf.showKeyBar {
		panelY2 = h - 3
	}
	panelH := panelY2 - contentY1 + 1
	if panelH < 0 { panelH = 0 }

	leftW := w / 2
	rightW := w - leftW

	if pf.panels[0] == nil {
		pf.panels[0] = NewFileSystemPanel(0, contentY1, leftW, panelH, vfs.NewOSVFS("."))
		pf.panels[1] = NewFileSystemPanel(leftW, contentY1, rightW, panelH, vfs.NewOSVFS("."))
	} else {
		pf.panels[0].SetPosition(0, contentY1, leftW-1, panelY2)
		pf.panels[1].SetPosition(leftW, contentY1, w-1, panelY2)

		for i, p := range pf.panels {
			width := leftW
			if i == 1 { width = rightW }
			if fsp, ok := p.(*FileSystemPanel); ok { fsp.Resize(width, panelH) }
		}
	}

	cmdLineY := h - 1
	if pf.showKeyBar {
		// KeyBar on the last line
		pf.keyBar.SetPosition(0, h-1, w-1, h-1)
		pf.keyBar.SetVisible(true)
		cmdLineY = h - 2 // CommandLine is above KeyBar
	} else {
		pf.keyBar.SetVisible(false)
		// CommandLine takes the last line
	}
	// Set CommandLine's base position. Show() will override if in terminal prompt mode.
	pf.cmdLine.SetPosition(0, cmdLineY, w-1, cmdLineY)
	pf.updateMenuCheckmarks()
}

func (pf *PanelsFrame) isPtyBusy() bool {
	active := pf.getActivePTY()
	if active == nil {
		return false
	}
	if active.IsBusy() {
		return true
	}
	// Managed execution signal from actionExecute
	if pf.executing && pf.termView.Title == "f4:busy" {
		return true
	}
	return false
}
func (pf *PanelsFrame) Show(scr *vtui.ScreenBuf) {
	isBusy := pf.isPtyBusy()

	// 0. Process auto-return from managed command execution
	if pf.executing && pf.termView.Title == "f4:done" {
		pf.executing = false
		pf.termView.Title = ""
		pf.showPanels = true
		vtui.FrameManager.Redraw()
		isBusy = false
	}

	// 1. Dynamic Layout Adjustment
	if pf.termView.UseAltScreen != pf.lastAlt {
		pf.lastAlt = pf.termView.UseAltScreen
		pf.ResizeConsole(pf.lastW, pf.lastH)
	}

	if pf.showPanels {
		pf.termView.SetVisible(false)
		for i, p := range pf.panels {
			p.SetFocus(pf.activeIdx == i)
			p.Show(scr)
		}
	} else {
		pf.termView.SetVisible(true)
		pf.termView.Show(scr)
	}

	// Command line logic depends on terminal state and editor visibility
	topType := vtui.FrameManager.GetTopFrameType()
	if (!pf.showPanels && (pf.termView.UseAltScreen || isBusy)) || topType == vtui.TypeUser+2 {
		pf.cmdLine.SetVisible(false)
	} else {
		pf.cmdLine.SetVisible(true)
		cmdLineY := pf.lastH - 1
		if pf.showKeyBar {
			cmdLineY = pf.lastH - 2
		}
		pf.cmdLine.SetRichPrompt(pf.buildPrompt())
		pf.cmdLine.SetPosition(0, cmdLineY, pf.lastW-1, cmdLineY)
		if pf.cmdLine.IsVisible() {
			pf.cmdLine.Show(scr)
		}
	}

	// KeyBar is at the bottom. It should only be hidden if a child process
	// in the terminal is running or using the alternate screen buffer.
	isTop := vtui.FrameManager.GetTopFrameType() == vtui.TypeUser+1
	if isTop { // Only the top-most user frame controls the keybar
		if pf.showKeyBar && !pf.termView.UseAltScreen && !isBusy {
			vtui.FrameManager.KeyBar = pf.keyBar
		} else {
			vtui.FrameManager.KeyBar = nil
		}
	}

	// Macro Recording Indicator
	if MacroMgr != nil && MacroMgr.Recording {
		scr.Write(0, 0, vtui.StringToCharInfo(" R ", vtui.SetRGBBoth(0, 0xFFFFFF, 0xFF0000)))
	}
}

func (pf *PanelsFrame) ProcessKey(e *vtinput.InputEvent) bool {
	ctrl := (e.ControlKeyState & (vtinput.LeftCtrlPressed | vtinput.RightCtrlPressed)) != 0
	alt := (e.ControlKeyState & (vtinput.LeftAltPressed | vtinput.RightAltPressed)) != 0
	shift := (e.ControlKeyState & vtinput.ShiftPressed) != 0

	// Check global hotkeys
	for _, hk := range GlobalHotkeys {
		if e.VirtualKeyCode == hk.VK && e.ControlKeyState == hk.Mods && e.KeyDown {
			hk.Handler(pf)
			return true
		}
	}

	// Panel Controller interception (allows plugins to override default keys)
	if pf.showPanels {
		fsp := pf.getActivePanel()
		if fsp != nil {
			if pc, ok := fsp.vfs.(PanelController); ok {
				if pc.ProcessPanelKey(pf, e) {
					return true
				}
			}
		}
	}

	// Arkanoid easter egg: Ctrl+Alt+A
	if e.VirtualKeyCode == 'A' && alt && ctrl && e.KeyDown {
		vtui.FrameManager.Push(NewArkanoidFrame())
		return true
	}
	// Drive menus
	if e.VirtualKeyCode == vtinput.VK_F1 && alt && !ctrl && !shift && e.KeyDown {
		pf.showDriveMenu(0)
		return true
	}
	if e.VirtualKeyCode == vtinput.VK_F2 && alt && !ctrl && !shift && e.KeyDown {
		pf.showDriveMenu(1)
		return true
	}

	// Alt+F5: Dummy Long Operation for debugging
	if e.VirtualKeyCode == vtinput.VK_F5 && alt && !ctrl && e.KeyDown {
		pf.showDummyOpDialog()
		return true
	}

	// Alt+F7: Find file
	if e.VirtualKeyCode == vtinput.VK_F7 && alt && !ctrl && !shift && e.KeyDown {
		return vtui.FrameManager.EmitCommand(CmFindFile, nil)
	}

	// F11: Plugin Menu
	if e.VirtualKeyCode == vtinput.VK_F11 && !alt && !ctrl && !shift && e.KeyDown {
		pf.showPluginMenu()
		return true
	}

	if e.Type == vtinput.FocusEventType {
		// Propagate focus to command line so its cursor state stays in sync
		pf.cmdLine.ProcessKey(e)
		return true
	}

	// Handle bracketed paste for terminal apps
	if e.Type == vtinput.PasteEventType {
		if !pf.showPanels && pf.termView.BracketedPasteMode && pf.pty != nil {
			if e.PasteStart {
				pf.pty.Write([]byte("\x1b[200~"))
			} else {
				pf.pty.Write([]byte("\x1b[201~"))
			}
			return true
		}
		// Editor view checks paste events internally, so we let it fall through if panels are shown
	}

	// Raw input mode for interactive terminal apps (like far2l inside f4)
	if !pf.showPanels && pf.termView.UseAltScreen {
		isCtrl := (e.ControlKeyState & (vtinput.LeftCtrlPressed | vtinput.RightCtrlPressed)) != 0
		isShift := (e.ControlKeyState & vtinput.ShiftPressed) != 0

		if e.VirtualKeyCode == vtinput.VK_TAB && isCtrl {
			if isShift {
				return false
			}
			isAdvanced := pf.termView.Win32InputMode || pf.termView.KittyFlags != 0
			if !isAdvanced {
				return false
			}
		}

		if e.KeyDown || pf.termView.Win32InputMode || pf.termView.KittyFlags != 0 {
			active := pf.getActivePTY()
			if active != nil {
				if seq := TranslateInput(e, pf.termView.Win32InputMode, pf.termView.KittyFlags, pf.termView.ApplicationCursorKeys); seq != "" {
					active.Write([]byte(seq))
				}
			}
		}
		return true
	}

	if !e.KeyDown {
		return false
	}

	// Selection by mask (+, -, *) logic
	// Intercepted only if command line is empty to allow typing these symbols into commands
	if pf.showPanels && pf.cmdLine.IsEmpty() && !alt && !ctrl {
		isSelectKey := false
		var selectChar rune

		switch e.VirtualKeyCode {
		case vtinput.VK_ADD: isSelectKey = true; selectChar = '+'
		case vtinput.VK_SUBTRACT: isSelectKey = true; selectChar = '-'
		case vtinput.VK_MULTIPLY: isSelectKey = true; selectChar = '*'
		default:
			if e.Char == '+' || e.Char == '-' || e.Char == '*' {
				isSelectKey = true
				selectChar = e.Char
			}
		}

		if isSelectKey {
			fsp := pf.getActivePanel()
			if fsp != nil {
				switch selectChar {
				case '*':
					fsp.InvertSelection()
				case '+':
					vtui.InputBox(Msg("Select.Title"), Msg("Select.Mask"), "*", func(mask string) {
						fsp.ApplyMaskSelection(mask, true)
					})
				case '-':
					vtui.InputBox(Msg("Deselect.Title"), Msg("Select.Mask"), "*", func(mask string) {
						fsp.ApplyMaskSelection(mask, false)
					})
				}
				return true
			}
		}
	}
	// Standard keys for file operations
	switch e.VirtualKeyCode {
	case vtinput.VK_ADD, vtinput.VK_SUBTRACT, vtinput.VK_MULTIPLY:
		// Numpad specific keys
		if pf.cmdLine.IsEmpty() && !alt && !ctrl {
			fsp := pf.getActivePanel()
			if fsp == nil { return true }
			switch e.VirtualKeyCode {
			case vtinput.VK_MULTIPLY: fsp.InvertSelection()
			case vtinput.VK_ADD:
				vtui.InputBox(Msg("Select.Title"), Msg("Select.Mask"), "*", func(mask string) {
					fsp.ApplyMaskSelection(mask, true)
				})
			case vtinput.VK_SUBTRACT:
				vtui.InputBox(Msg("Deselect.Title"), Msg("Select.Mask"), "*", func(mask string) {
					fsp.ApplyMaskSelection(mask, false)
				})
			}
			return true
		}

	case vtinput.VK_F1:
		return vtui.FrameManager.EmitCommand(vtui.CmHelp, nil)
	case vtinput.VK_F3:
		if ctrl { return vtui.FrameManager.EmitCommand(CmSortName, nil) }
		return vtui.FrameManager.EmitCommand(CmView, nil)
	case vtinput.VK_F4:
		if ctrl { return vtui.FrameManager.EmitCommand(CmSortExt, nil) }
		if shift {
			return vtui.FrameManager.EmitCommand(CmNew, nil)
		}
		return vtui.FrameManager.EmitCommand(CmEdit, nil)
	case vtinput.VK_F5:
		if ctrl { return vtui.FrameManager.EmitCommand(CmSortTime, nil) }
		return vtui.FrameManager.EmitCommand(CmCopy, nil)
	case vtinput.VK_F6:
		if ctrl { return vtui.FrameManager.EmitCommand(CmSortSize, nil) }
		return vtui.FrameManager.EmitCommand(CmMove, nil)
	case vtinput.VK_F7:
		if ctrl { return vtui.FrameManager.EmitCommand(CmSortUnsorted, nil) }
		return vtui.FrameManager.EmitCommand(CmMkDir, nil)
	case vtinput.VK_F8:
		return vtui.FrameManager.EmitCommand(CmDelete, nil)
	case vtinput.VK_F10:
		return vtui.FrameManager.EmitCommand(vtui.CmQuit, nil)
	case vtinput.VK_F9:
		pos := 0 // Left
		if pf.activeIdx == 1 {
			pos = 4 // Right
		}
		pf.menuBar.Active = true
		pf.menuBar.ActivateSubMenu(pos)
		return true
	}
	if e.VirtualKeyCode == vtinput.VK_ESCAPE && !pf.cmdLine.IsEmpty() {
		pf.cmdLine.Clear()
		return true
	}

	// Ctrl+Enter inserts selected file name
	if e.VirtualKeyCode == vtinput.VK_RETURN && ctrl {
		name := pf.Active().GetSelectedName()
		if name != "" {
			txt := pf.cmdLine.Edit.GetText()
			// Add space if the line is empty, or if it's not empty and doesn't end with a space.
			if len(txt) == 0 || txt[len(txt)-1] != ' ' {
				pf.cmdLine.InsertString(" ")
			}
			pf.cmdLine.InsertString(name)
		}
		return true
	}


	// Ctrl+O toggles panels visibility
	if e.VirtualKeyCode == vtinput.VK_O && ctrl {
		if !pf.showPanels && pf.isPtyBusy() {
			return true // Prevent switching back while script is working
		}
		pf.showPanels = !pf.showPanels
		if pf.showPanels {
			pf.RefreshAll()
		}
		return true
	}
	// Ctrl+U swaps panels
	if e.VirtualKeyCode == vtinput.VK_U && ctrl {
		return vtui.FrameManager.EmitCommand(CmSwapPanels, nil)
	}

	// Enter handling
	if e.VirtualKeyCode == vtinput.VK_RETURN {
		if !pf.cmdLine.IsEmpty() {
			if pf.isPtyBusy() {
				return true // Prevent sending command if PTY is busy to avoid "garbage"
			}
			cmd := pf.cmdLine.Edit.GetText()
			pf.cmdLine.Edit.AddHistory(cmd)
			
			activePty := pf.getActivePTY()
			if activePty != nil {
				var path string
				if fsp, ok := pf.panels[pf.activeIdx].(*FileSystemPanel); ok { path = fsp.vfs.GetPath() }
				if path != "" {
					vtui.DebugLog("SHELL: Executing %q in %s", cmd, path)
					activePty.Write([]byte(fmt.Sprintf(" cd %q\r", path)))
				}
				activePty.Write([]byte(cmd + "\r"))
			}
			
			pf.cmdLine.Clear()
			pf.showPanels = false
			return true
		} else if !pf.showPanels {
			activePty := pf.getActivePTY()
			if activePty != nil {
				activePty.Write([]byte("\r"))
			}
			return true
		} else {

			// CommandLine is empty, panels are visible.

			// 1. Try passing to panel to handle directory entry.
			handled := pf.Active().ProcessKey(e)

			// 2. If panel didn't handle it, it's a file. Execute or open it.
			if !handled {
				fsp := pf.getActivePanel()
				if fsp == nil { return true }

				name := fsp.GetSelectedName()
				if name != "" && name != ".." {
					path := fsp.vfs.Join(fsp.vfs.GetPath(), name)
					actionExecute(pf, fsp.vfs, fsp.vfs.GetPath(), name, path)
				}
			}
			return true
		}
	}

	// Selection by mask (+, -, *) logic for standard keyboard
	if pf.showPanels && pf.cmdLine.IsEmpty() && !alt && !ctrl {
		if e.Char == '*' || e.Char == '+' || e.Char == '-' {
			fsp := pf.getActivePanel()
			if fsp != nil {
				switch e.Char {
				case '*':
					fsp.InvertSelection()
				case '+':
					vtui.InputBox(Msg("Select.Title"), Msg("Select.Mask"), "*", func(mask string) {
						fsp.ApplyMaskSelection(mask, true)
					})
				case '-':
					vtui.InputBox(Msg("Deselect.Title"), Msg("Select.Mask"), "*", func(mask string) {
						fsp.ApplyMaskSelection(mask, false)
					})
				}
				return true
			}
		}
	}
	// 2. Try global hotkeys handled by PanelsFrame

	// Tab switches panels
	if e.VirtualKeyCode == vtinput.VK_TAB && !ctrl {
		if pf.showPanels {
			pf.activeIdx = 1 - pf.activeIdx
			return true
		}
	}

	// Ctrl+B toggles KeyBar
	if e.VirtualKeyCode == vtinput.VK_B && ctrl {
		pf.showKeyBar = !pf.showKeyBar
		pf.ResizeConsole(pf.lastW, pf.lastH)
		return true
	}

	// 3. Try Active Panel
	if pf.showPanels {
		if pf.Active().ProcessKey(e) {
			return true
		}
	} else {
		if e.VirtualKeyCode == vtinput.VK_UP {
			pf.cmdLine.Edit.HistoryUp()
			return true
		}
		if e.VirtualKeyCode == vtinput.VK_DOWN {
			pf.cmdLine.Edit.HistoryDown()
			return true
		}
	}

	// 4. Fallback: pass to CommandLine (handles text, Backspace, Delete, etc.)
	if pf.cmdLine.ProcessKey(e) {
		pf.cmdLine.SetFocus(true)
		return true
	}

	return false
}
func (pf *PanelsFrame) HandleBroadcast(cmd int, args any) bool {
	if cmd == CmFileChanged {
		pf.RefreshAll()
		return true
	}
	return pf.BaseFrame.HandleBroadcast(cmd, args)
}

func (pf *PanelsFrame) ProcessMouse(e *vtinput.InputEvent) bool {
	mx, my := int(e.MouseX), int(e.MouseY)

	for i, p := range pf.panels {
		if p == nil { continue }
		x1, y1, x2, y2 := p.GetPosition()
		if mx >= x1 && mx <= x2 && my >= y1 && my <= y2 {
			if pf.activeIdx != i && e.ButtonState != 0 {
				pf.activeIdx = i
				vtui.FrameManager.Redraw()
			}

			handled := p.ProcessMouse(e)
			if handled && (e.MouseEventFlags&vtinput.DoubleClick) != 0 && e.ButtonState == vtinput.FromLeft1stButtonPressed {
				pf.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_RETURN})
			}
			return handled || e.ButtonState != 0
		}
	}

	return false
}

func (pf *PanelsFrame) getActivePanel() *FileSystemPanel {
	if fsp, ok := pf.Active().(*FileSystemPanel); ok { return fsp }
	return nil
}

func (pf *PanelsFrame) getInactivePanel() *FileSystemPanel {
	if fsp, ok := pf.Passive().(*FileSystemPanel); ok { return fsp }
	return nil
}

// HandleCommand intercepts global commands (like CmQuit or CmCopy)
// sent by menus or other views.
func (pf *PanelsFrame) HandleCommand(cmd int, args any) bool {
	switch cmd {
	case vtui.CmQuit:
		if pf.pty != nil {
			pf.pty.Close()
		}
		vtui.FrameManager.Shutdown()
		return true

	case vtui.CmHelp:
		pf.ShowHelp()
		return true

	case CmNew:
		actionNewFile(pf)
		return true

	case CmView:
		actionViewFile(pf)
		return true

	case CmEdit:
		actionEditFile(pf)
		return true

	case CmCopy, CmMove:
		actionCopyMove(pf, cmd == CmMove)
		return true

	case CmMkDir:
		actionMkDir(pf)
		return true

	case CmDelete:
		actionDelete(pf)
		return true
	case CmFindFile:
		actionFindFile(pf)
		return true

	case CmBackground:
		if !SupportsBackgrounding() {
			vtui.ShowMessage(" Background ", "Backgrounding is not supported on this OS.", []string{"&Ok"})
			return true
		}
		vtui.FrameManager.Stop() // Clean exit from the main loop
		return true

	case vtui.CmResize: // Used as a hack for 'fork' command from FrameManager
		if s, ok := args.(string); ok && s == "fork" {
			vtui.FrameManager.AddScreen(pf.Clone())
			return true
		}

	case CmLeftMedium:
		if fsp, ok := pf.panels[0].(*FileSystemPanel); ok { fsp.SetViewMode(ViewModeMedium) }
		pf.updateMenuCheckmarks()
		return true
	case CmLeftDetailed:
		if fsp, ok := pf.panels[0].(*FileSystemPanel); ok { fsp.SetViewMode(ViewModeDetailed) }
		pf.updateMenuCheckmarks()
		return true
	case CmRightMedium:
		if fsp, ok := pf.panels[1].(*FileSystemPanel); ok { fsp.SetViewMode(ViewModeMedium) }
		pf.updateMenuCheckmarks()
		return true
	case CmRightDetailed:
		if fsp, ok := pf.panels[1].(*FileSystemPanel); ok { fsp.SetViewMode(ViewModeDetailed) }
		pf.updateMenuCheckmarks()
		return true

	
	case CmLeftSortName:
		if fsp, ok := pf.panels[0].(*FileSystemPanel); ok { fsp.SetSortMode(SortName) }
		pf.updateMenuCheckmarks()
		return true
	case CmLeftSortExt:
		if fsp, ok := pf.panels[0].(*FileSystemPanel); ok { fsp.SetSortMode(SortExt) }
		pf.updateMenuCheckmarks()
		return true
	case CmLeftSortTime:
		if fsp, ok := pf.panels[0].(*FileSystemPanel); ok { fsp.SetSortMode(SortTime) }
		pf.updateMenuCheckmarks()
		return true
	case CmLeftSortSize:
		if fsp, ok := pf.panels[0].(*FileSystemPanel); ok { fsp.SetSortMode(SortSize) }
		pf.updateMenuCheckmarks()
		return true
	case CmLeftSortUnsorted:
		if fsp, ok := pf.panels[0].(*FileSystemPanel); ok { fsp.SetSortMode(SortUnsorted) }
		pf.updateMenuCheckmarks()
		return true
	case CmRightSortName:
		if fsp, ok := pf.panels[1].(*FileSystemPanel); ok { fsp.SetSortMode(SortName) }
		pf.updateMenuCheckmarks()
		return true
	case CmRightSortExt:
		if fsp, ok := pf.panels[1].(*FileSystemPanel); ok { fsp.SetSortMode(SortExt) }
		pf.updateMenuCheckmarks()
		return true
	case CmRightSortTime:
		if fsp, ok := pf.panels[1].(*FileSystemPanel); ok { fsp.SetSortMode(SortTime) }
		pf.updateMenuCheckmarks()
		return true
	case CmRightSortSize:
		if fsp, ok := pf.panels[1].(*FileSystemPanel); ok { fsp.SetSortMode(SortSize) }
		pf.updateMenuCheckmarks()
		return true
	case CmRightSortUnsorted:
		if fsp, ok := pf.panels[1].(*FileSystemPanel); ok { fsp.SetSortMode(SortUnsorted) }
		pf.updateMenuCheckmarks()
		return true
	case CmSwapPanels:
		pf.panels[0], pf.panels[1] = pf.panels[1], pf.panels[0]
		pf.activeIdx = 1 - pf.activeIdx
		pf.ResizeConsole(pf.lastW, pf.lastH)
		return true
	case CmSortName:
		if fsp := pf.getActivePanel(); fsp != nil { fsp.SetSortMode(SortName) }
		pf.updateMenuCheckmarks()
		return true
	case CmSortExt:
		if fsp := pf.getActivePanel(); fsp != nil { fsp.SetSortMode(SortExt) }
		pf.updateMenuCheckmarks()
		return true
	case CmSortTime:
		if fsp := pf.getActivePanel(); fsp != nil { fsp.SetSortMode(SortTime) }
		pf.updateMenuCheckmarks()
		return true
	case CmSortSize:
		if fsp := pf.getActivePanel(); fsp != nil { fsp.SetSortMode(SortSize) }
		pf.updateMenuCheckmarks()
		return true
	case CmSortUnsorted:
		if fsp := pf.getActivePanel(); fsp != nil { fsp.SetSortMode(SortUnsorted) }
		pf.updateMenuCheckmarks()
		return true
	}
	return false
}


func (pf *PanelsFrame) GetKeyLabels() *vtui.KeySet {
	return &vtui.KeySet{
		Normal: vtui.KeyBarLabels{
			Msg("KeyBar.F1"), Msg("KeyBar.F2"), Msg("KeyBar.F3"), Msg("KeyBar.F4"),
			Msg("KeyBar.F5"), Msg("KeyBar.F6"), Msg("KeyBar.F7"), Msg("KeyBar.F8"),
			Msg("KeyBar.F9"), Msg("KeyBar.F10"), Msg("KeyBar.F11"), Msg("KeyBar.F12"),
		},
		Alt: vtui.KeyBarLabels{
			Msg("KeyBar.AltF1"), Msg("KeyBar.AltF2"), "", "",
			"", "", "", "", "", "", "", "",
		},
		Ctrl: vtui.KeyBarLabels{
			"", "", Msg("KeyBar.CtrlF3"), Msg("KeyBar.CtrlF4"), Msg("KeyBar.CtrlF5"), Msg("KeyBar.CtrlF6"), Msg("KeyBar.CtrlF7"), "", "", "", "Fork", "Close",
		},
	}
}

func (pf *PanelsFrame) GetType() vtui.FrameType { return vtui.TypeUser + 1 }

func (pf *PanelsFrame) SetExitCode(code int)     { pf.Done = true; pf.ExitCode = code }
func (pf *PanelsFrame) showDummyOpDialog() {
	msg := Msg("Op.DummyText")
	lines := vtui.WrapText(msg, 50-4)

	dlg := vtui.NewCenteredDialog(50, 10+len(lines)-1, Msg("Op.DummyTitle"))
	vbox := vtui.NewVBoxLayout(dlg.X1+2, dlg.Y1+2, 50-4, (10+len(lines)-1)-4)

	for _, l := range lines {
		t := vtui.NewText(0, 0, l, vtui.Palette[vtui.ColDialogText])
		dlg.AddItem(t)
		vbox.Add(t, vtui.Margins{}, vtui.AlignLeft)
	}

	chkClone := vtui.NewCheckbox(0, 0, Msg("Op.ClonePanels"), false)
	dlg.AddItem(chkClone)

	btnStart := vtui.NewButton(0, 0, "&Start")
	btnCancel := vtui.NewButton(0, 0, "&Cancel")
	dlg.AddItem(btnStart)
	dlg.AddItem(btnCancel)

	vbox.Add(chkClone, vtui.Margins{Top: 1}, vtui.AlignLeft)

	hbox := vtui.NewHBoxLayout(0, 0, 50-4, 1)
	hbox.HorizontalAlign = vtui.AlignCenter
	hbox.Spacing = 2
	hbox.Add(btnStart, vtui.Margins{}, vtui.AlignTop)
	hbox.Add(btnCancel, vtui.Margins{}, vtui.AlignTop)

	vbox.Add(hbox, vtui.Margins{Top: 1}, vtui.AlignFill)
	vbox.Apply()

	// Set default focus to Start button
	dlg.SetFocusedItem(btnStart)

	btnCancel.OnClick = func() { dlg.Close() }
	btnStart.OnClick = func() {
		mode := chkClone.State == 1
		dlg.Close()
		go pf.ExecuteDummyOp(mode)
	}

	vtui.FrameManager.Push(dlg)
}

// RunProgressTask encapsulates the boilerplate for creating a progress dialog,
// running a background task with cancellation, and optionally forking the workspace.
func (pf *PanelsFrame) RunProgressTask(title, startMsg string, forked bool, worker func(ctx context.Context, update func(msg string, percent int)) error, onComplete func(err error)) {
	dlg := vtui.NewCenteredDialog(50, 8, title)
	dlg.AttentionSuppressed = true

	lbl := vtui.NewText(0, 0, startMsg, vtui.Palette[vtui.ColDialogText])
	dlg.AddItem(lbl)

	btnCancel := vtui.NewButton(0, 0, "&Cancel")
	dlg.AddItem(btnCancel)

	vbox := vtui.NewVBoxLayout(dlg.X1+2, dlg.Y1+2, 50-4, 8-4)
	vbox.Add(lbl, vtui.Margins{}, vtui.AlignCenter)
	vbox.Add(btnCancel, vtui.Margins{Top: 1}, vtui.AlignCenter)
	vbox.Apply()

	var taskCtx *vtui.TaskContext
	btnCancel.OnClick = func() {
		if taskCtx != nil {
			taskCtx.Cancel()
		}
		dlg.Close()
	}

	vtui.FrameManager.PostTask(func() {
		if forked && pf != nil {
			clone := pf.Clone()
			vtui.FrameManager.AddScreen(clone)
			vtui.FrameManager.Push(dlg)
		} else {
			vtui.FrameManager.AddScreenHeadless(dlg)
		}
	})

	taskCtx = vtui.RunAsync(func(ctx *vtui.TaskContext) {
		update := func(msg string, percent int) {
			ctx.RunOnUI(func() {
				if msg != "" {
					safeMsg := runewidth.Truncate(msg, 46, "...")
					lbl.SetText(safeMsg)
				}
				if percent >= 0 { dlg.SetProgress(percent) }
				vtui.FrameManager.Redraw()
			})
		}
		err := worker(ctx.Context, update)
		ctx.RunOnUI(func() {
			dlg.Close()
			if onComplete != nil { onComplete(err) }
		})
	})
}
func (pf *PanelsFrame) ExecuteDummyOp(forked bool) {
	pf.RunProgressTask(" Processing... ", "Initializing...", forked, func(ctx context.Context, update func(msg string, percent int)) error {
		totalSteps := 300 // 5 minutes = 300 seconds
		for i := 1; i <= totalSteps; i++ {
			if ctx.Err() != nil { return ctx.Err() }
			time.Sleep(1 * time.Second)
			update(fmt.Sprintf("Step %d of %d...", i, totalSteps), (i*100)/totalSteps)
		}
		return nil
	}, func(err error) {
		if err == nil {
			// Find the active screen to attach the completion message
			top := vtui.FrameManager.GetTopFrame()
			vtui.ShowMessageOn(top, " Done ", "Dummy operation finished!", []string{"&Ok"})
		}
	})
}
func (pf *PanelsFrame) RefreshAll() {
	if pf == nil {
		return
	}
	for _, p := range pf.panels {
		if fsp, ok := p.(*FileSystemPanel); ok {
			fsp.ReadDirectory()
		}
	}
}
func (pf *PanelsFrame) Message(title, msg string, buttons []string) int {
	resChan := make(chan int, 1)
	vtui.FrameManager.PostTask(func() {
		dlg := vtui.ShowMessage(title, msg, buttons)
		dlg.OnResult = func(code int) { resChan <- code }
	})
	return <-resChan
}

func (pf *PanelsFrame) InputBox(title, prompt, history string, callback func(string)) {
	vtui.FrameManager.PostTask(func() {
		vtui.InputBox(title, prompt, history, callback)
	})
}

func (pf *PanelsFrame) Menu(title string, items []string, callback func(int)) {
	vtui.FrameManager.PostTask(func() {
		menu := vtui.NewVMenu(title)
		for _, itm := range items {
			menu.AddItem(vtui.MenuItem{Text: itm})
		}
		menu.OnAction = func(idx int) {
			menu.Close()
			if callback != nil { callback(idx) }
		}
		vtui.FrameManager.Push(menu)
	})
}
func (pf *PanelsFrame) getActivePTYUnsafe() PtyBackend {
	if pf.remotePtys == nil {
		pf.remotePtys = make(map[vfs.VFS]PtyBackend)
	}

	
	
	
	var activeVfs vfs.VFS
	if fsp := pf.getActivePanel(); fsp != nil {
		activeVfs = fsp.vfs
	}

	if pp, ok := activeVfs.(vfs.PtyProvider); ok {
		if pty, exists := pf.remotePtys[activeVfs]; exists {
			return pty
		}

		res, err := pp.OpenPty(pf.termView.Width, pf.termView.Height)
		if err == nil {
			pty := res.(PtyBackend)
			vtui.DebugLog("Created new remote PTY background session for VFS")
			pf.remotePtys[activeVfs] = pty

			go func() {
				buf := make([]byte, 4096)
				for {
					n, readErr := pty.Read(buf)
					if readErr != nil {
						break
					}

					pf.ptyMutex.Lock()
					if pf.getActivePTYUnsafe() == pty {
						pf.parser.Process(buf[:n])
						vtui.FrameManager.PostTask(vtui.FrameManager.Redraw)
					}
					pf.ptyMutex.Unlock()
				}
				pty.Close()
				pf.ptyMutex.Lock()
				delete(pf.remotePtys, activeVfs)
				pf.ptyMutex.Unlock()
			}()
			return pty
		}
	}
	return pf.pty
}

func (pf *PanelsFrame) getActivePTY() PtyBackend {
	pf.ptyMutex.Lock()
	defer pf.ptyMutex.Unlock()
	return pf.getActivePTYUnsafe()
}

func (pf *PanelsFrame) GetTitle() string {
	if !pf.showPanels {
		title := pf.termView.Title
		if title == "f4:busy" || title == "f4:done" {
			return "Terminal"
		}
		if title != "" {
			return title
		}
		return "Terminal"
	}

	path := ""
	if fsp, ok := pf.Active().(*FileSystemPanel); ok {
		path = fsp.vfs.GetPath()
	}

	if path != "" {
		return "Panels: " + path
	}
	return "Panels"
}

func (pf *PanelsFrame) Clone() *PanelsFrame {
	clone := NewPanelsFrame()
	if pf.lastW > 0 && pf.lastH > 0 {
		clone.ResizeConsole(pf.lastW, pf.lastH)
	}

	for i, p := range pf.panels {
		if fsp, ok := p.(*FileSystemPanel); ok {
			cloneFsp := clone.panels[i].(*FileSystemPanel)
			cloneFsp.vfs.SetPath(fsp.vfs.GetPath())
			cloneFsp.SetViewMode(fsp.viewMode)
			cloneFsp.cursorIdx = fsp.cursorIdx
			cloneFsp.sortMode = fsp.sortMode
			cloneFsp.sortReverse = fsp.sortReverse

			// Copy entries immediately so the visual state is valid before async reload
			cloneFsp.entries = make([]*fileEntry, len(fsp.entries))
			for j, e := range fsp.entries {
				cloneFsp.entries[j] = &fileEntry{
					VFSItem:  e.VFSItem,
					Selected: e.Selected,
				}
			}
			cloneFsp.Refresh() // Populate table rows from copied entries

			cloneFsp.readDirectoryEx(true) // ВАЖНО: не удалять скопированные записи при первом чтении
			cloneFsp.table.SelectPos = fsp.table.SelectPos
			cloneFsp.table.SelectCol = fsp.table.SelectCol
			cloneFsp.table.TopPos = fsp.table.TopPos
		}
	}

	clone.activeIdx = pf.activeIdx
	clone.showKeyBar = pf.showKeyBar
	clone.showPanels = pf.showPanels

	if pf.termView != nil && clone.termView != nil {
		clone.termView.CloneStateFrom(pf.termView)
	}
	clone.updateMenuCheckmarks()
	return clone
}

func (pf *PanelsFrame) showPluginMenu() {
	if len(PluginMenuItems) == 0 {
		vtui.ShowMessage(" Plugins ", "No plugins registered for F11 menu.", []string{"&Ok"})
		return
	}
	var labels []string
	for _, itm := range PluginMenuItems {
		labels = append(labels, itm.Label)
	}
	pf.Menu(" Plugins ", labels, func(idx int) {
		if idx >= 0 && idx < len(PluginMenuItems) {
			PluginMenuItems[idx].Handler(pf)
		}
	})
}

func (pf *PanelsFrame) showDriveMenu(panelIdx int) {
	menu := vtui.NewVMenu(" Drive ")
	for _, drv := range DriveRegistry {
		menu.AddItem(vtui.MenuItem{Text: drv.Name})
	}

	w, h := 26, menu.GetItemCount()+2
	x := (pf.lastW - w) / 2
	y := (pf.lastH - h) / 2
	if panelIdx == 0 {
		x = pf.lastW/4 - w/2
	} else {
		x = pf.lastW*3/4 - w/2
	}
	if x < 0 {
		x = 0
	}
	menu.SetPosition(x, y, x+w-1, y+h-1)

	menu.OnAction = func(idx int) {
		menu.Close()
		if fsp, ok := pf.panels[panelIdx].(*FileSystemPanel); ok {
			if idx >= 0 && idx < len(DriveRegistry) {
				newVFS := DriveRegistry[idx].Factory()
				if newVFS != nil {
					if fsp.vfs != nil {
						fsp.vfs.Close()
						pf.ptyMutex.Lock()
						if pty, ok := pf.remotePtys[fsp.vfs]; ok {
							pty.Close()
							delete(pf.remotePtys, fsp.vfs)
						}
						pf.ptyMutex.Unlock()
					}
					fsp.vfs = newVFS
					fsp.ReadDirectory()
					pf.RefreshAll()
				}
			}
		}
	}
	vtui.FrameManager.Push(menu)
}
