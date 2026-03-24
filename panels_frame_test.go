package main

import (
	"testing"
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
	}

	// Verify they are independent instances
	clone.activeIdx = 1
	if pf.activeIdx == 1 {
		t.Error("Clone should be independent from its parent")
	}
}
func TestPanelsFrame_Clone_TerminalData(t *testing.T) {
	pf := NewPanelsFrame()

	// Simulate some terminal output
	pf.termView.PutChar('H', 0)
	pf.termView.PutChar('i', 0)

	clone := pf.Clone()

	if clone.termView.pt.String() != "Hi" {
		t.Errorf("Terminal log not cloned. Got %q", clone.termView.pt.String())
	}
	if clone.termView.CursorX != pf.termView.CursorX {
		t.Error("Terminal CursorX not cloned")
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
