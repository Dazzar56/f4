package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"time"
	"github.com/unxed/f4/vfs"
	"github.com/unxed/vtui/piecetable"
	"os/user"
	"strings"

	"github.com/unxed/vtinput"
	"github.com/unxed/vtui"
)

// PanelsFrame is the main frame of the f4 manager, containing left and right panels.
type PanelsFrame struct {
	vtui.BaseFrame
	panels    [2]Panel
	activeIdx int // 0 for left, 1 for right

	menuBar   *vtui.MenuBar
	cmdLine   *CommandLine
	keyBar    *vtui.KeyBar

	showKeyBar bool
	showPanels bool
	lastW      int
	lastH      int

	// Integrated Terminal
	pty      PtyBackend
	termView *TerminalView
	parser   *AnsiParser

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
	pf.menuBar.Items = []vtui.MenuBarItem{
		// Using Command routing (TV style) instead of hardcoded indices
		{Label: "&" + Msg("Menu.Left"), SubItems: []vtui.MenuItem{
			{Text: "&" + Msg("Menu.Left.Medium"), Command: vtui.CmLeftMedium},
			{Text: "&" + Msg("Menu.Left.Detailed"), Command: vtui.CmLeftDetailed},
			{Separator: true},
			{Text: "Bac&kground", Command: vtui.CmBackground},
			{Text: Msg("Menu.Exit"), Command: vtui.CmQuit},
		}},
		{Label: "&" + Msg("Menu.Files"), SubItems: []vtui.MenuItem{
			{Text: "&" + Msg("Menu.Files.View"), Shortcut: "F3", Command: vtui.CmView},
			{Text: "&" + Msg("Menu.Files.Edit"), Shortcut: "F4", Command: vtui.CmEdit},
			{Text: "&" + Msg("Menu.Files.Copy"), Shortcut: "F5", Command: vtui.CmCopy},
			{Text: "&" + Msg("Menu.Files.RenMov"), Shortcut: "F6", Command: vtui.CmMove},
			{Text: "&" + Msg("Menu.Files.MkDir"), Shortcut: "F7", Command: vtui.CmMkDir},
			{Text: "&" + Msg("Menu.Files.Delete"), Shortcut: "F8", Command: vtui.CmDelete},
		}},
		{Label: "&" + Msg("Menu.Commands"), SubItems: []vtui.MenuItem{{Text: "Placeholder"}}},
		{Label: "&" + Msg("Menu.Options"), SubItems: []vtui.MenuItem{{Text: "Placeholder"}}},
		{Label: "&" + Msg("Menu.Right"), SubItems: []vtui.MenuItem{
			{Text: "&" + Msg("Menu.Left.Medium"), Command: vtui.CmRightMedium},
			{Text: "&" + Msg("Menu.Left.Detailed"), Command: vtui.CmRightDetailed},
		}},
	}
	// We no longer need pf.menuBar.OnCommand for routing!
	pf.cmdLine = NewCommandLine(Msg("Panels.Prompt"))
	pf.keyBar = vtui.NewKeyBar()

	pf.termView = NewTerminalView(80, 24)
	// Parser will be fully initialized in initPTY once pty is ready
	pf.initPTY()


	return pf
}

func getMenuText(current, target ViewMode, label string) string {
	if current == target {
		return "√" + label
	}
	return " " + label
}

func (pf *PanelsFrame) updateMenuCheckmarks() {
	if pf.panels[0] == nil || pf.panels[1] == nil { return }

	lMode, rMode := ViewModeMedium, ViewModeMedium
	if fsp, ok := pf.panels[0].(*FileSystemPanel); ok { lMode = fsp.viewMode }
	if fsp, ok := pf.panels[1].(*FileSystemPanel); ok { rMode = fsp.viewMode }

	pf.menuBar.Items[0].SubItems[0].Text = getMenuText(lMode, ViewModeMedium, "&"+Msg("Menu.Left.Medium"))
	pf.menuBar.Items[0].SubItems[1].Text = getMenuText(lMode, ViewModeDetailed, "&"+Msg("Menu.Left.Detailed"))

	pf.menuBar.Items[4].SubItems[0].Text = getMenuText(rMode, ViewModeMedium, "&"+Msg("Menu.Left.Medium"))
	pf.menuBar.Items[4].SubItems[1].Text = getMenuText(rMode, ViewModeDetailed, "&"+Msg("Menu.Left.Detailed"))
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

	// Read loop
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := pf.pty.Read(buf)
			if err != nil {
				return
			}
			pf.parser.Process(buf[:n])
			vtui.FrameManager.Redraw()
		}
	}()
}

func (pf *PanelsFrame) openEditor(v vfs.VFS, path string) {
	var f vfs.ReadAtCloser
	var pt *piecetable.PieceTable
	if v != nil {
		f, _ = v.Open(path)
	}
	if f != nil {
		pt = piecetable.NewWithBuffer(NewFileBuffer(f))
	} else {
		pt = piecetable.New(nil)
	}

	editor := NewEditorView(pt, v, path)
	editor.SetFile(f)
	editor.ResizeConsole(pf.lastW, pf.lastH)
	// Editor opens in a NEW workspace
	vtui.FrameManager.AddScreen(editor)
}

func (pf *PanelsFrame) openViewer(v vfs.VFS, path string) {
	viewer, err := NewViewerView(v, path)
	if err != nil {
		vtui.DebugLog("PANELS: Failed to open viewer for %s: %v", path, err)
		return
	}
	viewer.ResizeConsole(pf.lastW, pf.lastH)
	// Viewer opens in a NEW workspace
	vtui.FrameManager.AddScreen(viewer)
}


func (pf *PanelsFrame) executeFile(v vfs.VFS, dir, name, path string) {
	if _, isLocal := v.(*vfs.OSVFS); !isLocal {
		vtui.ShowMessage(" Error ", "Cannot execute files on a remote file system.", []string{"&Ok"})
		return
	}

	if vfs.IsTerminalRunnable(v, path) {
		if pf.pty != nil {
			// Sync PTY to the file's directory
			pf.pty.Write([]byte(fmt.Sprintf(" cd %q\r", dir)))
			// Execute. Use ./ on Unix for current-dir security.
			cmd := name
			if runtime.GOOS != "windows" {
				cmd = "./" + name
			}
			pf.pty.Write([]byte(cmd + "\r"))
		}
		pf.showPanels = false
	} else {
		// System Open
		var cmd *exec.Cmd
		switch runtime.GOOS {
		case "linux":
			cmd = exec.Command("xdg-open", path)
		case "windows":
			cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", path)
		case "darwin":
			cmd = exec.Command("open", path)
		}
		if cmd != nil {
			_ = cmd.Start()
		}
	}
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
		pf.pty.SetSize(w, termH)
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

func (pf *PanelsFrame) Show(scr *vtui.ScreenBuf) {
	// 0. Dynamic Layout Adjustment
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
	if (!pf.showPanels && pf.termView.UseAltScreen) || topType == vtui.TypeUser+2 {
		pf.cmdLine.SetVisible(false)
	} else {
		pf.cmdLine.SetVisible(true)
		cmdLineY := pf.lastH - 1
		if pf.showKeyBar {
			cmdLineY = pf.lastH - 2
		}
		if pf.showPanels {
			pf.cmdLine.SetRichPrompt(pf.buildPrompt())
			pf.cmdLine.SetPosition(0, cmdLineY, pf.lastW-1, cmdLineY)
		} else {
			pf.cmdLine.SetRichPrompt(nil)
			pf.cmdLine.SetPrompt("")
			tx, ty := pf.termView.CursorX, pf.termView.CursorY
			_, termY1, _, _ := pf.termView.GetPosition()
			pf.cmdLine.SetPosition(tx, termY1+ty, pf.lastW-1, termY1+ty)
		}
		if pf.cmdLine.IsVisible() {
			// CommandLine now uses ThemePalette[0] for background via OverlayMode,
			// which matches the terminal background perfectly.
			pf.cmdLine.Show(scr)
		}
	}

	// KeyBar is at the bottom. It should only be hidden if a child process
	// in the terminal uses the alternate screen buffer (e.g. vim, less).
	isTop := vtui.FrameManager.GetTopFrameType() == vtui.TypeUser+1
	if isTop { // Only the top-most user frame controls the keybar
		if pf.showKeyBar && !pf.termView.UseAltScreen {
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
	// Arkanoid easter egg :)
	if e.VirtualKeyCode == vtinput.VK_A && ctrl && alt && !shift && e.KeyDown {
		vtui.FrameManager.Push(NewArkanoidFrame())
		return true
	}

	// Alt+F5: Dummy Long Operation for debugging
	if e.VirtualKeyCode == vtinput.VK_F5 && alt && !ctrl && e.KeyDown {
		pf.showDummyOpDialog()
		return true
	}

	if e.Type == vtinput.FocusEventType {
		if e.SetFocus {
			pf.RefreshAll()
		}
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

		// Only forward KeyUp events if the guest app explicitly requested Win32 Input Mode.
		// Legacy apps (like mc) would interpret forwarded KeyUp escape sequences as new keypresses.
		if e.KeyDown || pf.termView.Win32InputMode || pf.termView.KittyFlags != 0 {
			if pf.pty != nil {
				if seq := TranslateInput(e, pf.termView.Win32InputMode, pf.termView.KittyFlags, pf.termView.ApplicationCursorKeys); seq != "" {
					pf.pty.Write([]byte(seq))
				}
			}
		}
		return true
	}

	if !e.KeyDown {
		return false
	}

	// Standard keys for file operations
	switch e.VirtualKeyCode {
	case vtinput.VK_F1:
		return vtui.FrameManager.EmitCommand(vtui.CmHelp, nil)
	case vtinput.VK_F3:
		return vtui.FrameManager.EmitCommand(vtui.CmView, nil)
	case vtinput.VK_F4:
		if shift {
			return vtui.FrameManager.EmitCommand(vtui.CmNew, nil)
		}
		return vtui.FrameManager.EmitCommand(vtui.CmEdit, nil)
	case vtinput.VK_F5:
		return vtui.FrameManager.EmitCommand(vtui.CmCopy, nil)
	case vtinput.VK_F6:
		return vtui.FrameManager.EmitCommand(vtui.CmMove, nil)
	case vtinput.VK_F7:
		return vtui.FrameManager.EmitCommand(vtui.CmMkDir, nil)
	case vtinput.VK_F8:
		return vtui.FrameManager.EmitCommand(vtui.CmDelete, nil)
	case vtinput.VK_F10:
		return vtui.FrameManager.EmitCommand(vtui.CmQuit, nil)
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
		pf.showPanels = !pf.showPanels
		return true
	}

	// Enter handling
	if e.VirtualKeyCode == vtinput.VK_RETURN {
		if !pf.cmdLine.IsEmpty() {
			cmd := pf.cmdLine.Edit.GetText()
			pf.cmdLine.AddHistory(cmd)
			pf.cmdLine.historyPos = -1 // Reset history browsing on new command
			if pf.pty != nil {
				var path string
				if fsp, ok := pf.panels[pf.activeIdx].(*FileSystemPanel); ok { path = fsp.vfs.GetPath() }
				if path != "" {
					pf.pty.Write([]byte(fmt.Sprintf(" cd %q\r", path)))
				}
				pf.pty.Write([]byte(cmd + "\r"))
			}
			pf.cmdLine.Clear()
			pf.showPanels = false
			return true
		} else if !pf.showPanels {
			if pf.pty != nil {
				pf.pty.Write([]byte("\r"))
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
					pf.executeFile(fsp.vfs, fsp.vfs.GetPath(), name, path)
				}
			}
			return true
		}
	}

	// 2. Try global hotkeys handled by PanelsFrame

	// Handle command history when panels are hidden
	if !pf.showPanels {
		switch e.VirtualKeyCode {
		case vtinput.VK_UP:
			pf.cmdLine.HistoryUp()
			return true
		case vtinput.VK_DOWN:
			pf.cmdLine.HistoryDown()
			return true
		}
	}
	// Tab switches panels
	if e.VirtualKeyCode == vtinput.VK_TAB && !ctrl {
		pf.activeIdx = 1 - pf.activeIdx
		return true
	}

	// Ctrl+B toggles KeyBar
	if e.VirtualKeyCode == vtinput.VK_B && ctrl {
		pf.showKeyBar = !pf.showKeyBar
		pf.ResizeConsole(pf.lastW, pf.lastH)
		return true
	}

	// 3. Try Active Panel
	panelHandled := pf.Active().ProcessKey(e)

	if panelHandled {
		return true
	}

	// 4. Fallback: pass to CommandLine (handles text, Backspace, Delete, etc.)
	if pf.cmdLine.ProcessKey(e) {
		pf.cmdLine.SetFocus(true)
		return true
	}

	return false
}
func (pf *PanelsFrame) HandleBroadcast(cmd int, args any) bool {
	if cmd == vtui.CmFileChanged {
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
		vtui.FrameManager.Shutdown()
		return true

	case vtui.CmHelp:
		pf.ShowHelp()
		return true

	case vtui.CmNew:
		actionNewFile(pf)
		return true

	case vtui.CmView:
		actionViewFile(pf)
		return true

	case vtui.CmEdit:
		actionEditFile(pf)
		return true

	case vtui.CmCopy, vtui.CmMove:
		actionCopyMove(pf, cmd == vtui.CmMove)
		return true

	case vtui.CmMkDir:
		actionMkDir(pf)
		return true

	case vtui.CmDelete:
		actionDelete(pf)
		return true

	case vtui.CmBackground:
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

	case vtui.CmLeftMedium:
		if fsp, ok := pf.panels[0].(*FileSystemPanel); ok { fsp.SetViewMode(ViewModeMedium) }
		pf.updateMenuCheckmarks()
		return true
	case vtui.CmLeftDetailed:
		if fsp, ok := pf.panels[0].(*FileSystemPanel); ok { fsp.SetViewMode(ViewModeDetailed) }
		pf.updateMenuCheckmarks()
		return true
	case vtui.CmRightMedium:
		if fsp, ok := pf.panels[1].(*FileSystemPanel); ok { fsp.SetViewMode(ViewModeMedium) }
		pf.updateMenuCheckmarks()
		return true
	case vtui.CmRightDetailed:
		if fsp, ok := pf.panels[1].(*FileSystemPanel); ok { fsp.SetViewMode(ViewModeDetailed) }
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
			"", "", "", "", "", "", "", "", "", "", "Fork", "Close",
		},
	}
}

func (pf *PanelsFrame) GetType() vtui.FrameType { return vtui.TypeUser + 1 }

func (pf *PanelsFrame) SetExitCode(code int)     { pf.Done = true; pf.ExitCode = code }
func (pf *PanelsFrame) showDummyOpDialog() {
	dlg := vtui.NewDialog(0, 0, 50, 10, Msg("Op.DummyTitle"))
	dlg.Center(vtui.FrameManager.GetScreenSize(), 25)

	dlg.AddItem(vtui.NewText(dlg.X1+2, dlg.Y1+2, Msg("Op.DummyText"), vtui.Palette[vtui.ColDialogText]))

	chkClone := vtui.NewCheckbox(dlg.X1+2, dlg.Y1+5, Msg("Op.ClonePanels"), false)
	dlg.AddItem(chkClone)

	btnStart := vtui.NewButton(dlg.X1+10, dlg.Y1+7, "&Start")
	btnStart.OnClick = func() {
		mode := chkClone.State == 1
		dlg.Close()
		go pf.ExecuteDummyOp(mode)
	}
	dlg.AddItem(btnStart)

	btnCancel := vtui.NewButton(dlg.X1+25, dlg.Y1+7, "&Cancel")
	btnCancel.OnClick = func() { dlg.Close() }
	dlg.AddItem(btnCancel)

	vtui.FrameManager.Push(dlg)
}

// RunProgressTask encapsulates the boilerplate for creating a progress dialog,
// running a background task with cancellation, and optionally forking the workspace.
func (pf *PanelsFrame) RunProgressTask(title, startMsg string, forked bool, worker func(ctx *vtui.TaskContext, update func(msg string, percent int)) error, onComplete func(err error)) {
	dlg := vtui.NewDialog(0, 0, 50, 8, title)
	dlg.AttentionSuppressed = true
	dlg.Center(vtui.FrameManager.GetScreenSize(), 25)

	lbl := vtui.NewText(dlg.X1+2, dlg.Y1+2, startMsg, vtui.Palette[vtui.ColDialogText])
	dlg.AddItem(lbl)

	btnCancel := vtui.NewButton(dlg.X1+20, dlg.Y1+5, "&Cancel")
	var taskCtx *vtui.TaskContext
	btnCancel.OnClick = func() {
		if taskCtx != nil { taskCtx.Cancel() }
		dlg.Close()
	}
	dlg.AddItem(btnCancel)

	vtui.FrameManager.PostTask(func() {
		if forked {
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
					// Pad string to overwrite old text
					padded := msg
					for len([]rune(padded)) < 46 { padded += " " }
					lbl.SetText(padded)
				}
				if percent >= 0 { dlg.SetProgress(percent) }
				vtui.FrameManager.Redraw()
			})
		}
		err := worker(ctx, update)
		ctx.RunOnUI(func() {
			dlg.Close()
			if onComplete != nil { onComplete(err) }
		})
	})
}
func (pf *PanelsFrame) ExecuteDummyOp(forked bool) {
	pf.RunProgressTask(" Processing... ", "Initializing...", forked, func(ctx *vtui.TaskContext, update func(msg string, percent int)) error {
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
	for _, p := range pf.panels {
		if fsp, ok := p.(*FileSystemPanel); ok { fsp.ReadDirectory() }
	}
}

func (pf *PanelsFrame) GetTitle() string {
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
			cloneFsp.ReadDirectory()
			if len(cloneFsp.entries) == len(fsp.entries) {
				for j := range cloneFsp.entries {
					cloneFsp.entries[j].Selected = fsp.entries[j].Selected
				}
			}
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
