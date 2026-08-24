package main

import (
	"testing"

	"github.com/unxed/vtinput"
	"github.com/unxed/vtui"
)

func TestMenuHistory_ShiftF10SelectsLastExecutedItem(t *testing.T) {
	clearMenuHistory()
	t.Cleanup(clearMenuHistory)

	oldScreens := vtui.FrameManager.Screens
	oldActive := vtui.FrameManager.ActiveIdx
	t.Cleanup(func() {
		vtui.FrameManager.Screens = oldScreens
		vtui.FrameManager.ActiveIdx = oldActive
	})
	vtui.FrameManager.Screens = []*vtui.AppScreen{{Frames: nil}}
	vtui.FrameManager.ActiveIdx = 0

	first := vtui.NewVMenu("&Files")
	first.AddItem(vtui.MenuItem{Text: "&View", UserData: menuHistoryItemKey("view")})
	first.AddItem(vtui.MenuItem{Text: "√ &Copy", UserData: menuHistoryItemKey("copy")})
	hookMenuHistory(first)
	recordMenuHistory(first, 1)

	second := vtui.NewVMenu("&Files")
	second.AddItem(vtui.MenuItem{Text: "&View", UserData: menuHistoryItemKey("view")})
	second.AddItem(vtui.MenuItem{Text: " &Copy", UserData: menuHistoryItemKey("copy")})
	vtui.FrameManager.Push(second)

	if !handleMenuHistoryEvent(&vtinput.InputEvent{
		Type:            vtinput.KeyEventType,
		KeyDown:         true,
		VirtualKeyCode:  vtinput.VK_F10,
		ControlKeyState: vtinput.ShiftPressed,
	}) {
		t.Fatal("Shift+F10 was not consumed")
	}
	if second.SelectPos != 1 {
		t.Fatalf("Shift+F10 selected item %d, want 1", second.SelectPos)
	}
}

func TestMenuHistory_ShiftF10OpensMainMenuAtLastExecutedItem(t *testing.T) {
	clearMenuHistory()
	t.Cleanup(clearMenuHistory)

	oldScreens := vtui.FrameManager.Screens
	oldActive := vtui.FrameManager.ActiveIdx
	oldMenuBar := vtui.FrameManager.MenuBar
	t.Cleanup(func() {
		vtui.FrameManager.Screens = oldScreens
		vtui.FrameManager.ActiveIdx = oldActive
		vtui.FrameManager.MenuBar = oldMenuBar
	})
	vtui.FrameManager.Screens = []*vtui.AppScreen{{Frames: nil}}
	vtui.FrameManager.ActiveIdx = 0
	vtui.FrameManager.Push(vtui.NewDesktop())

	first := vtui.NewVMenu("&File")
	first.AddItem(vtui.MenuItem{Text: "&Open", UserData: menuHistoryItemKey("open")})
	first.AddItem(vtui.MenuItem{Text: "&Save", UserData: menuHistoryItemKey("save")})
	hookMenuHistory(first)
	recordMenuHistory(first, 1)

	menuBar := vtui.NewMenuBar([]string{"&File"})
	menuBar.Items[0].SubItems = []vtui.MenuItem{
		{Text: "&Open", UserData: menuHistoryItemKey("open")},
		{Text: "&Save", UserData: menuHistoryItemKey("save")},
	}
	vtui.FrameManager.MenuBar = menuBar

	if !handleMenuHistoryEvent(&vtinput.InputEvent{
		Type:            vtinput.KeyEventType,
		KeyDown:         true,
		VirtualKeyCode:  vtinput.VK_F10,
		ControlKeyState: vtinput.ShiftPressed,
	}) {
		t.Fatal("Shift+F10 was not consumed")
	}
	menu, ok := vtui.FrameManager.GetTopFrame().(*vtui.VMenu)
	if !ok {
		t.Fatalf("top frame = %T, want main menu", vtui.FrameManager.GetTopFrame())
	}
	if menu.SelectPos != 1 {
		t.Fatalf("main menu selected item %d, want 1", menu.SelectPos)
	}
}

func TestMenuHistory_ShiftF10DoesNotOverrideUserMenu(t *testing.T) {
	clearMenuHistory()
	t.Cleanup(clearMenuHistory)

	menu := vtui.NewVMenu("User menu")
	markUserMenu(menu)
	menu.AddItem(vtui.MenuItem{Text: "First"})
	menu.AddItem(vtui.MenuItem{Text: "Second"})
	menu.SetSelectPos(1)
	vtui.FrameManager.Push(menu)

	if handleMenuHistoryEvent(&vtinput.InputEvent{
		Type:            vtinput.KeyEventType,
		KeyDown:         true,
		VirtualKeyCode:  vtinput.VK_F10,
		ControlKeyState: vtinput.ShiftPressed,
	}) {
		t.Fatal("Shift+F10 unexpectedly intercepted user menu")
	}
	if menu.SelectPos != 1 {
		t.Fatalf("user menu selection changed to %d", menu.SelectPos)
	}
}
