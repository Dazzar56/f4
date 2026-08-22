package main

import (
	"testing"

	"github.com/unxed/vtui"
)

func TestPanelsFrame_SideMenusExposeDriveHotkeys(t *testing.T) {
	pf := &PanelsFrame{}

	left := pf.leftMenu().SubItems
	if !findSideDriveMenuItem(left, Msg("Menu.Left.DriveMenu"), "Alt+F1", CmLeftDriveMenu) {
		t.Fatalf("left drive menu has no drive item: %+v", left)
	}

	right := pf.rightMenu().SubItems
	if !findSideDriveMenuItem(right, Msg("Menu.Right.DriveMenu"), "Alt+F2", CmRightDriveMenu) {
		t.Fatalf("right drive menu has no drive item: %+v", right)
	}
}

func findSideDriveMenuItem(items []vtui.MenuItem, label, shortcut string, command int) bool {
	for _, item := range items {
		if item.Text == label && item.Shortcut == shortcut && item.Command == command {
			return true
		}
	}
	return false
}
