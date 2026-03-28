package main

import (
	"fmt"
	"os"
	"path/filepath"
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
	left      Panel
	right     Panel
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

func NewPanelsFrame() *PanelsFrame {
	pf := &PanelsFrame{activeIdx: 1}
	pf.SetHelp("Panels")
	pf.showKeyBar = true
	pf.showPanels = true

	pf.menuBar = vtui.NewMenuBar(nil)
	pf.menuBar.Items = []vtui.MenuBarItem{
		// Using Command routing (TV style) instead of hardcoded indices
		{Label: "&" + Msg("Menu.Left"), SubItems: []vtui.MenuItem{
			{Text: "Bac&kground", Command: vtui.CmBackground},
			{Text: Msg("Menu.Exit"), Command: vtui.CmQuit},
		}},
		{Label: "&" + Msg("Menu.Files"), SubItems: []vtui.MenuItem{
			{Text: "&" + Msg("KeyBar.F3"), Command: vtui.CmView},
			{Text: "&" + Msg("KeyBar.F4"), Command: vtui.CmEdit},
			{Text: "&" + Msg("KeyBar.F5"), Command: vtui.CmCopy},
			{Text: "&" + Msg("KeyBar.F6"), Command: vtui.CmMove},
			{Text: "&" + Msg("KeyBar.F7"), Command: vtui.CmMkDir},
			{Text: "&" + Msg("KeyBar.F8"), Command: vtui.CmDelete},
		}},
		{Label: "&" + Msg("Menu.Commands"), SubItems: []vtui.MenuItem{{Text: "Placeholder"}}},
		{Label: "&" + Msg("Menu.Options"), SubItems: []vtui.MenuItem{{Text: "Placeholder"}}},
		{Label: "&" + Msg("Menu.Right"), SubItems: []vtui.MenuItem{{Text: "Placeholder"}}},
	}
	// We no longer need pf.menuBar.OnCommand for routing!
	pf.cmdLine = NewCommandLine(Msg("Panels.Prompt"))
	pf.keyBar = vtui.NewKeyBar()

	pf.termView = NewTerminalView(80, 24)
	// Parser will be fully initialized in initPTY once pty is ready
	pf.initPTY()

	return pf
}

func (pf *PanelsFrame) buildPrompt() []vtui.CharInfo {
	var path string
	if pf.activeIdx == 0 {
		if fsp, ok := pf.left.(*FileSystemPanel); ok { path = fsp.vfs.GetPath() }
	} else {
		if fsp, ok := pf.right.(*FileSystemPanel); ok { path = fsp.vfs.GetPath() }
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

func (pf *PanelsFrame) openEditor(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		data = []byte("")
	}
	pt := piecetable.New(data)
	editor := NewEditorView(pt, path)
	editor.ResizeConsole(pf.lastW, pf.lastH)
	// Editor opens in a NEW workspace
	vtui.FrameManager.AddScreen(editor)
}

func (pf *PanelsFrame) openViewer(path string) {
	viewer, err := NewViewerView(path)
	if err != nil {
		vtui.DebugLog("PANELS: Failed to open viewer for %s: %v", path, err)
		return
	}
	viewer.ResizeConsole(pf.lastW, pf.lastH)
	// Viewer opens in a NEW workspace
	vtui.FrameManager.AddScreen(viewer)
}

func (pf *PanelsFrame) ResizeConsole(w, h int) {
	pf.lastW, pf.lastH = w, h
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

	if pf.left == nil {
		pf.left = NewFileSystemPanel(0, contentY1, leftW, panelH, vfs.NewOSVFS("."))
		pf.right = NewFileSystemPanel(leftW, contentY1, rightW, panelH, vfs.NewOSVFS("."))
	} else {
		pf.left.SetPosition(0, contentY1, leftW-1, panelY2)
		pf.right.SetPosition(leftW, contentY1, w-1, panelY2)

		// Special methods for column adaptation (if it's FileSystemPanel)
		if fsp, ok := pf.left.(*FileSystemPanel); ok { fsp.Resize(leftW, panelH) }
		if fsp, ok := pf.right.(*FileSystemPanel); ok { fsp.Resize(rightW, panelH) }
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
}

func (pf *PanelsFrame) Show(scr *vtui.ScreenBuf) {
	// 0. Dynamic Layout Adjustment
	if pf.termView.UseAltScreen != pf.lastAlt {
		pf.lastAlt = pf.termView.UseAltScreen
		pf.ResizeConsole(pf.lastW, pf.lastH)
	}

	if pf.showPanels {
		pf.termView.SetVisible(false)
		if pf.activeIdx == 0 {
			pf.left.SetFocus(true)
			pf.right.SetFocus(false)
		} else {
			pf.left.SetFocus(false)
			pf.right.SetFocus(true)
		}
		pf.left.Show(scr)
		pf.right.Show(scr)
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

	// Alt+F5: Dummy Long Operation for debugging
	if e.VirtualKeyCode == vtinput.VK_F5 && alt && !ctrl && e.KeyDown {
		pf.showDummyOpDialog()
		return true
	}

	if e.Type == vtinput.FocusEventType {
		if e.SetFocus {
			if fsp, ok := pf.left.(*FileSystemPanel); ok { fsp.Refresh() }
			if fsp, ok := pf.right.(*FileSystemPanel); ok { fsp.Refresh() }
		}
		// Propagate focus to command line so its cursor state stays in sync
		pf.cmdLine.ProcessKey(e)
		return true
	}

	shift := (e.ControlKeyState & vtinput.ShiftPressed) != 0
	// ctrl и alt уже объявлены выше в начале функции

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

	// F10 exits the application (global, but can be overridden by terminal raw mode)
	if e.VirtualKeyCode == vtinput.VK_F10 {
		vtui.FrameManager.Shutdown()
		return true
	}

	// Standard keys for file operations
	switch e.VirtualKeyCode {
	case vtinput.VK_F1:
		pf.ShowHelp()
		return true
	case vtinput.VK_F3:
		return pf.HandleCommand(vtui.CmView, nil)
	case vtinput.VK_F4:
		if shift {
			var fsp *FileSystemPanel
			var ok bool
			if pf.activeIdx == 0 { fsp, ok = pf.left.(*FileSystemPanel) } else { fsp, ok = pf.right.(*FileSystemPanel) }
			if ok {
				dir := fsp.vfs.GetPath()
				vtui.InputBox(Msg("Edit.NewFileTitle"), Msg("Edit.NewFilePrompt"), "", func(name string) {
					if name == "" { name = "newfile.txt" }
					pf.openEditor(filepath.Join(dir, name))
				})
				return true
			}
		}
		return pf.HandleCommand(vtui.CmEdit, nil)
	case vtinput.VK_F5:
		return pf.HandleCommand(vtui.CmCopy, nil)
	case vtinput.VK_F6:
		return pf.HandleCommand(vtui.CmMove, nil)
	case vtinput.VK_F7:
		return pf.HandleCommand(vtui.CmMkDir, nil)
	case vtinput.VK_F8:
		return pf.HandleCommand(vtui.CmDelete, nil)
	case vtinput.VK_F10:
		vtui.FrameManager.Shutdown()
		return true
	}
	if e.VirtualKeyCode == vtinput.VK_ESCAPE && !pf.cmdLine.IsEmpty() {
		pf.cmdLine.Clear()
		return true
	}

	// Ctrl+Enter inserts selected file name
	if e.VirtualKeyCode == vtinput.VK_RETURN && ctrl {
		var name string
		if pf.activeIdx == 0 && pf.left != nil {
			name = pf.left.GetSelectedName()
		} else if pf.activeIdx == 1 && pf.right != nil {
			name = pf.right.GetSelectedName()
		}
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
				// 1. Determine current path of active panel
				var path string
				if pf.activeIdx == 0 {
					if fsp, ok := pf.left.(*FileSystemPanel); ok { path = fsp.vfs.GetPath() }
				} else {
					if fsp, ok := pf.right.(*FileSystemPanel); ok { path = fsp.vfs.GetPath() }
				}

				// 2. Sync PTY directory (send cd) and then the command
				if path != "" {
					pf.pty.Write([]byte(fmt.Sprintf(" cd %q\r", path)))
				}
				pf.pty.Write([]byte(cmd + "\r"))
			}
			pf.cmdLine.Clear()
			pf.showPanels = false // Auto-hide panels to show output
			return true
		} else if !pf.showPanels {
			if pf.pty != nil {
				pf.pty.Write([]byte("\r"))
			}
			return true
		}
		// If command line is empty and panels visible, Enter is passed to panels (to enter dir)
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

	// Ctrl+W closes current screen
	if e.VirtualKeyCode == vtinput.VK_W && ctrl {
		vtui.FrameManager.Flash()
		vtui.FrameManager.CloseActiveScreen()
		return true
	}

	// 3. Try Active Panel
	panelHandled := false
	if pf.activeIdx == 0 && pf.left != nil {
		panelHandled = pf.left.ProcessKey(e)
	} else if pf.activeIdx == 1 && pf.right != nil {
		panelHandled = pf.right.ProcessKey(e)
	}

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

func (pf *PanelsFrame) ProcessMouse(e *vtinput.InputEvent) bool {
	// Determine which panel was clicked
	mx, my := int(e.MouseX), int(e.MouseY)

	if pf.left != nil {
		x1, y1, x2, y2 := pf.left.GetPosition()
		if mx >= x1 && mx <= x2 && my >= y1 && my <= y2 {
			pf.activeIdx = 0
			return pf.left.ProcessMouse(e)
		}
	}

	if pf.right != nil {
		x1, y1, x2, y2 := pf.right.GetPosition()
		if mx >= x1 && mx <= x2 && my >= y1 && my <= y2 {
			pf.activeIdx = 1
			return pf.right.ProcessMouse(e)
		}
	}

	return false
}

// HandleCommand intercepts global commands (like CmQuit or CmCopy)
// sent by menus or other views.
func (pf *PanelsFrame) HandleCommand(cmd int, args any) bool {
	switch cmd {
	case vtui.CmQuit:
		vtui.FrameManager.Shutdown()
		return true

	case vtui.CmView:
		var fsp *FileSystemPanel
		var ok bool
		if pf.activeIdx == 0 { fsp, ok = pf.left.(*FileSystemPanel) } else { fsp, ok = pf.right.(*FileSystemPanel) }
		if ok {
			name := fsp.GetSelectedName()
			path := fsp.vfs.Join(fsp.vfs.GetPath(), name)
			pf.openViewer(path)
		}
		return true

	case vtui.CmEdit:
		var fsp *FileSystemPanel
		var ok bool
		if pf.activeIdx == 0 { fsp, ok = pf.left.(*FileSystemPanel) } else { fsp, ok = pf.right.(*FileSystemPanel) }
		if ok {
			name := fsp.GetSelectedName()
			path := fsp.vfs.Join(fsp.vfs.GetPath(), name)
			pf.openEditor(path)
		}
		return true

	case vtui.CmCopy, vtui.CmMove:
		isMove := cmd == vtui.CmMove
		var fspSrc, fspDst *FileSystemPanel
		var okSrc, okDst bool

		if pf.activeIdx == 0 {
			fspSrc, okSrc = pf.left.(*FileSystemPanel)
			fspDst, okDst = pf.right.(*FileSystemPanel)
		} else {
			fspSrc, okSrc = pf.right.(*FileSystemPanel)
			fspDst, okDst = pf.left.(*FileSystemPanel)
		}
		if !okSrc || !okDst { return true }

		names := fspSrc.GetSelectedNames()
		if len(names) == 0 { return true }

		title := Msg("Copy.Title")
		prompt := Msg("Copy.Prompt")
		if isMove {
			title = Msg("Move.Title")
			prompt = Msg("Move.Prompt")
		}

		srcVfs, dstVfs := fspSrc.vfs, fspDst.vfs
		dlg := vtui.NewDialog(0, 0, 50, 11, title)
		dlg.Center(pf.lastW, pf.lastH)
		dlg.ShowClose = true

		dlg.AddItem(vtui.NewLabel(dlg.X1+2, dlg.Y1+2, fmt.Sprintf(prompt, len(names)), nil))
		editDest := vtui.NewEdit(dlg.X1+2, dlg.Y1+3, 46, dstVfs.GetPath())
		dlg.AddItem(editDest)

		chkFork := vtui.NewCheckbox(dlg.X1+2, dlg.Y1+5, Msg("Op.ClonePanels"), false)
		dlg.AddItem(chkFork)

		btnOk := vtui.NewButton(dlg.X1+10, dlg.Y1+8, Msg("Copy.Btn"))
		if isMove { btnOk = vtui.NewButton(dlg.X1+10, dlg.Y1+8, Msg("Move.Btn")) }

		btnOk.OnClick = func() {
			dest := editDest.GetText()
			forked := chkFork.State == 1
			dlg.Close()
			if dest != "" {
				go pf.ExecuteFileOp(srcVfs, dstVfs, names, dest, isMove, forked)
			}
		}
		dlg.AddItem(btnOk)

		btnCancel := vtui.NewButton(dlg.X1+25, dlg.Y1+8, "Cancel")
		btnCancel.OnClick = func() { dlg.Close() }
		dlg.AddItem(btnCancel)

		vtui.FrameManager.Push(dlg)
		return true

	case vtui.CmMkDir:
		var fsp *FileSystemPanel
		var ok bool
		if pf.activeIdx == 0 { fsp, ok = pf.left.(*FileSystemPanel) } else { fsp, ok = pf.right.(*FileSystemPanel) }
		if !ok { return true }

		panel := fsp
		activeVfs := fsp.vfs

		vtui.InputBox(Msg("MakeFolder.Title"), Msg("MakeFolder.Prompt"), "", func(name string) {
			if name == "" { return }
			fullPath := activeVfs.Join(activeVfs.GetPath(), name)
			if err := activeVfs.MkDir(fullPath); err != nil {
				vtui.ShowMessage(" Error ", fmt.Sprintf(Msg("Operation.Error"), err.Error()), []string{"&Ok"})
			}
			pf.RefreshAll()
			panel.SelectName(name)
		})
		return true

	case vtui.CmDelete:
		var fsp *FileSystemPanel
		var ok bool
		if pf.activeIdx == 0 { fsp, ok = pf.left.(*FileSystemPanel) } else { fsp, ok = pf.right.(*FileSystemPanel) }
		if !ok { return true }

		activeVfs := fsp.vfs
		names := fsp.GetSelectedNames()
		if len(names) == 0 { return true }

		msgName := names[0]
		if len(names) > 1 {
			msgName = fmt.Sprintf("%d items", len(names))
		}

		msg := fmt.Sprintf(Msg("Delete.Confirm"), msgName)
		dlg := vtui.ShowMessage(Msg("Delete.Title"), msg, []string{Msg("Delete.Btn"), "Cancel"})
		dlg.OnResult = func(code int) {
			if code == 0 {
				for _, name := range names {
					fullPath := activeVfs.Join(activeVfs.GetPath(), name)
					if err := activeVfs.Remove(fullPath); err != nil {
						vtui.ShowMessage(" Error ", fmt.Sprintf(Msg("Operation.Error"), err.Error()), []string{"&Ok"})
						break
					}
				}
				pf.RefreshAll()
			}
		}
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

func (pf *PanelsFrame) ExecuteDummyOp(forked bool) {
	title := " Processing... "

	// Mode 1 (Headless): Workspace = [Desktop, Dialog]
	// Mode 2 (Forked): Workspace = [Desktop, ClonedPanels, Dialog]

	dlg := vtui.NewDialog(0, 0, 45, 8, title)
	dlg.AttentionSuppressed = true
	dlg.Center(vtui.FrameManager.GetScreenSize(), 25)
	lbl := vtui.NewText(dlg.X1+2, dlg.Y1+2, "Initializing...", vtui.Palette[vtui.ColDialogText])
	dlg.AddItem(lbl)

	btnCancel := vtui.NewButton(dlg.X1+16, dlg.Y1+5, "&Cancel")
	var taskCtx *vtui.TaskContext
	btnCancel.OnClick = func() { if taskCtx != nil { taskCtx.Cancel() }; dlg.Close() }
	dlg.AddItem(btnCancel)

	// Create and switch to the new workspace
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
		totalSteps := 300 // 5 minutes = 300 seconds
		for i := 1; i <= totalSteps; i++ {
			if ctx.Err() != nil || dlg.IsDone() { return }

			time.Sleep(1 * time.Second)
			percent := (i * 100) / totalSteps

			ctx.RunOnUI(func() {
				lbl.SetText(fmt.Sprintf("Step %d of %d...", i, totalSteps))
				dlg.SetProgress(percent)
				vtui.FrameManager.Redraw()
			})
		}

		ctx.RunOnUI(func() {
			if ctx.Err() == nil && !dlg.IsDone() {
				vtui.ShowMessageOn(dlg, " Done ", "Dummy operation finished!", []string{"&Ok"})
				dlg.Close()
			}
		})
	})
}
func (pf *PanelsFrame) RefreshAll() {
	if fsp, ok := pf.left.(*FileSystemPanel); ok {
		fsp.Refresh()
	}
	if fsp, ok := pf.right.(*FileSystemPanel); ok {
		fsp.Refresh()
	}
}
func (pf *PanelsFrame) GetTitle() string {
	path := ""
	// Show the path of the active panel
	if pf.activeIdx == 0 {
		if fsp, ok := pf.left.(*FileSystemPanel); ok { path = fsp.vfs.GetPath() }
	} else {
		if fsp, ok := pf.right.(*FileSystemPanel); ok { path = fsp.vfs.GetPath() }
	}

	if path != "" {
		return "Panels: " + path
	}
	return "Panels"
}

func (pf *PanelsFrame) Clone() *PanelsFrame {
	clone := NewPanelsFrame()
	// Only resize if parent has been sized, otherwise keep default 80x24
	if pf.lastW > 0 && pf.lastH > 0 {
		clone.ResizeConsole(pf.lastW, pf.lastH)
	}

	if fsp, ok := pf.left.(*FileSystemPanel); ok {
		clone.left.(*FileSystemPanel).vfs.SetPath(fsp.vfs.GetPath())
		clone.left.(*FileSystemPanel).Refresh()
		clone.left.(*FileSystemPanel).table.SelectPos = fsp.table.SelectPos
		clone.left.(*FileSystemPanel).table.TopPos = fsp.table.TopPos
	}
	if fsp, ok := pf.right.(*FileSystemPanel); ok {
		clone.right.(*FileSystemPanel).vfs.SetPath(fsp.vfs.GetPath())
		clone.right.(*FileSystemPanel).Refresh()
		clone.right.(*FileSystemPanel).table.SelectPos = fsp.table.SelectPos
		clone.right.(*FileSystemPanel).table.TopPos = fsp.table.TopPos
	}
	clone.activeIdx = pf.activeIdx
	clone.showKeyBar = pf.showKeyBar
	clone.showPanels = pf.showPanels

	if pf.termView != nil && clone.termView != nil {
		clone.termView.CloneStateFrom(pf.termView)
	}
	return clone
}
