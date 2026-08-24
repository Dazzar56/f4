package main

import (
	"fmt"
	"strings"
	"sync"

	"github.com/unxed/vtinput"
	"github.com/unxed/vtui"
)

// menuHistoryItemKey is attached to generated menu items so a checkmark,
// localization change, or a hidden item cannot make the remembered position
// drift to a different command the next time the menu is built.
type menuHistoryItemKey string

var menuHistory = struct {
	sync.Mutex
	lastByTitle map[string]string
	hooked      map[*vtui.VMenu]bool
	userMenus   map[*vtui.VMenu]bool
}{
	lastByTitle: make(map[string]string),
	hooked:      make(map[*vtui.VMenu]bool),
	userMenus:   make(map[*vtui.VMenu]bool),
}

// markUserMenu excludes Far-style user menus from this feature. Their
// Shift+F10 behavior is intentionally different: it closes the whole menu
// chain, including nested submenus.
func markUserMenu(menu *vtui.VMenu) {
	if menu == nil {
		return
	}
	menuHistory.Lock()
	menuHistory.userMenus[menu] = true
	menuHistory.Unlock()
}

func isUserMenu(menu *vtui.VMenu) bool {
	if menu == nil {
		return false
	}
	menuHistory.Lock()
	marked := menuHistory.userMenus[menu]
	menuHistory.Unlock()
	return marked
}

func menuItemHistoryKey(item vtui.MenuItem) string {
	if key, ok := item.UserData.(menuHistoryItemKey); ok {
		return string(key)
	}
	if item.Command != 0 {
		return fmt.Sprintf("command:%d", item.Command)
	}

	// This fallback covers menus whose items are supplied by plugins or by
	// small ad-hoc dialogs. Strip visual state and accelerator markers so the
	// key remains useful when the same menu is rebuilt.
	text := strings.TrimSpace(item.Text)
	text = strings.TrimLeft(text, "√✓")
	text = strings.TrimSpace(text)
	text, _, _ = vtui.ParseAmpersandString(text)
	return "text:" + strings.TrimSpace(text)
}

func recordMenuHistory(menu *vtui.VMenu, index int) {
	if menu == nil || isUserMenu(menu) || index < 0 || index >= len(menu.Items) {
		return
	}
	item := menu.Items[index]
	if item.Separator {
		return
	}

	menuHistory.Lock()
	menuHistory.lastByTitle[menu.GetTitle()] = menuItemHistoryKey(item)
	menuHistory.Unlock()
}

// hookMenuHistory installs a small OnAction wrapper. VMenu is supplied by
// vtui, and the main menu creates its VMenu internally, so the wrapper is
// installed lazily by the application's event filter when a menu reaches the
// top of the frame stack.
func hookMenuHistory(menu *vtui.VMenu) {
	if menu == nil || isUserMenu(menu) {
		return
	}

	menuHistory.Lock()
	if menuHistory.hooked[menu] {
		menuHistory.Unlock()
		return
	}
	menuHistory.hooked[menu] = true
	previous := menu.OnAction
	menu.OnAction = func(index int) {
		recordMenuHistory(menu, index)
		if previous != nil {
			previous(index)
		}
	}
	menuHistory.Unlock()
}

func selectLastMenuItem(menu *vtui.VMenu) bool {
	if menu == nil || isUserMenu(menu) {
		return false
	}

	menuHistory.Lock()
	key, ok := menuHistory.lastByTitle[menu.GetTitle()]
	menuHistory.Unlock()
	if !ok {
		return true
	}

	for i, item := range menu.Items {
		if item.Separator || menuItemHistoryKey(item) != key {
			continue
		}
		menu.SetSelectPos(i)
		return true
	}
	return true
}

func isSelectLastMenuItemKey(e *vtinput.InputEvent) bool {
	if e == nil || e.Type != vtinput.KeyEventType || !e.KeyDown || e.VirtualKeyCode != vtinput.VK_F10 {
		return false
	}
	mods := e.ControlKeyState
	return mods&vtinput.ShiftPressed != 0 &&
		mods&(vtinput.LeftCtrlPressed|vtinput.RightCtrlPressed|vtinput.LeftAltPressed|vtinput.RightAltPressed) == 0
}

// handleMenuHistoryEvent is installed in FrameManager.EventFilter. It both
// observes normal menu activations and handles Shift+F10 before vtui's
// default F10 handler can close the menu.
func handleMenuHistoryEvent(e *vtinput.InputEvent) bool {
	if vtui.FrameManager == nil {
		return false
	}
	menu, ok := vtui.FrameManager.GetTopFrame().(*vtui.VMenu)
	if isSelectLastMenuItemKey(e) {
		if ok && menu != nil {
			hookMenuHistory(menu)
			return selectLastMenuItem(menu)
		}

		// With no submenu open, Shift+F10 has the same entry point as F9:
		// activate the current screen's main menu, then restore its last item.
		// User menus are handled above by their own VMenu behavior and are not
		// routed through this branch.
		if !actionActivateMainMenu() {
			return false
		}
		if menu, ok = vtui.FrameManager.GetTopFrame().(*vtui.VMenu); ok {
			hookMenuHistory(menu)
			selectLastMenuItem(menu)
		}
		return true
	}

	if ok && menu != nil {
		hookMenuHistory(menu)
	}
	return false
}

// clearMenuHistory is kept small and package-local so tests can isolate the
// process-global menu history without reaching into the implementation.
func clearMenuHistory() {
	menuHistory.Lock()
	menuHistory.lastByTitle = make(map[string]string)
	menuHistory.hooked = make(map[*vtui.VMenu]bool)
	menuHistory.userMenus = make(map[*vtui.VMenu]bool)
	menuHistory.Unlock()
}
