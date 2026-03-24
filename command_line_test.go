package main

import (
	"testing"
	"github.com/unxed/vtinput"
	"github.com/unxed/vtui"
)

func TestCommandLine_Input(t *testing.T) {
	vtui.SetDefaultPalette()
	SetDefaultF4Palette()
	cl := NewCommandLine("> ")
	cl.SetPosition(0, 0, 10, 0)

	// Simulate typing 'f'
	cl.ProcessKey(&vtinput.InputEvent{
		Type:    vtinput.KeyEventType,
		KeyDown: true,
		Char:    'f',
	})

	if cl.Edit.GetText() != "f" {
		t.Errorf("Expected cmdline text 'f', got '%s'", cl.Edit.GetText())
	}

	// Simulate Backspace
	cl.ProcessKey(&vtinput.InputEvent{
		Type:           vtinput.KeyEventType,
		KeyDown:        true,
		VirtualKeyCode: vtinput.VK_BACK,
	})

	if len(cl.Edit.GetText()) != 0 {
		t.Error("CommandLine should be empty after backspace")
	}
}
func TestCommandLine_InitialFocus(t *testing.T) {
	cl := NewCommandLine("> ")

	if !cl.Edit.IsFocused() {
		t.Error("CommandLine's underlying Edit should be focused upon creation to ensure cursor visibility")
	}
	if !cl.IsFocused() {
		t.Error("CommandLine should be focused upon creation")
	}
}

func TestCommandLine_History(t *testing.T) {
	vtui.SetDefaultPalette()
	SetDefaultF4Palette()
	cl := NewCommandLine("> ")

	// 1. Test adding history
	cl.AddHistory("ls -la")
	cl.AddHistory("cd /tmp")
	cl.AddHistory("ls -la") // Duplicate of a previous one, but not the latest

	if len(cl.History) != 3 {
		t.Errorf("Expected history length 3, got %d", len(cl.History))
	}

	// 2. Test navigation
	cl.HistoryUp() // Should be "ls -la" (index 0)
	if cl.Edit.GetText() != "ls -la" {
		t.Errorf("HistoryUp(1) failed: expected 'ls -la', got '%s'", cl.Edit.GetText())
	}

	cl.HistoryUp() // Should be "cd /tmp" (index 1)
	if cl.Edit.GetText() != "cd /tmp" {
		t.Errorf("HistoryUp(2) failed: expected 'cd /tmp', got '%s'", cl.Edit.GetText())
	}

	cl.HistoryDown() // Back to "ls -la" (index 0)
	if cl.Edit.GetText() != "ls -la" {
		t.Errorf("HistoryDown(1) failed: expected 'ls -la', got '%s'", cl.Edit.GetText())
	}

	cl.HistoryDown() // Should clear the line
	if cl.Edit.GetText() != "" {
		t.Errorf("HistoryDown(2) failed: expected empty string, got '%s'", cl.Edit.GetText())
	}

	// 3. Test duplicate prevention (consecutive)
	cl.AddHistory("pwd")
	cl.AddHistory("pwd")
	if len(cl.History) != 4 { // Only one "pwd" should be added
		t.Errorf("Duplicate history prevention failed, length: %d", len(cl.History))
	}

	// 4. Test reset on typing
	cl.HistoryUp() // "pwd"
	cl.ProcessKey(&vtinput.InputEvent{
		Type:    vtinput.KeyEventType,
		KeyDown: true,
		Char:    ' ',
	})
	if cl.historyPos != -1 {
		t.Error("History browsing state should reset after typing")
	}
}
func TestCommandLine_HistoryBoundaries(t *testing.T) {
	cl := NewCommandLine("> ")
	cl.AddHistory("cmd1")

	// Go up once
	cl.HistoryUp()
	if cl.Edit.GetText() != "cmd1" { t.Fatal("Setup failed") }

	// Go up again - should stay at cmd1
	cl.HistoryUp()
	if cl.Edit.GetText() != "cmd1" {
		t.Error("HistoryUp should cap at the end of the list")
	}

	// Go down to clear
	cl.HistoryDown()
	if cl.Edit.GetText() != "" {
		t.Error("HistoryDown should clear the line when at the start of history")
	}

	// Go down again - should stay empty and not crash
	cl.HistoryDown()
	if cl.historyPos != -1 || cl.Edit.GetText() != "" {
		t.Error("HistoryDown should stay at -1 when already empty")
	}
}
