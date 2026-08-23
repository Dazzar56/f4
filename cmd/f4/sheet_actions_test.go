package main

import (
	"strings"
	"testing"

	"github.com/unxed/vtui"
)

// spreadsheetMenuItem finds the spreadsheet entry in a built menu bar.
func spreadsheetMenuItem(items []vtui.MenuBarItem) (vtui.MenuBarItem, vtui.MenuItem, bool) {
	label := Msg("Action.App.Spreadsheet")
	for _, bar := range items {
		for _, item := range bar.SubItems {
			if strings.Contains(item.Text, label) {
				return bar, item, true
			}
		}
	}
	return vtui.MenuBarItem{}, vtui.MenuItem{}, false
}

// TestSpreadsheetStaysInTheMenuWhileAPopupIsOpen guards the reason the command
// was invisible in practice.
//
// Menu items are rebuilt on every GetMenuBar call, and once the dropdown is
// open that dropdown is the top frame. An action whose Visible predicate asked
// for panels on top therefore removed itself from the very menu the user was
// reading. The predicate came from Arkanoid, where it is harmless because that
// action is HideFromMenu and the check only ever runs for the palette.
func TestSpreadsheetStaysInTheMenuWhileAPopupIsOpen(t *testing.T) {
	t.Cleanup(swapFrameManager(t))
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())
	pf := NewPanelsFrame()
	defer pf.Close()
	pf.ResizeConsole(80, 25)
	vtui.FrameManager.Push(pf)

	bar, _, ok := spreadsheetMenuItem(pf.GetMenuBar().Items)
	if !ok {
		t.Fatal("the spreadsheet command is missing from the panels menu")
	}
	if commands := plainLabel(Msg("Menu.Shell.Commands")); !strings.Contains(bar.Label, commands) {
		t.Errorf("the spreadsheet command sits in %q, expected %q", bar.Label, commands)
	}

	popup := vtui.NewVMenu("dropdown")
	popup.SetPosition(1, 1, 20, 10)
	vtui.FrameManager.Push(popup)
	if _, _, ok := spreadsheetMenuItem(BuildMenuBarItems("Shell")); !ok {
		t.Error("the spreadsheet command disappeared from the menu while a popup was on top")
	}
}

// TestSpreadsheetPathLookupIgnoresPopups keeps the file under the panel cursor
// reachable when the command is launched from a menu or from the palette,
// where the popup rather than the panels frame is on top.
func TestSpreadsheetPathLookupIgnoresPopups(t *testing.T) {
	t.Cleanup(swapFrameManager(t))
	vtui.FrameManager.Init(vtui.NewSilentScreenBuf())
	pf := NewPanelsFrame()
	defer pf.Close()
	pf.ResizeConsole(80, 25)
	vtui.FrameManager.Push(pf)

	if activePanelsFrame() != pf {
		t.Fatal("the panels frame was not found with panels on top")
	}
	popup := vtui.NewVMenu("dropdown")
	popup.SetPosition(1, 1, 20, 10)
	vtui.FrameManager.Push(popup)
	if activePanelsFrame() != pf {
		t.Error("the panels frame was not found while a popup was on top")
	}
}
