package main

import (
	"testing"
	"time"

	"github.com/unxed/vtui"
)

func TestPanelsFrame_MenuFitsSmallScreenAndScrolls(t *testing.T) {
	defer swapFrameManager(t)()
	scr := vtui.NewSilentScreenBuf()
	scr.AllocBuf(80, 10)
	vtui.FrameManager.Init(scr)

	pf := &PanelsFrame{lastW: 80, lastH: 10}
	items := make([]string, 30)
	for i := range items {
		items[i] = "Item"
	}
	pf.Menu("Long plugin menu", items, nil)

	select {
	case task := <-vtui.FrameManager.TaskChan:
		task()
	case <-time.After(time.Second):
		t.Fatal("menu task was not posted")
	}

	menu, ok := vtui.FrameManager.GetTopFrame().(*vtui.VMenu)
	if !ok {
		t.Fatalf("top frame = %T, want *vtui.VMenu", vtui.FrameManager.GetTopFrame())
	}
	if menu.Y1 < 0 || menu.Y2 >= scr.Height() {
		t.Fatalf("menu bounds = (%d,%d)-(%d,%d), screen height = %d", menu.X1, menu.Y1, menu.X2, menu.Y2, scr.Height())
	}
	if menu.ViewHeight >= len(items) || !menu.ShowScrollBar || menu.ScrollBar == nil {
		t.Fatalf("menu viewport = %d items=%d scrollbar=%v bar=%v", menu.ViewHeight, len(items), menu.ShowScrollBar, menu.ScrollBar != nil)
	}
	menu.SetSelectPos(len(items) - 1)
	if menu.TopPos == 0 {
		t.Fatal("selecting the last item did not scroll the menu")
	}
	menu.Close()
}

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

func TestPanelsFrame_SideMenuExposesWorkspaceHotkeys(t *testing.T) {
	items := (&PanelsFrame{}).leftMenu().SubItems
	for _, tc := range []struct {
		command  int
		label    string
		shortcut string
	}{
		{command: CmWorkspaceNew, label: "Action.Workspace.New", shortcut: "Ctrl+N"},
		{command: CmWorkspaceClose, label: "Action.Workspace.Close", shortcut: "Ctrl+W"},
	} {
		found := false
		for _, item := range items {
			if item.Command != tc.command {
				continue
			}
			found = true
			if item.Text != Msg(tc.label) {
				t.Errorf("workspace command %d label = %q, want %q", tc.command, item.Text, Msg(tc.label))
			}
			if item.Shortcut != tc.shortcut {
				t.Errorf("workspace command %d shortcut = %q, want %q", tc.command, item.Shortcut, tc.shortcut)
			}
		}
		if !found {
			t.Errorf("left menu has no workspace command %d", tc.command)
		}
	}
}
