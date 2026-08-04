package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHotkeyManager(t *testing.T) {
	tmpDir := t.TempDir()
	iniPath := filepath.Join(tmpDir, "hotkeys.ini")

	// Pre-populate INI file
	content := `[Shell]
F5=My.Custom.Copy
F9=Custom.Action
CtrlU=None
`
	os.WriteFile(iniPath, []byte(content), 0644)

	hm := NewHotkeyManager(iniPath)

	// Test default existing
	if action := hm.GetAction("Shell", "F3"); action != "File.View" {
		t.Errorf("Expected F3 to be File.View, got %q", action)
	}

	// Test override
	if action := hm.GetAction("Shell", "F5"); action != "My.Custom.Copy" {
		t.Errorf("Expected F5 to be overridden as My.Custom.Copy, got %q", action)
	}

	// Test addition
	if action := hm.GetAction("Shell", "F9"); action != "Custom.Action" {
		t.Errorf("Expected F9 to be Custom.Action, got %q", action)
	}

	// Test removal
	if action := hm.GetAction("Shell", "CtrlU"); action != "" {
		t.Errorf("Expected CtrlU to be removed, got %q", action)
	}

	// Modify and save
	hm.Bind("Terminal", "AltF1", "Terminal.ShowMenu")
	hm.Save()

	// Load into a new manager
	hm2 := NewHotkeyManager(iniPath)
	if action := hm2.GetAction("Terminal", "AltF1"); action != "Terminal.ShowMenu" {
		t.Errorf("Expected saved action Terminal.ShowMenu, got %q", action)
	}
	if action := hm2.GetAction("Shell", "CtrlU"); action != "" {
		t.Errorf("Expected CtrlU to still be removed after reload, got %q", action)
	}
}
