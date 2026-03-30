package main

import (
	"os"
	"path/filepath"
	"testing"
	"runtime"
	"time"
	"strings"
	"github.com/unxed/vtui"
	"github.com/unxed/vtinput"
)

func TestPanelsFrame_Layout(t *testing.T) {
	vtui.SetDefaultPalette()
	SetDefaultF4Palette()
	pf := NewPanelsFrame()

	// Simulate 80x25 terminal
	pf.ResizeConsole(80, 25)

	// Calculate expected positions for 80x25 with KeyBar
	expectedKeyBarY := 24
	expectedCmdLineY := 23 // Always 1 line above KeyBar if KeyBar is present

	// 1. Check reserved rows with KeyBar visible
	if pf.keyBar.Y1 != expectedKeyBarY {
		t.Errorf("KeyBar position error: expected %d, got %d", expectedKeyBarY, pf.keyBar.Y1)
	}
	if pf.cmdLine.Y1 != expectedCmdLineY {
		t.Errorf("CommandLine position error: expected %d, got %d", expectedCmdLineY, pf.cmdLine.Y1)
	}

	// 2. Check layout after hiding KeyBar
	pf.showKeyBar = false
	pf.ResizeConsole(80, 25)

	// After hiding KeyBar, CommandLine should move to the bottom row
	expectedKeyBarY = 24 // Still the last line, but invisible
	expectedCmdLineY = 24
	if pf.cmdLine.Y1 != expectedCmdLineY {
		t.Errorf("CommandLine should be at %d when KeyBar hidden, got %d", expectedCmdLineY, pf.cmdLine.Y1)
	}
	if pf.keyBar.IsVisible() {
		t.Error("KeyBar should be invisible")
	}
}
func TestPanelsFrame_ProcessMouse_DoubleClick(t *testing.T) {
	pf := NewPanelsFrame()
	pf.ResizeConsole(80, 25)

	// Active is initially right (1)
	if pf.activeIdx != 1 {
		t.Fatalf("Expected initial activeIdx 1, got %d", pf.activeIdx)
	}

	tmp := t.TempDir()
	fsp := pf.left.(*FileSystemPanel)
	fsp.vfs.SetPath(tmp)
	fsp.ReadDirectory() // Will contain ".." at index 0

	initialPath := fsp.vfs.GetPath()

	// Double click on ".." in left panel.
	// Left panel 0..39. Table start Y=1. Header Y=1. Row 0 at Y=2.
	pf.ProcessMouse(&vtinput.InputEvent{
		Type:        vtinput.MouseEventType,
		KeyDown:     true,
		MouseX:      5,
		MouseY:      2,
		ButtonState: vtinput.FromLeft1stButtonPressed,
		MouseEventFlags: vtinput.DoubleClick,
	})

	if pf.activeIdx != 0 {
		t.Errorf("Expected activeIdx 0 after left click, got %d", pf.activeIdx)
	}

	if fsp.vfs.GetPath() == initialPath {
		t.Error("Double click on '..' should have changed directory")
	}
}
func TestPanelsFrame_ProcessMouse_DoubleClickFile(t *testing.T) {
	pf := NewPanelsFrame()
	pf.ResizeConsole(80, 25)

	tmp := t.TempDir()
	runnablePath := filepath.Join(tmp, "run.sh")
	os.WriteFile(runnablePath, []byte("echo"), 0755)

	fsp := pf.left.(*FileSystemPanel)
	fsp.vfs.SetPath(tmp)
	fsp.ReadDirectory() // ".." at index 0, "run.sh" at index 1

	// Double click on "run.sh" in left panel. Row 1 -> Y=3
	pf.ProcessMouse(&vtinput.InputEvent{
		Type:        vtinput.MouseEventType,
		KeyDown:     true,
		MouseX:      5,
		MouseY:      3,
		ButtonState: vtinput.FromLeft1stButtonPressed,
		MouseEventFlags: vtinput.DoubleClick,
	})

	// Executing a file should hide the panels
	if pf.showPanels {
		t.Error("Double clicking a runnable file should hide the panels")
	}
}
func TestPanelsFrame_CtrlW_CloseScreen(t *testing.T) {
	// Initialize global FrameManager for the test
	fm := vtui.FrameManager
	fm.Init(vtui.NewScreenBuf())

	// Create Screen 0
	fm.Push(vtui.NewDesktop())
	pf1 := NewPanelsFrame()
	fm.Push(pf1)

	// Create Screen 1
	pf2 := NewPanelsFrame()
	fm.AddScreen(pf2)

	if fm.ActiveIdx != 1 {
		t.Fatalf("Should be on Screen 1")
	}

	// Simulate Ctrl+W on pf2
	handled := pf2.ProcessKey(&vtinput.InputEvent{
		Type:            vtinput.KeyEventType,
		KeyDown:         true,
		VirtualKeyCode:  vtinput.VK_W,
		ControlKeyState: vtinput.LeftCtrlPressed,
	})

	if !handled {
		t.Error("PanelsFrame failed to handle Ctrl+W")
	}

	// Verify Screen 1 was closed and focus fell back to Screen 0
	if fm.ActiveIdx != 0 {
		t.Errorf("Screen was not closed via Ctrl+W, ActiveIdx is %d", fm.ActiveIdx)
	}
}

func TestPanelsFrame_KeyHandling(t *testing.T) {
	pf := NewPanelsFrame()
	pf.ResizeConsole(80, 25)

	// 1. Test Tab to switch active panel
	if pf.activeIdx != 1 {
		t.Fatalf("Initial active panel should be right (1), got %d", pf.activeIdx)
	}
	pf.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_TAB})
	if pf.activeIdx != 0 {
		t.Error("Tab did not switch active panel to left (0)")
	}
	pf.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_TAB})
	if pf.activeIdx != 1 {
		t.Error("Tab did not switch active panel back to right (1)")
	}

	// 2. Test Ctrl+O to toggle panels
	if !pf.showPanels {
		t.Fatal("Panels should be visible initially")
	}
	pf.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_O, ControlKeyState: vtinput.LeftCtrlPressed})
	if pf.showPanels {
		t.Error("Ctrl+O did not hide panels")
	}
	pf.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_O, ControlKeyState: vtinput.LeftCtrlPressed})
	if !pf.showPanels {
		t.Error("Ctrl+O did not show panels again")
	}

	// 3. Test Ctrl+Enter to insert filename
	// Set focus on left panel and select the first "real" file (not "..")
	pf.activeIdx = 0
	if fsp, ok := pf.left.(*FileSystemPanel); ok {
		fsp.table.SelectPos = 1 // Assuming ".." is at 0
	}
	pf.ProcessKey(&vtinput.InputEvent{Type: vtinput.KeyEventType, KeyDown: true, VirtualKeyCode: vtinput.VK_RETURN, ControlKeyState: vtinput.LeftCtrlPressed})

	expectedName := pf.left.GetSelectedName()
	if pf.cmdLine.Edit.GetText() != " "+expectedName {
		t.Errorf("Ctrl+Enter failed: expected ' %s', got '%s'", expectedName, pf.cmdLine.Edit.GetText())
	}
}
func TestPanelsFrame_F9_MenuActivation(t *testing.T) {
	pf := NewPanelsFrame()
	pf.ResizeConsole(80, 25)

	// Active panel is Right (1)
	pf.activeIdx = 1
	pf.ProcessKey(&vtinput.InputEvent{
		Type:           vtinput.KeyEventType,
		KeyDown:        true,
		VirtualKeyCode: vtinput.VK_F9,
	})

	if !pf.menuBar.Active {
		t.Error("F9 should activate the menu bar")
	}
	if pf.menuBar.SelectPos != 4 {
		t.Errorf("F9 with Right panel active should select menu index 4, got %d", pf.menuBar.SelectPos)
	}

	// Reset
	pf.menuBar.Active = false
	pf.activeIdx = 0
	pf.ProcessKey(&vtinput.InputEvent{
		Type:           vtinput.KeyEventType,
		KeyDown:        true,
		VirtualKeyCode: vtinput.VK_F9,
	})

	if !pf.menuBar.Active {
		t.Error("F9 should activate the menu bar")
	}
	if pf.menuBar.SelectPos != 0 {
		t.Errorf("F9 with Left panel active should select menu index 0, got %d", pf.menuBar.SelectPos)
	}
}
func TestPanelsFrame_ViewModeCommands(t *testing.T) {
	pf := NewPanelsFrame()
	pf.ResizeConsole(80, 25)

	handled := pf.HandleCommand(vtui.CmLeftDetailed, nil)
	if !handled { t.Error("CmLeftDetailed not handled") }
	if pf.left.(*FileSystemPanel).viewMode != ViewModeDetailed {
		t.Error("Left panel mode not changed to Detailed")
	}

	pf.HandleCommand(vtui.CmRightDetailed, nil)
	if pf.right.(*FileSystemPanel).viewMode != ViewModeDetailed {
		t.Error("Right panel mode not changed to Detailed")
	}

	// Menu checkmarks
	menuText := pf.menuBar.Items[0].SubItems[1].Text
	if !strings.HasPrefix(menuText, "√") {
		t.Errorf("Menu checkmark not updated, got %q", menuText)
	}
}
func TestPanelsFrame_RefreshOnFocus(t *testing.T) {
	pf := NewPanelsFrame()

	// We need to verify Refresh was called.
	// Since we don't have a mock VFS easily swappable here without refactoring,
	// we check if the internal state handles the focus event without crashing
	// and returns true.

	handled := pf.ProcessKey(&vtinput.InputEvent{
		Type:     vtinput.FocusEventType,
		SetFocus: true,
	})

	if !handled {
		t.Error("PanelsFrame should handle FocusEventType and return true")
	}
}
func TestPanelsFrame_Clone(t *testing.T) {
	pf := NewPanelsFrame()
	pf.ResizeConsole(100, 30)

	// Set some specific state
	pf.activeIdx = 0
	if fsp, ok := pf.left.(*FileSystemPanel); ok {
		fsp.vfs.SetPath("/tmp")
		fsp.table.SelectPos = 5
	}

	// Clone the panels
	clone := pf.Clone()

	// Verify state transfer
	if clone.activeIdx != 0 {
		t.Errorf("Clone failed to copy activeIdx: %d", clone.activeIdx)
	}

	if fsp, ok := clone.left.(*FileSystemPanel); ok {
		if fsp.vfs.GetPath() != "/tmp" {
			t.Errorf("Clone failed to copy VFS path: %s", fsp.vfs.GetPath())
		}
		if fsp.table.SelectPos != 5 {
			t.Errorf("Clone failed to copy Table SelectPos: %d", fsp.table.SelectPos)
		}
		if fsp.viewMode != pf.left.(*FileSystemPanel).viewMode {
			t.Error("Clone failed to copy ViewMode")
		}
	}

	// Verify they are independent instances
	clone.activeIdx = 1
	if pf.activeIdx == 1 {
		t.Error("Clone should be independent from its parent")
	}
}
func TestPanelsFrame_Clone_TerminalData(t *testing.T) {
	pf := NewPanelsFrame()

	// 1. Simulate complex terminal output (2 lines)
	// We add a trailing newline so "L2" becomes history.
	// CloneStateFrom intentionally wipes the current ACTIVE line to avoid duplicate prompt.
	pf.termView.PutChar('L', 0)
	pf.termView.PutChar('1', 0)
	pf.termView.PutChar('\n', 0)
	pf.termView.PutChar('L', 0)
	pf.termView.PutChar('2', 0)
	pf.termView.PutChar('\n', 0)

	clone := pf.Clone()

	// 2. Check if log is deep-copied
	if clone.termView.pt.String() != "L1\nL2\n" {
		t.Errorf("Terminal log not cloned. Got %q", clone.termView.pt.String())
	}

	// 3. CRITICAL: Check if LineIndex is correctly pointing to the NEW pt
	// Expected 3 lines: L1\n, L2\n, and the new active empty line.
	if clone.termView.li.LineCount() != 3 {
		t.Errorf("Terminal LineIndex not synced in clone. Expected 3 lines, got %d", clone.termView.li.LineCount())
	}

	// 4. Check if visual grid is copied
	// Note: We check the PREVIOUS line because the current line was wiped by CloneStateFrom
	if clone.termView.Lines[pf.termView.CursorY-1][0].Char != 'L' {
		t.Error("Terminal visual grid (Lines) history not copied to clone")
	}

	// 5. Verify prompt reset logic
	if clone.termView.CursorX != 0 {
		t.Errorf("Expected clone CursorX to be 0 after prompt wipe, got %d", clone.termView.CursorX)
	}
	if clone.termView.Lines[clone.termView.CursorY][0].Char != ' ' {
		t.Error("Current terminal line was not cleared during clone")
	}
}
func TestPanelsFrame_Labels(t *testing.T) {
	pf := NewPanelsFrame()
	ks := pf.GetKeyLabels()

	if ks == nil {
		t.Fatal("PanelsFrame labels are nil")
	}

	// F3 in panels should be "View" (or whatever you set in lang.go)
	if ks.Normal[2] == "" {
		t.Error("PanelsFrame F3 label should not be empty")
	}
}
func TestPanelsFrame_HistoryNavigation(t *testing.T) {
	pf := NewPanelsFrame()
	pf.ResizeConsole(80, 25) // Initialize panels
	pf.showPanels = false    // Hide panels to enable history intercept
	pf.cmdLine.AddHistory("git status")

	// Press Up Arrow
	pf.ProcessKey(&vtinput.InputEvent{
		Type:           vtinput.KeyEventType,
		KeyDown:        true,
		VirtualKeyCode: vtinput.VK_UP,
	})

	if pf.cmdLine.Edit.GetText() != "git status" {
		t.Errorf("PanelsFrame failed to pass Up Arrow to history. Got '%s'", pf.cmdLine.Edit.GetText())
	}

	// Reset, show panels, try again
	pf.cmdLine.Clear()
	pf.cmdLine.historyPos = -1
	pf.showPanels = true

	pf.ProcessKey(&vtinput.InputEvent{
		Type:           vtinput.KeyEventType,
		KeyDown:        true,
		VirtualKeyCode: vtinput.VK_UP,
	})

	if pf.cmdLine.Edit.GetText() != "" {
		t.Error("Up Arrow should NOT trigger history when panels are visible")
	}
}
func TestPanelsFrame_EnterAddsToHistory(t *testing.T) {
	pf := NewPanelsFrame()
	pf.cmdLine.Edit.SetText("ls -la")

	// Simulate Enter
	pf.ProcessKey(&vtinput.InputEvent{
		Type:           vtinput.KeyEventType,
		KeyDown:        true,
		VirtualKeyCode: vtinput.VK_RETURN,
	})

	if len(pf.cmdLine.History) == 0 || pf.cmdLine.History[0] != "ls -la" {
		t.Errorf("Command was not added to history on Enter. History: %v", pf.cmdLine.History)
	}
}

func TestPanelsFrame_AltScreenTerminalHeight(t *testing.T) {
	pf := NewPanelsFrame()
	height := 25
	pf.showKeyBar = true

	// 1. Normal mode: terminal should leave space for KeyBar
	pf.termView.UseAltScreen = false
	pf.ResizeConsole(80, height)
	// termY2 should be h-2 (23)
	if pf.termView.Y2 != 23 {
		t.Errorf("Normal mode: expected terminal Y2=23, got %d", pf.termView.Y2)
	}

	// 2. AltScreen mode: terminal should occupy the KeyBar's row
	pf.termView.UseAltScreen = true
	pf.ResizeConsole(80, height)
	// termY2 should be h-1 (24)
	if pf.termView.Y2 != 24 {
		t.Errorf("AltScreen mode: expected terminal Y2=24, got %d", pf.termView.Y2)
	}
}

func TestPanelsFrame_KeyBarSuppression(t *testing.T) {
	vtui.SetDefaultPalette()
	scr := vtui.NewScreenBuf()
	scr.AllocBuf(80, 25)
	vtui.FrameManager.Init(scr)

	pf := NewPanelsFrame()
	pf.showKeyBar = true
	pf.ResizeConsole(80, 25)

	// We need to simulate the frame being on top to trigger the logic
	vtui.FrameManager.Push(pf)

	// 1. Normal mode: KeyBar should be registered
	pf.termView.UseAltScreen = false
	pf.Show(scr)
	if vtui.FrameManager.KeyBar == nil {
		t.Error("KeyBar should be registered in FrameManager in normal mode")
	}

	// 2. AltScreen mode: KeyBar should be removed from FrameManager
	pf.termView.UseAltScreen = true
	pf.Show(scr)
	if vtui.FrameManager.KeyBar != nil {
		t.Error("KeyBar should be UNregistered from FrameManager in AltScreen mode")
	}
}
func TestPanelsFrame_RefreshAll(t *testing.T) {
	pf := NewPanelsFrame()
	// Test that RefreshAll doesn't crash on freshly initialized panels
	pf.RefreshAll()
}
func TestPanelsFrame_CloneIndependence(t *testing.T) {
	pf := NewPanelsFrame()
	pf.ResizeConsole(80, 25)

	// Set path in original
	fsp := pf.left.(*FileSystemPanel)
	origPath := t.TempDir()
	fsp.vfs.SetPath(origPath)

	// Clone
	clone := pf.Clone()

	// Change path in clone
	newPath := t.TempDir()
	clone.left.(*FileSystemPanel).vfs.SetPath(newPath)

	// Verify original is unchanged
	if pf.left.(*FileSystemPanel).vfs.GetPath() != origPath {
		t.Error("Cloned PanelsFrame shares VFS state with parent!")
	}
}
func TestIsTerminalRunnable(t *testing.T) {
	tmpDir := t.TempDir()

	// 1. Обычный текстовый файл -> false
	txtFile := filepath.Join(tmpDir, "test.txt")
	os.WriteFile(txtFile, []byte("hello"), 0644)
	if isTerminalRunnable(txtFile) {
		t.Error("Text file should not be terminal-runnable")
	}

	// 2. Файл с расширением .sh -> true
	shFile := filepath.Join(tmpDir, "test.sh")
	os.WriteFile(shFile, []byte("echo hi"), 0644)
	if !isTerminalRunnable(shFile) {
		t.Error(".sh file should be terminal-runnable")
	}

	// 3. Файл с шебангом без расширения -> true
	binFile := filepath.Join(tmpDir, "my-tool")
	os.WriteFile(binFile, []byte("#!/usr/bin/env bash\necho hi"), 0644)
	if !isTerminalRunnable(binFile) {
		t.Error("File with shebang should be terminal-runnable")
	}

	// 4. Директория -> false
	subDir := filepath.Join(tmpDir, "folder")
	os.Mkdir(subDir, 0755)
	if isTerminalRunnable(subDir) {
		t.Error("Directory should not be terminal-runnable")
	}

	// 5. Unix Executable Bit (если не на Windows)
	if runtime.GOOS != "windows" {
		execFile := filepath.Join(tmpDir, "compiled-bin")
		os.WriteFile(execFile, []byte{0x7f, 'E', 'L', 'F'}, 0755)
		if !isTerminalRunnable(execFile) {
			t.Error("File with executable bit should be terminal-runnable on Unix")
		}
	}
}

func TestPanelsFrame_ReturnExecution(t *testing.T) {
	pf := NewPanelsFrame()
	pf.ResizeConsole(80, 25)

	// Создаем временный запускаемый файл
	tmp := t.TempDir()
	runnablePath := filepath.Join(tmp, "runme.sh")
	os.WriteFile(runnablePath, []byte("echo 1"), 0755)

	// Настраиваем VFS и выбираем этот файл на панели
	fsp := pf.right.(*FileSystemPanel)
	fsp.vfs.SetPath(tmp)
	fsp.ReadDirectory()
	fsp.SelectName("runme.sh")

	// Проверяем начальное состояние
	if !pf.showPanels {
		t.Fatal("Panels should be visible initially")
	}

	// Имитируем нажатие Enter
	pf.ProcessKey(&vtinput.InputEvent{
		Type:           vtinput.KeyEventType,
		KeyDown:        true,
		VirtualKeyCode: vtinput.VK_RETURN,
	})

	// После запуска исполняемого файла панели должны скрыться, чтобы показать терминал
	if pf.showPanels {
		t.Error("Panels should be hidden after executing a terminal-runnable file")
	}
}
func TestPanelsFrame_CommandLineEnter(t *testing.T) {
	pf := NewPanelsFrame()
	pty := &mockPty{} // Используем mock из ansi_parser_test.go
	pf.pty = pty

	// Вводим команду в консоль
	pf.cmdLine.Edit.SetText("ls -la")

	// Нажимаем Enter
	pf.ProcessKey(&vtinput.InputEvent{
		Type:           vtinput.KeyEventType,
		KeyDown:        true,
		VirtualKeyCode: vtinput.VK_RETURN,
	})

	// Панели должны скрыться
	if pf.showPanels {
		t.Error("Panels should hide after command execution from command line")
	}
	// PTY должен получить команду
	if !strings.Contains(string(pty.written), "ls -la\r") {
		t.Errorf("PTY did not receive command. Got: %q", string(pty.written))
	}
}

func TestPanelsFrame_DirectoryEnter(t *testing.T) {
	pf := NewPanelsFrame()
	pf.ResizeConsole(80, 25)
	tmp := t.TempDir()
	sub := filepath.Join(tmp, "work_dir")
	os.Mkdir(sub, 0755)

	fsp := pf.right.(*FileSystemPanel)
	fsp.vfs.SetPath(tmp)
	fsp.ReadDirectory() // Populate entries
	fsp.SelectName("work_dir")

	// Нажимаем Enter на директории
	pf.ProcessKey(&vtinput.InputEvent{
		Type:           vtinput.KeyEventType,
		KeyDown:        true,
		VirtualKeyCode: vtinput.VK_RETURN,
	})

	// Панели НЕ должны скрываться
	if !pf.showPanels {
		t.Error("Panels should NOT hide when entering a directory")
	}
	// Путь должен измениться
	if fsp.vfs.GetPath() != sub {
		t.Errorf("VFS path did not change. Expected %s, got %s", sub, fsp.vfs.GetPath())
	}
}

func TestPanelsFrame_NonRunnableOpen(t *testing.T) {
	pf := NewPanelsFrame()
	pf.ResizeConsole(80, 25)
	tmp := t.TempDir()
	docPath := filepath.Join(tmp, "readme.txt")
	os.WriteFile(docPath, []byte("some text"), 0644)

	fsp := pf.right.(*FileSystemPanel)
	fsp.vfs.SetPath(tmp)
	fsp.ReadDirectory()
	fsp.SelectName("readme.txt")

	// Нажимаем Enter на текстовом файле
	pf.ProcessKey(&vtinput.InputEvent{
		Type:           vtinput.KeyEventType,
		KeyDown:        true,
		VirtualKeyCode: vtinput.VK_RETURN,
	})

	// Панели должны остаться видимыми (так как открытие идет через внешнюю ОС)
	if !pf.showPanels {
		t.Error("Panels should stay visible when opening non-runnable files via OS associations")
	}
}

func TestExecuteFileOp_BackgroundButtonTrigger(t *testing.T) {
	// This test ensures that the logic inside Background button click works
	fm := vtui.FrameManager
	fm.Init(vtui.NewScreenBuf())

	pf := NewPanelsFrame()
	pf.ResizeConsole(80, 25)
	fm.Push(pf)

	initialScreens := len(fm.Screens)

	// Simulate what Background button does:
	fork := pf.Clone()
	fm.AddScreen(fork)

	if len(fm.Screens) != initialScreens + 1 {
		t.Errorf("Backgrounding failed to create a new screen. Got %d, want %d", len(fm.Screens), initialScreens+1)
	}
}
func TestExecuteDummyOp_HeadlessMode(t *testing.T) {
	fm := vtui.FrameManager
	fm.Init(vtui.NewScreenBuf())
	pf := NewPanelsFrame()
	fm.Push(pf)

	initialScreens := len(fm.Screens)

	// Trigger Mode 1 (Headless)
	go pf.ExecuteDummyOp(false)

	// Manually process the task queue (since we are not in fm.Run loop)
	select {
	case task := <-fm.TaskChan:
		task()
	case <-time.After(1 * time.Second):
		t.Fatal("ExecuteDummyOp did not post workspace creation task")
	}

	if len(fm.Screens) != initialScreens + 1 {
		t.Fatalf("Headless screen not created. Got %d", len(fm.Screens))
	}

	newScreen := fm.Screens[len(fm.Screens)-1]
	if len(newScreen.Frames) != 1 { // Только диалог, без Desktop
		t.Errorf("Headless screen should have 1 frame, got %d", len(newScreen.Frames))
	}
	if !newScreen.Transparent {
		t.Error("Headless screen should be transparent")
	}
}

func TestPanelsFrame_TerminalForwarding_Legacy(t *testing.T) {
	pf := NewPanelsFrame()
	pf.showPanels = false
	pf.termView.UseAltScreen = true
	
	// Mock PTY
	pty := &mockPty{}
	pf.pty = pty

	// 1. Ctrl+W should be FORWARDED (Legacy mode has no Kitty/Win32 flags)
	// For letters, TranslateInput expects the Char field to be populated.
	pf.ProcessKey(&vtinput.InputEvent{
		Type: vtinput.KeyEventType, KeyDown: true,
		VirtualKeyCode: vtinput.VK_W, Char: 'w', ControlKeyState: vtinput.LeftCtrlPressed,
	})
	if !strings.Contains(string(pty.written), "\x17") { // 0x17 is Ctrl+W byte
		t.Error("Ctrl+W should be forwarded to terminal in legacy mode")
	}
	pty.written = nil

	// 2. Ctrl+Tab should NOT be forwarded (returns false, handled by FrameManager)
	handled := pf.ProcessKey(&vtinput.InputEvent{
		Type: vtinput.KeyEventType, KeyDown: true,
		VirtualKeyCode: vtinput.VK_TAB, ControlKeyState: vtinput.LeftCtrlPressed,
	})
	if handled {
		t.Error("Ctrl+Tab should NOT be handled by PanelsFrame in legacy mode")
	}
	if len(pty.written) > 0 {
		t.Error("PTY received bytes for Ctrl+Tab in legacy mode")
	}
}

func TestPanelsFrame_TerminalForwarding_Advanced(t *testing.T) {
	pf := NewPanelsFrame()
	pf.showPanels = false
	pf.termView.UseAltScreen = true
	pf.termView.Win32InputMode = true // Advanced mode
	
	pty := &mockPty{}
	pf.pty = pty

	// 1. Ctrl+Tab should be FORWARDED in Advanced mode
	handled := pf.ProcessKey(&vtinput.InputEvent{
		Type: vtinput.KeyEventType, KeyDown: true,
		VirtualKeyCode: vtinput.VK_TAB, ControlKeyState: vtinput.LeftCtrlPressed,
	})
	if !handled {
		t.Error("Ctrl+Tab should be handled by PanelsFrame in Advanced mode")
	}
	if len(pty.written) == 0 {
		t.Error("PTY did not receive Win32 sequence for Ctrl+Tab")
	}
	pty.written = nil

	// 2. Shift+Ctrl+Tab should NOT be forwarded in any mode
	handled = pf.ProcessKey(&vtinput.InputEvent{
		Type: vtinput.KeyEventType, KeyDown: true,
		VirtualKeyCode: vtinput.VK_TAB, ControlKeyState: vtinput.LeftCtrlPressed | vtinput.ShiftPressed,
	})
	if handled {
		t.Error("Shift+Ctrl+Tab was erroneously forwarded to PTY")
	}
}
func TestPanelsFrame_FilesMenuLabels(t *testing.T) {
	pf := NewPanelsFrame()

	// Items[1] is the "Files" menu
	filesMenu := pf.menuBar.Items[1]
	if filesMenu.Label != "&Files" {
		t.Errorf("Expected Files menu label '&Files', got %q", filesMenu.Label)
	}

	// SubItems[3] should be "Rename or move"
	renMove := filesMenu.SubItems[3]
	expected := "&" + Msg("Menu.Files.RenMov")
	if renMove.Text != expected {
		t.Errorf("Expected Files item %q, got %q", expected, renMove.Text)
	}

	if renMove.Shortcut != "F6" {
		t.Errorf("Expected shortcut 'F6', got %q", renMove.Shortcut)
	}
}

